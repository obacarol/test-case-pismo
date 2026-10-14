FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22

RUN adduser -D -H -u 65532 app
COPY --from=build --chown=app:app /out/api /api
COPY --from=build --chown=app:app /out/migrate /migrate
USER app
EXPOSE 8080
ENTRYPOINT ["/api"]
