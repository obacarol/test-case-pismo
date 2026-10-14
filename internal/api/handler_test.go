package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caroline-amorim/test-case-pismo/internal/account"
	"github.com/caroline-amorim/test-case-pismo/internal/money"
	"github.com/caroline-amorim/test-case-pismo/internal/transaction"
)

func TestAccountAndTransactionHTTP(t *testing.T) {
	t.Parallel()

	when := time.Date(2020, 1, 5, 9, 34, 18, 589322300, time.UTC)
	accounts := account.NewService(newMemAccounts())
	txs := &memTransactions{}
	handler := NewHandler(accounts, transaction.NewService(accounts, txs, func() time.Time { return when }), okPinger{}, DefaultSettings())
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	created := postJSON(t, srv.URL+"/accounts", `{"document_number":"12345678900"}`, http.StatusCreated)
	if !strings.Contains(created, `"account_id":1`) || !strings.Contains(created, `"document_number":"12345678900"`) {
		t.Fatalf("create account body: %s", created)
	}

	got := get(t, srv.URL+"/accounts/1", http.StatusOK)
	if got != created && !strings.Contains(got, `"document_number":"12345678900"`) {
		t.Fatalf("get account body: %s", got)
	}

	conflict := postJSON(t, srv.URL+"/accounts", `{"document_number":"12345678900"}`, http.StatusConflict)
	requireCode(t, conflict, "document_number_already_exists")

	formatted := postJSON(t, srv.URL+"/accounts", `{"document_number":"  AB-12  "}`, http.StatusCreated)
	if !strings.Contains(formatted, `"document_number":"AB-12"`) {
		t.Fatalf("formatted document body: %s", formatted)
	}

	invalidDoc := postJSON(t, srv.URL+"/accounts", `{"document_number":"ba\nd"}`, http.StatusBadRequest)
	requireCode(t, invalidDoc, "invalid_document_number")
	unknownField := postJSON(t, srv.URL+"/accounts", `{"document_number":"1","extra":true}`, http.StatusBadRequest)
	requireCode(t, unknownField, "invalid_request")
	missing := get(t, srv.URL+"/accounts/99", http.StatusNotFound)
	requireCode(t, missing, "account_not_found")
	badID := get(t, srv.URL+"/accounts/0", http.StatusBadRequest)
	requireCode(t, badID, "invalid_request")
	get(t, srv.URL+"/accounts/01", http.StatusBadRequest)

	purchase := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":1,"amount":50.0}`, "purchase", http.StatusCreated)
	for _, want := range []string{`"transaction_id":1`, `"operation_type_id":1`, `"amount":-50.00`, `"event_date":"2020-01-05T09:34:18.5893223Z"`} {
		if !strings.Contains(purchase, want) {
			t.Fatalf("purchase body %s missing %s", purchase, want)
		}
	}
	if txs.last.Amount != -5000 {
		t.Fatalf("stored cents %d", txs.last.Amount)
	}

	voucher := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":4,"amount":123.45}`, "voucher", http.StatusCreated)
	if !strings.Contains(voucher, `"amount":123.45`) {
		t.Fatalf("voucher body: %s", voucher)
	}

	installment := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":2,"amount":10.5}`, "installment", http.StatusCreated)
	if !strings.Contains(installment, `"amount":-10.50`) {
		t.Fatalf("installment body: %s", installment)
	}

	missingAccount := postJSONKey(t, srv.URL+"/transactions", `{"account_id":99,"operation_type_id":3,"amount":10}`, "missing", http.StatusNotFound)
	requireCode(t, missingAccount, "account_not_found")
	badType := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":9,"amount":10}`, "bad-type", http.StatusBadRequest)
	requireCode(t, badType, "invalid_operation_type")
	zeroAmount := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":1,"amount":0}`, "zero", http.StatusBadRequest)
	requireCode(t, zeroAmount, "invalid_amount")
	negativeAmount := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":1,"amount":-10}`, "negative", http.StatusBadRequest)
	requireCode(t, negativeAmount, "invalid_amount")
	tooPrecise := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":1,"amount":10.999}`, "precise", http.StatusBadRequest)
	requireCode(t, tooPrecise, "invalid_amount")
	missingKey := postJSON(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":1,"amount":10}`, http.StatusBadRequest)
	requireCode(t, missingKey, "invalid_idempotency_key")

	replay := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":1,"amount":50.0}`, "purchase", http.StatusOK)
	if replay != purchase {
		t.Fatalf("replay body %s", replay)
	}
	if txs.next != 3 {
		t.Fatalf("stored %d transactions", txs.next)
	}
	conflictKey := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":4,"amount":50.0}`, "purchase", http.StatusConflict)
	requireCode(t, conflictKey, "idempotency_key_already_used")

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/accounts", strings.NewReader(`{"document_number":"999"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d body %s", res.StatusCode, payload)
	}
	requireCode(t, string(payload), "unsupported_media_type")
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("missing cache control")
	}

	missingType, err := http.NewRequest(http.MethodPost, srv.URL+"/accounts", strings.NewReader(`{"document_number":"888"}`))
	if err != nil {
		t.Fatal(err)
	}
	missingRes, err := srv.Client().Do(missingType)
	if err != nil {
		t.Fatal(err)
	}
	defer missingRes.Body.Close()
	missingBody, err := io.ReadAll(missingRes.Body)
	if err != nil {
		t.Fatal(err)
	}
	if missingRes.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d body %s", missingRes.StatusCode, missingBody)
	}
	requireCode(t, string(missingBody), "unsupported_media_type")

	tooLarge := strings.Repeat("a", int(DefaultSettings().MaxBodyBytes)+1)
	largeReq, err := http.NewRequest(http.MethodPost, srv.URL+"/accounts", strings.NewReader(tooLarge))
	if err != nil {
		t.Fatal(err)
	}
	largeReq.Header.Set("Content-Type", "application/json")
	largeRes, err := srv.Client().Do(largeReq)
	if err != nil {
		t.Fatal(err)
	}
	defer largeRes.Body.Close()
	largeBody, err := io.ReadAll(largeRes.Body)
	if err != nil {
		t.Fatal(err)
	}
	if largeRes.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d body %s", largeRes.StatusCode, largeBody)
	}
	requireCode(t, string(largeBody), "request_body_too_large")

	tooWideType := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":32768,"amount":10}`, "wide-type", http.StatusBadRequest)
	requireCode(t, tooWideType, "invalid_operation_type")
	floatID := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1.5,"operation_type_id":1,"amount":10}`, "float-id", http.StatusBadRequest)
	requireCode(t, floatID, "invalid_request")
	get(t, srv.URL+"/accounts/+1", http.StatusBadRequest)
	get(t, srv.URL+"/accounts/-1", http.StatusBadRequest)
}

func TestHandlerMapsInternalAndPanic(t *testing.T) {
	t.Parallel()

	accounts := account.NewService(newMemAccounts())
	handler := NewHandler(accounts, transaction.NewService(accounts, &failingTx{err: errors.New("boom")}, time.Now), okPinger{}, DefaultSettings())
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	created := postJSON(t, srv.URL+"/accounts", `{"document_number":"1"}`, http.StatusCreated)
	if !strings.Contains(created, `"account_id":1`) {
		t.Fatalf("create account: %s", created)
	}
	internal := postJSONKey(t, srv.URL+"/transactions", `{"account_id":1,"operation_type_id":1,"amount":10}`, "boom", http.StatusInternalServerError)
	requireCode(t, internal, "internal_error")

	panicHandler := NewHandler(accounts, transaction.NewService(accounts, &failingTx{panicOnCreate: true}, time.Now), okPinger{}, DefaultSettings())
	req := httptest.NewRequest(http.MethodPost, "/transactions", strings.NewReader(`{"account_id":1,"operation_type_id":1,"amount":10}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "panic")
	rec := httptest.NewRecorder()
	panicHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic status %d body %s", rec.Code, rec.Body.String())
	}
	requireCode(t, rec.Body.String(), "internal_error")
}

func TestHealth(t *testing.T) {
	t.Parallel()

	accounts := account.NewService(newMemAccounts())
	txs := transaction.NewService(accounts, &memTransactions{}, time.Now)

	okHandler := NewHandler(accounts, txs, okPinger{}, DefaultSettings())
	okReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	okRec := httptest.NewRecorder()
	okHandler.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK || !strings.Contains(okRec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health ok %d %s", okRec.Code, okRec.Body.String())
	}

	downHandler := NewHandler(accounts, txs, downPinger{}, DefaultSettings())
	downReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	downRec := httptest.NewRecorder()
	downHandler.ServeHTTP(downRec, downReq)
	if downRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health down %d %s", downRec.Code, downRec.Body.String())
	}
	requireCode(t, downRec.Body.String(), "service_unavailable")

	nilHandler := NewHandler(accounts, txs, nil, DefaultSettings())
	nilReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	nilRec := httptest.NewRecorder()
	nilHandler.ServeHTTP(nilRec, nilReq)
	if nilRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health nil pinger %d %s", nilRec.Code, nilRec.Body.String())
	}
	requireCode(t, nilRec.Body.String(), "service_unavailable")
}

func requireCode(t *testing.T, body, code string) {
	t.Helper()
	if !strings.Contains(body, `"code":"`+code+`"`) || !strings.Contains(body, `"message":`) {
		t.Fatalf("body %s missing code %s", body, code)
	}
}

func postJSON(t *testing.T, url, body string, want int) string {
	t.Helper()
	return postJSONKey(t, url, body, "", want)
}

func postJSONKey(t *testing.T, url, body, key string, want int) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("POST %s status %d body %s", url, res.StatusCode, payload)
	}
	return string(payload)
}

func get(t *testing.T, url string, want int) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("GET %s status %d body %s", url, res.StatusCode, payload)
	}
	return string(payload)
}

type memAccounts struct {
	byDoc map[string]account.Account
	byID  map[int64]account.Account
	next  int64
}

func newMemAccounts() *memAccounts {
	return &memAccounts{
		byDoc: map[string]account.Account{},
		byID:  map[int64]account.Account{},
		next:  1,
	}
}

func (m *memAccounts) Create(_ context.Context, documentNumber string) (account.Account, error) {
	if _, ok := m.byDoc[documentNumber]; ok {
		return account.Account{}, account.ErrConflict
	}
	acc := account.Account{ID: m.next, DocumentNumber: documentNumber}
	m.next++
	m.byDoc[documentNumber] = acc
	m.byID[acc.ID] = acc
	return acc, nil
}

func (m *memAccounts) Get(_ context.Context, id int64) (account.Account, error) {
	acc, ok := m.byID[id]
	if !ok {
		return account.Account{}, account.ErrNotFound
	}
	return acc, nil
}

type memTransactions struct {
	next  int64
	last  transaction.Transaction
	byKey map[string]transaction.Transaction
}

func (m *memTransactions) Create(_ context.Context, accountID int64, operationTypeID int16, amount money.Cents, eventDate time.Time, idempotencyKey string) (transaction.Transaction, error) {
	if m.byKey == nil {
		m.byKey = map[string]transaction.Transaction{}
	}
	if _, ok := m.byKey[idempotencyKey]; ok {
		return transaction.Transaction{}, transaction.ErrDuplicateIdempotencyKey
	}
	m.next++
	m.last = transaction.Transaction{
		ID:              m.next,
		AccountID:       accountID,
		OperationTypeID: operationTypeID,
		Amount:          amount,
		EventDate:       eventDate,
	}
	m.byKey[idempotencyKey] = m.last
	return m.last, nil
}

func (m *memTransactions) GetByIdempotencyKey(_ context.Context, idempotencyKey string) (transaction.Transaction, error) {
	tx, ok := m.byKey[idempotencyKey]
	if !ok {
		return transaction.Transaction{}, errors.New("missing")
	}
	return tx, nil
}

type okPinger struct{}

func (okPinger) PingContext(context.Context) error { return nil }

type downPinger struct{}

func (downPinger) PingContext(context.Context) error { return errors.New("db down") }

type failingTx struct {
	err           error
	panicOnCreate bool
}

func (f *failingTx) Create(context.Context, int64, int16, money.Cents, time.Time, string) (transaction.Transaction, error) {
	if f.panicOnCreate {
		panic("boom")
	}
	return transaction.Transaction{}, f.err
}

func (f *failingTx) GetByIdempotencyKey(context.Context, string) (transaction.Transaction, error) {
	return transaction.Transaction{}, errors.New("unused")
}
