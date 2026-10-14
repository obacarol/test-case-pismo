package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDefaultSettings(t *testing.T) {
	t.Parallel()

	got := DefaultSettings()
	if got.MaxOpenConns != 10 || got.MaxIdleConns != 5 {
		t.Fatalf("pool %+v", got)
	}
	if got.ConnMaxLifetime != 30*time.Minute || got.ConnMaxIdleTime != 5*time.Minute || got.PingTimeout != 5*time.Second {
		t.Fatalf("durations %+v", got)
	}
}

func TestSplitSQL(t *testing.T) {
	t.Parallel()

	got := splitSQL(`
		CREATE TABLE a (id INT);
		CREATE TABLE b (id INT);
	`)
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0] != "CREATE TABLE a (id INT)" || got[1] != "CREATE TABLE b (id INT)" {
		t.Fatalf("statements %#v", got)
	}
	if len(splitSQL("   ;  ; ")) != 0 {
		t.Fatal("expected empty statements")
	}
}

func TestPostgresViolation(t *testing.T) {
	t.Parallel()

	code, constraint := postgresViolation(&pgconn.PgError{Code: "23505", ConstraintName: "accounts_document_number_key"})
	if code != "23505" || constraint != "accounts_document_number_key" {
		t.Fatalf("got %s %s", code, constraint)
	}
	code, constraint = postgresViolation(errors.New("plain"))
	if code != "" || constraint != "" {
		t.Fatalf("got %s %s", code, constraint)
	}
}
