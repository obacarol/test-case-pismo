//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/caroline-amorim/test-case-pismo/internal/account"
	"github.com/caroline-amorim/test-case-pismo/internal/money"
	"github.com/caroline-amorim/test-case-pismo/internal/transaction"
)

func TestRepositoryAgainstPostgres(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	accounts := NewAccountRepository(db)
	txs := NewTransactionRepository(db)
	when := time.Date(2020, 1, 1, 10, 0, 0, 0, time.FixedZone("BRT", -3*60*60))

	created, err := accounts.Create(ctx, "12345678900")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.DocumentNumber != "12345678900" {
		t.Fatalf("created %+v", created)
	}
	if _, err := accounts.Create(ctx, "12345678900"); !errors.Is(err, account.ErrConflict) {
		t.Fatalf("duplicate err = %v", err)
	}
	got, err := accounts.Get(ctx, created.ID)
	if err != nil || got != created {
		t.Fatalf("get %+v err %v", got, err)
	}
	if _, err := accounts.Get(ctx, 99); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}

	purchase, err := txs.Create(ctx, created.ID, transaction.NormalPurchase, -5000, when, "purchase")
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Amount != -5000 || purchase.AccountID != created.ID {
		t.Fatalf("purchase %+v", purchase)
	}
	if !purchase.EventDate.Equal(when.UTC()) {
		t.Fatalf("event date %s", purchase.EventDate)
	}

	voucher, err := txs.Create(ctx, created.ID, transaction.CreditVoucher, 12345, when.UTC(), "voucher")
	if err != nil || voucher.Amount != 12345 {
		t.Fatalf("voucher %+v err %v", voucher, err)
	}
	installment, err := txs.Create(ctx, created.ID, transaction.PurchaseWithInstallments, -2350, when.UTC(), "installment")
	if err != nil || installment.Amount != -2350 {
		t.Fatalf("installment %+v err %v", installment, err)
	}
	withdrawal, err := txs.Create(ctx, created.ID, transaction.Withdrawal, -1870, when.UTC(), "withdrawal")
	if err != nil || withdrawal.Amount != -1870 {
		t.Fatalf("withdrawal %+v err %v", withdrawal, err)
	}

	if _, err := txs.Create(ctx, created.ID, transaction.NormalPurchase, -100, when.UTC(), "purchase"); !errors.Is(err, transaction.ErrDuplicateIdempotencyKey) {
		t.Fatalf("duplicate key err = %v", err)
	}
	if _, err := txs.Create(ctx, 99, transaction.NormalPurchase, -100, when.UTC(), "missing-account"); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("fk err = %v", err)
	}
	if _, err := txs.Create(ctx, created.ID, 9, -100, when.UTC(), "bad-type"); !errors.Is(err, transaction.ErrInvalidOperation) {
		t.Fatalf("check err = %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO transactions (account_id, operation_type_id, amount_cents, event_date, idempotency_key)
		VALUES ($1, $2, $3, $4, $5)`, created.ID, transaction.NormalPurchase, 5000, when.UTC(), "positive-purchase")
	if err == nil {
		t.Fatal("positive purchase amount bypassed the sign check")
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO transactions (account_id, operation_type_id, amount_cents, event_date, idempotency_key)
		VALUES ($1, $2, $3, $4, $5)`, created.ID, transaction.CreditVoucher, -5000, when.UTC(), "negative-credit")
	if err == nil {
		t.Fatal("negative credit amount bypassed the sign check")
	}

	loaded, err := txs.GetByIdempotencyKey(ctx, "purchase")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != purchase.ID || loaded.Amount != money.Cents(-5000) {
		t.Fatalf("loaded %+v", loaded)
	}
	if !loaded.EventDate.Equal(when.UTC()) {
		t.Fatalf("loaded event date %s", loaded.EventDate)
	}
	if _, err := txs.GetByIdempotencyKey(ctx, "absent"); err == nil {
		t.Fatal("missing key was accepted")
	}

	var stored int64
	if err := db.QueryRowContext(ctx, `SELECT amount_cents FROM transactions WHERE idempotency_key = $1`, "purchase").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != -5000 {
		t.Fatalf("stored cents %d", stored)
	}
}

func TestOpenAndMigrateErrors(t *testing.T) {
	dsn := startPostgresDSN(t)
	ctx := context.Background()

	if _, err := Open(ctx, "postgres://bad:bad@127.0.0.1:1/none?sslmode=disable", DefaultSettings()); err == nil {
		t.Fatal("unreachable database was accepted")
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Open(canceled, dsn, DefaultSettings()); err == nil {
		t.Fatal("canceled open was accepted")
	}

	db, err := Open(ctx, dsn, DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	if err := applyScript(ctx, db, "broken.sql", `
		CREATE TABLE should_not_exist (id INT);
		THIS IS NOT SQL;
	`); err == nil {
		t.Fatal("bad migration script was accepted")
	}
	var leaked bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'should_not_exist'
	)`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked {
		t.Fatal("failed migration left a partial table")
	}

	if err := applyScript(ctx, db, "0001_accounts.sql", "SELECT 1"); err == nil {
		t.Fatal("duplicate migration version was accepted")
	}

	_ = db.Close()
	if err := Migrate(ctx, db); err == nil {
		t.Fatal("migrate on closed db was accepted")
	}
	if err := applyScript(ctx, db, "closed.sql", "SELECT 1"); err == nil {
		t.Fatal("applyScript on closed db was accepted")
	}
}

func TestRepositoryErrorsAfterClose(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	accounts := NewAccountRepository(db)
	txs := NewTransactionRepository(db)
	created, err := accounts.Create(ctx, "close-me")
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := txs.Create(ctx, created.ID, transaction.CreditVoucher, 100, when, "before-close"); err != nil {
		t.Fatal(err)
	}

	_ = db.Close()

	if _, err := accounts.Create(ctx, "after-close"); err == nil || errors.Is(err, account.ErrConflict) {
		t.Fatalf("create after close err = %v", err)
	}
	if _, err := accounts.Get(ctx, created.ID); err == nil || errors.Is(err, account.ErrNotFound) {
		t.Fatalf("get after close err = %v", err)
	}
	if _, err := txs.Create(ctx, created.ID, transaction.CreditVoucher, 100, when, "after-close"); err == nil ||
		errors.Is(err, account.ErrNotFound) ||
		errors.Is(err, transaction.ErrDuplicateIdempotencyKey) ||
		errors.Is(err, transaction.ErrInvalidOperation) {
		t.Fatalf("tx create after close err = %v", err)
	}
	if _, err := txs.GetByIdempotencyKey(ctx, "before-close"); err == nil {
		t.Fatal("get by key after close was accepted")
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), startPostgresDSN(t), DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func startPostgresDSN(t *testing.T) string {
	t.Helper()
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
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}
