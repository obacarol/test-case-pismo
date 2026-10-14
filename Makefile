.PHONY: test test-integration cover run

test:
	go test -race ./...

test-integration:
	go test -race -count=1 -tags=integration ./...

cover:
	go test -race ./internal/... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1
	go test -race -count=1 -tags=integration -coverpkg=./internal/... -coverprofile=coverage-integration.out ./internal/...
	go tool cover -func=coverage-integration.out | tail -1

run:
	docker compose up --build
