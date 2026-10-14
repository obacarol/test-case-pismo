// Package transaction records cardholder operations and applies the amount sign.
package transaction

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/caroline-amorim/test-case-pismo/internal/account"
	"github.com/caroline-amorim/test-case-pismo/internal/money"
)

// Operation types. The sign of each type is applied in ApplySign.
const (
	NormalPurchase           int16 = 1
	PurchaseWithInstallments int16 = 2
	Withdrawal               int16 = 3
	CreditVoucher            int16 = 4
)

const maxIdempotencyKeyChars = 64

var (
	// ErrInvalidOperation means the operation type is not one of the four known types.
	ErrInvalidOperation = errors.New("invalid operation type")
	// ErrInvalidIdempotencyKey means the key is empty, too long, or contains a control character.
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key")
	// ErrDuplicateIdempotencyKey means the key is already stored.
	ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
	// ErrIdempotencyConflict means the key is already stored for a different transaction.
	ErrIdempotencyConflict = errors.New("idempotency key already used")
)

// Transaction is one operation on an account. Amount is signed cents.
type Transaction struct {
	ID              int64
	AccountID       int64
	OperationTypeID int16
	Amount          money.Cents
	EventDate       time.Time
}

// Repository stores transactions. Amount is already signed.
type Repository interface {
	Create(ctx context.Context, accountID int64, operationTypeID int16, amount money.Cents, eventDate time.Time, idempotencyKey string) (Transaction, error)
	GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (Transaction, error)
}

// AccountReader checks that the account exists before a transaction is stored.
type AccountReader interface {
	Get(ctx context.Context, id int64) (account.Account, error)
}

// Service creates transactions.
type Service struct {
	accounts AccountReader
	repo     Repository
	now      func() time.Time
}

// NewService returns a service. now supplies EventDate; nil means time.Now.
func NewService(accounts AccountReader, repo Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{accounts: accounts, repo: repo, now: now}
}

// Create stores a transaction. magnitude is a positive amount; the sign is applied here.
// Purchase, purchase with installments, and withdrawal are stored negative.
// Credit voucher is stored positive.
// The bool is true when this call inserted the row. A repeated key for the same
// account, operation type, and amount returns the stored transaction and false.
func (s *Service) Create(ctx context.Context, accountID int64, operationTypeID int16, magnitude money.Cents, idempotencyKey string) (Transaction, bool, error) {
	key, err := NormalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return Transaction{}, false, err
	}
	if _, err := s.accounts.Get(ctx, accountID); err != nil {
		return Transaction{}, false, err
	}
	signed, err := ApplySign(operationTypeID, magnitude)
	if err != nil {
		return Transaction{}, false, err
	}
	created, err := s.repo.Create(ctx, accountID, operationTypeID, signed, s.now().UTC(), key)
	if errors.Is(err, ErrDuplicateIdempotencyKey) {
		return s.replay(ctx, accountID, operationTypeID, signed, key)
	}
	if err != nil {
		return Transaction{}, false, err
	}
	return created, true, nil
}

func (s *Service) replay(ctx context.Context, accountID int64, operationTypeID int16, signed money.Cents, key string) (Transaction, bool, error) {
	existing, err := s.repo.GetByIdempotencyKey(ctx, key)
	if err != nil {
		return Transaction{}, false, err
	}
	if existing.AccountID != accountID || existing.OperationTypeID != operationTypeID || existing.Amount != signed {
		return Transaction{}, false, ErrIdempotencyConflict
	}
	return existing, false, nil
}

// NormalizeIdempotencyKey trims surrounding space.
// Empty values, control characters, and more than 64 characters are rejected.
func NormalizeIdempotencyKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" || utf8.RuneCountInString(key) > maxIdempotencyKeyChars || containsControl(key) {
		return "", ErrInvalidIdempotencyKey
	}
	return key, nil
}

func containsControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// ApplySign turns a positive magnitude into the signed amount required by the operation type.
func ApplySign(operationTypeID int16, magnitude money.Cents) (money.Cents, error) {
	if magnitude <= 0 {
		return 0, money.ErrInvalidAmount
	}
	switch operationTypeID {
	case NormalPurchase, PurchaseWithInstallments, Withdrawal:
		return -magnitude, nil
	case CreditVoucher:
		return magnitude, nil
	default:
		return 0, ErrInvalidOperation
	}
}
