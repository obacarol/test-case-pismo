//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/caroline-amorim/test-case-pismo/internal/account"
	"github.com/caroline-amorim/test-case-pismo/internal/api"
	"github.com/caroline-amorim/test-case-pismo/internal/db/postgres"
	"github.com/caroline-amorim/test-case-pismo/internal/transaction"
)

func TestAPIAgainstPostgres(t *testing.T) {
	if _, err := os.Stat("/var/run/docker.sock"); err != nil {
		t.Skip("docker daemon is not running")
	}

	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("transactions"),
		tcpostgres.WithUsername("transactions"),
		tcpostgres.WithPassword("transactions"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := postgres.Open(ctx, dsn, postgres.DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := postgres.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	when := time.Date(2020, 1, 1, 10, 32, 7, 719922000, time.UTC)
	accounts := account.NewService(postgres.NewAccountRepository(db))
	transactions := transaction.NewService(accounts, postgres.NewTransactionRepository(db), func() time.Time { return when })
	server := httptest.NewServer(api.NewHandler(accounts, transactions, db, api.DefaultSettings()))
	t.Cleanup(server.Close)

	health := get(t, server.URL+"/health", http.StatusOK)
	if !strings.Contains(health, `"status":"ok"`) {
		t.Fatalf("health: %s", health)
	}

	body := post(t, server.URL+"/accounts", `{"document_number":"12345678900"}`, http.StatusCreated)
	if !strings.Contains(body, `"account_id":1`) || !strings.Contains(body, `"document_number":"12345678900"`) {
		t.Fatalf("create account: %s", body)
	}

	got := get(t, server.URL+"/accounts/1", http.StatusOK)
	if !strings.Contains(got, `"document_number":"12345678900"`) {
		t.Fatalf("get account: %s", got)
	}
	conflict := post(t, server.URL+"/accounts", `{"document_number":"12345678900"}`, http.StatusConflict)
	if !strings.Contains(conflict, `"code":"document_number_already_exists"`) {
		t.Fatalf("conflict body: %s", conflict)
	}

	formatted := post(t, server.URL+"/accounts", `{"document_number":"AB-12"}`, http.StatusCreated)
	if !strings.Contains(formatted, `"document_number":"AB-12"`) {
		t.Fatalf("formatted document: %s", formatted)
	}

	purchase := postKey(t, server.URL+"/transactions", `{"account_id":1,"operation_type_id":1,"amount":50}`, "purchase", http.StatusCreated)
	for _, want := range []string{`"amount":-50.00`, `"event_date":"2020-01-01T10:32:07.719922Z"`, `"operation_type_id":1`} {
		if !strings.Contains(purchase, want) {
			t.Fatalf("purchase %s missing %s", purchase, want)
		}
	}

	voucher := postKey(t, server.URL+"/transactions", `{"account_id":1,"operation_type_id":4,"amount":60.0}`, "voucher", http.StatusCreated)
	if !strings.Contains(voucher, `"amount":60.00`) {
		t.Fatalf("voucher: %s", voucher)
	}
	installment := postKey(t, server.URL+"/transactions", `{"account_id":1,"operation_type_id":2,"amount":23.5}`, "installment", http.StatusCreated)
	if !strings.Contains(installment, `"amount":-23.50`) {
		t.Fatalf("installment: %s", installment)
	}
	withdrawal := postKey(t, server.URL+"/transactions", `{"account_id":1,"operation_type_id":3,"amount":18.7}`, "withdrawal", http.StatusCreated)
	if !strings.Contains(withdrawal, `"amount":-18.70`) {
		t.Fatalf("withdrawal: %s", withdrawal)
	}

	postKey(t, server.URL+"/transactions", `{"account_id":99,"operation_type_id":1,"amount":10}`, "missing", http.StatusNotFound)
	replay := postKey(t, server.URL+"/transactions", `{"account_id":1,"operation_type_id":1,"amount":50}`, "purchase", http.StatusOK)
	if replay != purchase {
		t.Fatalf("replay %s", replay)
	}
	var other struct {
		AccountID int64 `json:"account_id"`
	}
	if err := json.Unmarshal([]byte(formatted), &other); err != nil {
		t.Fatal(err)
	}
	postKey(t, server.URL+"/transactions", fmt.Sprintf(`{"account_id":%d,"operation_type_id":1,"amount":50}`, other.AccountID), "purchase", http.StatusConflict)

	var cents int64
	err = db.QueryRowContext(ctx, `SELECT amount_cents FROM transactions WHERE operation_type_id = $1`, 1).Scan(&cents)
	if err != nil {
		t.Fatal(err)
	}
	if cents != -5000 {
		t.Fatalf("stored cents %d", cents)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO transactions (account_id, operation_type_id, amount_cents, event_date, idempotency_key)
		VALUES ($1, $2, $3, $4, $5)`, 1, 9, -100, when, "bad-type")
	if err == nil {
		t.Fatal("operation type 9 was stored")
	}
}

func post(t *testing.T, url, payload string, want int) string {
	t.Helper()
	return postKey(t, url, payload, "", want)
}

func postKey(t *testing.T, url, payload, key string, want int) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(payload)))
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
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("POST %s status %d body %s", url, res.StatusCode, body)
	}
	return string(body)
}

func get(t *testing.T, url string, want int) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("GET %s status %d body %s", url, res.StatusCode, body)
	}
	return string(body)
}
