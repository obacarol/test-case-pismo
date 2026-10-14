package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/caroline-amorim/test-case-pismo/internal/account"
)

// AccountRepository implements account.Repository.
type AccountRepository struct {
	db *sql.DB
}

// NewAccountRepository returns a repository backed by db.
func NewAccountRepository(db *sql.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

// Create inserts an account. documentNumber must already be validated.
func (r *AccountRepository) Create(ctx context.Context, documentNumber string) (account.Account, error) {
	const q = `
		INSERT INTO accounts (document_number)
		VALUES ($1)
		RETURNING account_id, document_number`
	var acc account.Account
	err := r.db.QueryRowContext(ctx, q, documentNumber).Scan(&acc.ID, &acc.DocumentNumber)
	if err != nil {
		if code, _ := postgresViolation(err); code == "23505" {
			return account.Account{}, account.ErrConflict
		}
		return account.Account{}, errors.New("insert account")
	}
	return acc, nil
}

// Get loads an account by id.
func (r *AccountRepository) Get(ctx context.Context, id int64) (account.Account, error) {
	const q = `
		SELECT account_id, document_number
		FROM accounts
		WHERE account_id = $1`
	var acc account.Account
	err := r.db.QueryRowContext(ctx, q, id).Scan(&acc.ID, &acc.DocumentNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return account.Account{}, account.ErrNotFound
	}
	if err != nil {
		return account.Account{}, errors.New("select account")
	}
	return acc, nil
}

var _ account.Repository = (*AccountRepository)(nil)
