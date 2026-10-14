package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/caroline-amorim/test-case-pismo/internal/account"
	"github.com/caroline-amorim/test-case-pismo/internal/money"
	"github.com/caroline-amorim/test-case-pismo/internal/transaction"
)

// TransactionRepository implements transaction.Repository.
type TransactionRepository struct {
	db *sql.DB
}

// NewTransactionRepository returns a repository backed by db.
func NewTransactionRepository(db *sql.DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

// Create inserts a signed transaction. The database rejects unknown types and wrong signs.
func (r *TransactionRepository) Create(ctx context.Context, accountID int64, operationTypeID int16, amount money.Cents, eventDate time.Time, idempotencyKey string) (transaction.Transaction, error) {
	const q = `
		INSERT INTO transactions (account_id, operation_type_id, amount_cents, event_date, idempotency_key)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING transaction_id, account_id, operation_type_id, amount_cents, event_date`
	var tx transaction.Transaction
	var cents int64
	err := r.db.QueryRowContext(ctx, q, accountID, operationTypeID, int64(amount), eventDate, idempotencyKey).
		Scan(&tx.ID, &tx.AccountID, &tx.OperationTypeID, &cents, &tx.EventDate)
	if err != nil {
		code, _ := postgresViolation(err)
		switch code {
		case "23503":
			return transaction.Transaction{}, account.ErrNotFound
		case "23505":
			return transaction.Transaction{}, transaction.ErrDuplicateIdempotencyKey
		case "23514":
			return transaction.Transaction{}, transaction.ErrInvalidOperation
		default:
			return transaction.Transaction{}, errors.New("insert transaction")
		}
	}
	tx.Amount = money.Cents(cents)
	tx.EventDate = tx.EventDate.UTC()
	return tx, nil
}

// GetByIdempotencyKey loads the transaction stored for key.
func (r *TransactionRepository) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (transaction.Transaction, error) {
	const q = `
		SELECT transaction_id, account_id, operation_type_id, amount_cents, event_date
		FROM transactions
		WHERE idempotency_key = $1`
	var tx transaction.Transaction
	var cents int64
	err := r.db.QueryRowContext(ctx, q, idempotencyKey).
		Scan(&tx.ID, &tx.AccountID, &tx.OperationTypeID, &cents, &tx.EventDate)
	if err != nil {
		return transaction.Transaction{}, errors.New("select transaction")
	}
	tx.Amount = money.Cents(cents)
	tx.EventDate = tx.EventDate.UTC()
	return tx, nil
}

var _ transaction.Repository = (*TransactionRepository)(nil)
