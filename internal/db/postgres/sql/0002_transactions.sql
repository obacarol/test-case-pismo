CREATE TABLE transactions (
    transaction_id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts (account_id),
    operation_type_id SMALLINT NOT NULL CHECK (operation_type_id IN (1, 2, 3, 4)),
    amount_cents BIGINT NOT NULL CHECK (
        (operation_type_id IN (1, 2, 3) AND amount_cents < 0)
        OR (operation_type_id = 4 AND amount_cents > 0)
    ),
    event_date TIMESTAMPTZ NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE
);
