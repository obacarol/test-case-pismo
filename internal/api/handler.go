// Package api exposes the account and transaction HTTP endpoints.
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/caroline-amorim/test-case-pismo/internal/account"
	"github.com/caroline-amorim/test-case-pismo/internal/money"
	"github.com/caroline-amorim/test-case-pismo/internal/transaction"
)

// Settings controls request limits. Domain rules stay outside this struct.
type Settings struct {
	MaxBodyBytes   int64
	RequestTimeout time.Duration
}

// DefaultSettings returns the limits used when the environment does not override them.
func DefaultSettings() Settings {
	return Settings{
		MaxBodyBytes:   4096,
		RequestTimeout: 5 * time.Second,
	}
}

// Pinger checks that the process can reach its database.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Handler serves the account and transaction endpoints, plus /health.
type Handler struct {
	accounts     *account.Service
	transactions *transaction.Service
	db           Pinger
	settings     Settings
	mux          *http.ServeMux
}

// NewHandler wires the services and database ping to the HTTP routes.
func NewHandler(accounts *account.Service, transactions *transaction.Service, db Pinger, settings Settings) http.Handler {
	h := &Handler{
		accounts:     accounts,
		transactions: transactions,
		db:           db,
		settings:     settings,
		mux:          http.NewServeMux(),
	}
	h.mux.HandleFunc("POST /accounts", h.createAccount)
	h.mux.HandleFunc("GET /accounts/{accountId}", h.getAccount)
	h.mux.HandleFunc("POST /transactions", h.createTransaction)
	h.mux.HandleFunc("GET /health", h.health)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := newRequestID()
	w.Header().Set("X-Request-Id", requestID)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")

	rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	started := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("panic", "request_id", requestID, "method", r.Method, "path", r.URL.Path)
			writeError(rec, http.StatusInternalServerError, "internal_error", "internal error")
		}
		slog.Info("request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}()

	h.mux.ServeHTTP(rec, r.WithContext(withRequestID(r.Context(), requestID)))
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.settings.RequestTimeout)
	defer cancel()
	if h.db == nil || h.db.PingContext(ctx) != nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DocumentNumber string `json:"document_number"`
	}
	if err := h.readJSON(r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.settings.RequestTimeout)
	defer cancel()

	created, err := h.accounts.Create(ctx, body.DocumentNumber)
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	w.Header().Set("Location", "/accounts/"+strconv.FormatInt(created.ID, 10))
	writeJSON(w, http.StatusCreated, accountJSON{
		AccountID:      created.ID,
		DocumentNumber: created.DocumentNumber,
	})
}

func (h *Handler) getAccount(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r.PathValue("accountId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid account id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.settings.RequestTimeout)
	defer cancel()

	found, err := h.accounts.Get(ctx, id)
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accountJSON{
		AccountID:      found.ID,
		DocumentNumber: found.DocumentNumber,
	})
}

func (h *Handler) createTransaction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID       positiveInt64 `json:"account_id"`
		OperationTypeID positiveInt64 `json:"operation_type_id"`
		Amount          money.Cents   `json:"amount"`
	}
	if err := h.readJSON(r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	if body.AccountID == 0 || body.OperationTypeID == 0 || body.Amount == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	if body.OperationTypeID > 32767 {
		writeError(w, http.StatusBadRequest, "invalid_operation_type", "invalid operation type")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.settings.RequestTimeout)
	defer cancel()

	created, inserted, err := h.transactions.Create(ctx, int64(body.AccountID), int16(body.OperationTypeID), body.Amount, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	status := http.StatusOK
	if inserted {
		status = http.StatusCreated
	}
	writeJSON(w, status, transactionJSON{
		TransactionID:   created.ID,
		AccountID:       created.AccountID,
		OperationTypeID: created.OperationTypeID,
		Amount:          created.Amount,
		EventDate:       created.EventDate,
	})
}

type accountJSON struct {
	AccountID      int64  `json:"account_id"`
	DocumentNumber string `json:"document_number"`
}

type transactionJSON struct {
	TransactionID   int64       `json:"transaction_id"`
	AccountID       int64       `json:"account_id"`
	OperationTypeID int16       `json:"operation_type_id"`
	Amount          money.Cents `json:"amount"`
	EventDate       time.Time   `json:"event_date"`
}

type positiveInt64 int64

func (n *positiveInt64) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] == '"' || bytes.ContainsAny(b, ".eE+-") {
		return errInvalidJSON
	}
	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil || v <= 0 {
		return errInvalidJSON
	}
	*n = positiveInt64(v)
	return nil
}

func parsePathID(raw string) (int64, error) {
	if raw == "" || raw[0] == '0' || raw[0] == '-' || raw[0] == '+' {
		return 0, errInvalidJSON
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, errInvalidJSON
		}
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errInvalidJSON
	}
	return id, nil
}

var errInvalidJSON = errors.New("invalid json")

func (h *Handler) readJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return errInvalidJSON
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		return errUnsupportedMedia
	}
	limited := http.MaxBytesReader(nil, r.Body, h.settings.MaxBodyBytes)
	defer limited.Close()
	body, err := io.ReadAll(limited)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return errBodyTooLarge
		}
		return errInvalidJSON
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, money.ErrInvalidAmount) {
			return err
		}
		return errInvalidJSON
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errInvalidJSON
	}
	return nil
}

var (
	errUnsupportedMedia = errors.New("unsupported media type")
	errBodyTooLarge     = errors.New("body too large")
)

func writeDecodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnsupportedMedia):
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "content type must be application/json")
	case errors.Is(err, errBodyTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "request_body_too_large", "request body too large")
	case errors.Is(err, money.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, "invalid_amount", "invalid amount")
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request")
	}
}

func writeUseCaseError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, account.ErrInvalidDocument):
		writeError(w, http.StatusBadRequest, "invalid_document_number", "invalid document number")
	case errors.Is(err, money.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, "invalid_amount", "invalid amount")
	case errors.Is(err, transaction.ErrInvalidOperation):
		writeError(w, http.StatusBadRequest, "invalid_operation_type", "invalid operation type")
	case errors.Is(err, transaction.ErrInvalidIdempotencyKey):
		writeError(w, http.StatusBadRequest, "invalid_idempotency_key", "invalid idempotency key")
	case errors.Is(err, transaction.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_key_already_used", "idempotency key already used")
	case errors.Is(err, account.ErrNotFound):
		writeError(w, http.StatusNotFound, "account_not_found", "account not found")
	case errors.Is(err, account.ErrConflict):
		writeError(w, http.StatusConflict, "document_number_already_exists", "document number already exists")
	default:
		slog.Error("request failed", "request_id", requestIDFrom(r.Context()), "method", r.Method, "path", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}

type errorJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorJSON{Code: code, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		payload = []byte(`{"code":"internal_error","message":"internal error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

type contextKey int

const requestIDKey contextKey = 1

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func newRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(buf[:])
}
