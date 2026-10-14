package transaction

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caroline-amorim/test-case-pismo/internal/account"
	"github.com/caroline-amorim/test-case-pismo/internal/money"
)

func TestApplySign(t *testing.T) {
	t.Parallel()

	magnitude := money.Cents(5000)
	negative := []int16{NormalPurchase, PurchaseWithInstallments, Withdrawal}
	for _, op := range negative {
		got, err := ApplySign(op, magnitude)
		if err != nil {
			t.Fatal(err)
		}
		if got != -5000 {
			t.Fatalf("type %d signed to %d", op, got)
		}
	}

	got, err := ApplySign(CreditVoucher, magnitude)
	if err != nil {
		t.Fatal(err)
	}
	if got != 5000 {
		t.Fatalf("credit voucher signed to %d", got)
	}

	if _, err := ApplySign(9, magnitude); !errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("unknown type err = %v", err)
	}
	if _, err := ApplySign(NormalPurchase, 0); !errors.Is(err, money.ErrInvalidAmount) {
		t.Fatalf("zero err = %v", err)
	}
}

func TestServiceCreateAppliesSignAndTime(t *testing.T) {
	t.Parallel()

	when := time.Date(2020, 1, 1, 10, 32, 7, 719922000, time.FixedZone("BRT", -3*60*60))
	accounts := account.NewService(newAccountRepo(1))
	repo := &memTx{}
	svc := NewService(accounts, repo, func() time.Time { return when })

	tx, created, err := svc.Create(context.Background(), 1, NormalPurchase, 5050, "purchase")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected a new transaction")
	}
	if tx.Amount != -5050 {
		t.Fatalf("amount %d", tx.Amount)
	}
	if !tx.EventDate.Equal(when.UTC()) {
		t.Fatalf("event date %s", tx.EventDate)
	}
	if repo.saved.OperationTypeID != NormalPurchase || repo.saved.Amount != -5050 {
		t.Fatalf("saved %+v", repo.saved)
	}

	voucher, _, err := svc.Create(context.Background(), 1, CreditVoucher, 6000, "voucher")
	if err != nil {
		t.Fatal(err)
	}
	if voucher.Amount != 6000 {
		t.Fatalf("voucher amount %d", voucher.Amount)
	}

	if _, _, err := svc.Create(context.Background(), 2, Withdrawal, 100, "missing"); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("missing account err = %v", err)
	}
	if _, _, err := svc.Create(context.Background(), 1, 8, 100, "bad-type"); !errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("bad type err = %v", err)
	}
	if _, _, err := svc.Create(context.Background(), 1, NormalPurchase, 100, "  "); !errors.Is(err, ErrInvalidIdempotencyKey) {
		t.Fatalf("blank key err = %v", err)
	}
}

func TestServiceCreateReplaysTheSameKey(t *testing.T) {
	t.Parallel()

	when := time.Date(2020, 1, 1, 10, 32, 7, 0, time.UTC)
	accounts := account.NewService(newAccountRepo(1))
	repo := &memTx{}
	svc := NewService(accounts, repo, func() time.Time { return when })

	first, created, err := svc.Create(context.Background(), 1, NormalPurchase, 1000, "same")
	if err != nil || !created {
		t.Fatalf("first created=%v err=%v", created, err)
	}
	second, created, err := svc.Create(context.Background(), 1, NormalPurchase, 1000, " same ")
	if err != nil {
		t.Fatal(err)
	}
	if created || second.ID != first.ID || second.Amount != first.Amount {
		t.Fatalf("replay created=%v second=%+v first=%+v", created, second, first)
	}
	if repo.next != 1 {
		t.Fatalf("stored %d rows", repo.next)
	}

	if _, _, err := svc.Create(context.Background(), 1, CreditVoucher, 1000, "same"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different operation err = %v", err)
	}
}

func TestNewServiceUsesTimeNowWhenClockNil(t *testing.T) {
	t.Parallel()

	accounts := account.NewService(newAccountRepo(1))
	svc := NewService(accounts, &memTx{}, nil)
	before := time.Now().UTC().Add(-time.Second)
	tx, created, err := svc.Create(context.Background(), 1, CreditVoucher, 100, "now")
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if tx.EventDate.Before(before) || tx.EventDate.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("event date %s", tx.EventDate)
	}
}

func TestNormalizeIdempotencyKeyRejectsBadValues(t *testing.T) {
	t.Parallel()

	bad := []string{"", "  ", "a\nb", strings.Repeat("k", 65)}
	for _, in := range bad {
		if _, err := NormalizeIdempotencyKey(in); !errors.Is(err, ErrInvalidIdempotencyKey) {
			t.Fatalf("NormalizeIdempotencyKey(%q) err = %v", in, err)
		}
	}
	got, err := NormalizeIdempotencyKey("  key-1  ")
	if err != nil || got != "key-1" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestReplayFailsWhenStoredRowMissing(t *testing.T) {
	t.Parallel()

	accounts := account.NewService(newAccountRepo(1))
	repo := &memTx{forceDuplicate: true, failLookup: true}
	svc := NewService(accounts, repo, func() time.Time { return time.Unix(1, 0).UTC() })
	if _, _, err := svc.Create(context.Background(), 1, NormalPurchase, 100, "gone"); err == nil {
		t.Fatal("expected lookup failure after duplicate key")
	}
}

type accountRepo struct {
	id int64
}

func newAccountRepo(id int64) *accountRepo {
	return &accountRepo{id: id}
}

func (a *accountRepo) Create(context.Context, string) (account.Account, error) {
	return account.Account{}, errors.New("unused")
}

func (a *accountRepo) Get(_ context.Context, id int64) (account.Account, error) {
	if id != a.id {
		return account.Account{}, account.ErrNotFound
	}
	return account.Account{ID: id, DocumentNumber: "12345678900"}, nil
}

type memTx struct {
	saved          Transaction
	byKey          map[string]Transaction
	next           int64
	forceDuplicate bool
	failLookup     bool
}

func (m *memTx) Create(_ context.Context, accountID int64, operationTypeID int16, amount money.Cents, eventDate time.Time, idempotencyKey string) (Transaction, error) {
	if m.byKey == nil {
		m.byKey = map[string]Transaction{}
	}
	if m.forceDuplicate {
		return Transaction{}, ErrDuplicateIdempotencyKey
	}
	if _, ok := m.byKey[idempotencyKey]; ok {
		return Transaction{}, ErrDuplicateIdempotencyKey
	}
	m.next++
	m.saved = Transaction{
		ID:              m.next,
		AccountID:       accountID,
		OperationTypeID: operationTypeID,
		Amount:          amount,
		EventDate:       eventDate,
	}
	m.byKey[idempotencyKey] = m.saved
	return m.saved, nil
}

func (m *memTx) GetByIdempotencyKey(_ context.Context, idempotencyKey string) (Transaction, error) {
	if m.failLookup {
		return Transaction{}, errors.New("missing")
	}
	tx, ok := m.byKey[idempotencyKey]
	if !ok {
		return Transaction{}, errors.New("missing")
	}
	return tx, nil
}
