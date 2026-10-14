# Transactions

REST service for cardholder accounts and card transactions — a technical case.

Built with Go's standard library, the `pgx` driver, and Postgres.

## Prerequisites

- Docker and Docker Compose (for `make run` and integration tests)
- Make
- Go 1.26+ (for tests and for running the API on the host)

## How to run

### With Docker

```bash
make run
```

This builds the stack, starts Postgres, runs migrations, and serves the API at `http://localhost:8080`.

### On the host

Postgres must already be reachable. You can start only the database with Compose and wait until it is healthy:

```bash
docker compose up -d postgres --wait
```

Then migrate and run the API locally:

```bash
export DATABASE_URL='postgres://transactions:transactions@localhost:5432/transactions?sslmode=disable'
go run ./cmd/migrate
go run ./cmd/api
```

`PORT` defaults to `8080`. The API does not migrate on boot — `cmd/migrate` has to run first (or again after new `.sql` files).

## API

| Method | Path | Success | Errors |
| --- | --- | --- | --- |
| `POST` | `/accounts` | `201` | `400`, `409`, `413`, `415` |
| `GET` | `/accounts/{accountId}` | `200` | `400`, `404` |
| `POST` | `/transactions` | `201`, `200` | `400`, `404`, `409`, `413`, `415` |
| `GET` | `/health` | `200` | `503` |

Errors always have this shape:

```json
{"code":"invalid_request","message":"invalid request"}
```

In short: `400` for validation, `404` when the account does not exist, `409` for conflicts (duplicate document or idempotency key). A `Content-Type` other than `application/json` returns `415`. A body larger than 4 KiB returns `413`.

### Create an account

`POST /accounts`

```json
{"document_number":"12345678900"}
```

```bash
curl -s -X POST localhost:8080/accounts \
  -H 'Content-Type: application/json' \
  -d '{"document_number":"12345678900"}'
# 201 {"account_id":1,"document_number":"12345678900"}
```

The response includes `Location: /accounts/1`. Leading and trailing spaces on the document are removed. An empty value, more than 36 characters, or a control character returns `400` (`invalid_document_number`).

Document already registered:

```bash
curl -s -X POST localhost:8080/accounts \
  -H 'Content-Type: application/json' \
  -d '{"document_number":"12345678900"}'
# 409 {"code":"document_number_already_exists","message":"document number already exists"}
```

### Retrieve an account

`GET /accounts/{accountId}`

```bash
curl -s localhost:8080/accounts/1
# 200 {"account_id":1,"document_number":"12345678900"}
```

An invalid id returns `400`. A missing account returns `404` (`account_not_found`).

### Create a transaction

`POST /transactions`

The `Idempotency-Key` header is required. It is trimmed, must be 1–64 characters, and cannot contain control characters. Missing, empty, too long, or invalid keys return `400` (`invalid_idempotency_key`).

Purchase. The server stores a negative amount:

```bash
curl -s -X POST localhost:8080/transactions \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: purchase-1' \
  -d '{"account_id":1,"operation_type_id":1,"amount":50.00}'
# 201 {"transaction_id":1,"account_id":1,"operation_type_id":1,"amount":-50.00,"event_date":"..."}
```

Credit. The sign stays positive:

```bash
curl -s -X POST localhost:8080/transactions \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: voucher-1' \
  -d '{"account_id":1,"operation_type_id":4,"amount":123.45}'
# 201 {"transaction_id":2,"account_id":1,"operation_type_id":4,"amount":123.45,"event_date":"..."}
```

The request `amount` is positive and has at most two fractional digits. The API fills `event_date`, in UTC.

| Type | Operation | Stored sign |
| --- | --- | --- |
| `1` | purchase | negative |
| `2` | installment purchase | negative |
| `3` | withdrawal | negative |
| `4` | credit | positive |

The same key, with the same request, returns the transaction already stored:

```bash
curl -s -X POST localhost:8080/transactions \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: purchase-1' \
  -d '{"account_id":1,"operation_type_id":1,"amount":50.00}'
# 200 {"transaction_id":1,"account_id":1,"operation_type_id":1,"amount":-50.00,"event_date":"..."}
```

There is no `GET /transactions`. A caller that lost the `201` repeats this `POST`.

The same key with a different request:

```bash
curl -s -X POST localhost:8080/transactions \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: purchase-1' \
  -d '{"account_id":1,"operation_type_id":4,"amount":50.00}'
# 409 {"code":"idempotency_key_already_used","message":"idempotency key already used"}
```

A missing account returns `404`. A missing header, an invalid amount, or a type outside 1–4 returns `400`.

### Health

`GET /health` pings Postgres. It returns `200` with `{"status":"ok"}` when the database answers, and `503` with the usual error envelope when it does not:

```json
{"code":"service_unavailable","message":"database unavailable"}
```

Handy for local checks; not part of the case contract.

The OpenAPI contract is in [`openapi.yaml`](openapi.yaml).

## Configuration

Everything is loaded from the environment. There is no `.env` file.

`DATABASE_URL` is required — without it the process does not start. `PORT` defaults to `8080`. With `make run`, Compose sets both for the API container.

Optional variables cover HTTP timeouts, request size limits, and the Postgres pool (open/idle connections, lifetimes). Leave them unset and the defaults apply. Durations use Go's format (`5s`, `1m`). A bad value fails startup instead of falling back silently.

## Architecture

```text
cmd/api                  HTTP process
cmd/migrate              applies the schema
openapi.yaml             HTTP contract
internal/api             routes
internal/account         account rules
internal/transaction     transaction rules
internal/money           cents
internal/config          environment
internal/db/postgres     SQL
```

Account and transaction rules stay out of SQL; SQL stays out of the HTTP layer.

## Testing

```bash
make test
make test-integration
make cover
```

Or without Make:

```bash
go test -race ./...
go test -race -count=1 -tags=integration ./...
```

| Suite | Command | What it covers |
| --- | --- | --- |
| Unit | `make test` | Document normalization, sign rules, money parsing (no `float64`), idempotent create, config defaults and rejects |
| Handler | `make test` | Full HTTP paths with in-memory stores: status codes, error codes, `415`/`413`, replay `200`, key conflict `409`, health `200`/`503` |
| Integration | `make test-integration` | HTTP and repository flows against Postgres via Testcontainers (`-tags=integration`) |
| Coverage | `make cover` | Prints statement coverage for unit tests, then again with integration + `-coverpkg` |

Unit and handler tests stay offline. Integration tests need Docker — they catch SQL, constraints, and migrations that fakes miss. `make cover` writes `coverage.out` and `coverage-integration.out` (gitignored).

## Decisions

**Postgres.** This is a financial write path. Constraints, migrations, and a production-shaped driver matter more here than the lightest possible local boot.

**Money as `int64` cents.** Amounts stay exact through parse, store, and JSON. The API accepts a decimal with at most two fractional digits and rejects anything beyond that, so nothing is rounded away quietly.

**Sign from the operation type.** The client sends a positive amount; the type decides the stored sign (1–3 negative, 4 positive). One rule in the domain, and a SQL `CHECK` so a bypass cannot store the wrong sign.

**Operation types live in code.** The four ids are Go constants with the sign rule next to them. The database only enforces `IN (1, 2, 3, 4)` and the amount sign. That keeps the behaviour in one place for this closed set — a lookup table would make sense if types were edited outside a deploy.

**`404` when the account is missing on `POST /transactions`.** The create depends on that account. Same status as `GET /accounts/{id}`, so “unknown account” reads the same in both places.

**Migrate as its own process.** `cmd/migrate` applies each `.sql` once and records the filename in `schema_migrations`. Schema changes stay explicit, and the API can start without owning that step.

**Idempotency-Key on `POST /transactions`.** Without a `GET /transactions`, retries still need a way to recover the `transaction_id`. New key → `201`. Same key and body → `200` with the stored row. Same key, different body → `409`. The unique column keeps concurrent retries safe.

**Installment purchase as one row.** Type `2` stores the total amount. Count and due dates are out of scope for this case.

**No authentication.** The API is local and open on purpose for this exercise.
