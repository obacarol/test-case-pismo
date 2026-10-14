// Package account creates and reads cardholder accounts.
package account

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxDocumentChars = 36

var (
	// ErrNotFound means no account exists for the given id.
	ErrNotFound = errors.New("account not found")
	// ErrConflict means the document number is already registered.
	ErrConflict = errors.New("document number already exists")
	// ErrInvalidDocument means the document number is empty, too long, or contains a control character.
	ErrInvalidDocument = errors.New("invalid document number")
)

// Account is a cardholder account. DocumentNumber is the customer document.
type Account struct {
	ID             int64
	DocumentNumber string
}

// Repository persists accounts. Implementations must use parameterized queries.
type Repository interface {
	Create(ctx context.Context, documentNumber string) (Account, error)
	Get(ctx context.Context, id int64) (Account, error)
}

// Service applies account rules.
type Service struct {
	repo Repository
}

// NewService returns a service that stores accounts in repo.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create stores a new account. documentNumber is trimmed.
// Empty values, control characters, and more than 36 characters are rejected.
// Letters and punctuation stay. There is no country checksum.
func (s *Service) Create(ctx context.Context, documentNumber string) (Account, error) {
	doc, err := NormalizeDocumentNumber(documentNumber)
	if err != nil {
		return Account{}, err
	}
	return s.repo.Create(ctx, doc)
}

// Get returns the account with the given id. Non-positive ids return ErrNotFound.
func (s *Service) Get(ctx context.Context, id int64) (Account, error) {
	if id <= 0 {
		return Account{}, ErrNotFound
	}
	return s.repo.Get(ctx, id)
}

// NormalizeDocumentNumber trims surrounding space and keeps letters and punctuation.
// Empty values, control characters, and more than 36 characters are rejected.
func NormalizeDocumentNumber(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || utf8.RuneCountInString(s) > maxDocumentChars || containsControl(s) {
		return "", ErrInvalidDocument
	}
	return s, nil
}

func containsControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
