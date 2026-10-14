package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://transactions:transactions@localhost:5432/transactions?sslmode=disable")
	clearOptional(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 || cfg.Addr() != ":8080" {
		t.Fatalf("port %d addr %s", cfg.Port, cfg.Addr())
	}
	if cfg.ReadTimeout != 10*time.Second || cfg.API.MaxBodyBytes != 4096 || cfg.DB.MaxOpenConns != 10 {
		t.Fatalf("defaults were replaced: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/transactions")
	clearOptional(t)
	t.Setenv("PORT", "9090")
	t.Setenv("HTTP_READ_HEADER_TIMEOUT", "2s")
	t.Setenv("HTTP_READ_TIMEOUT", "3s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "4s")
	t.Setenv("HTTP_IDLE_TIMEOUT", "5s")
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "6s")
	t.Setenv("HTTP_REQUEST_TIMEOUT", "7s")
	t.Setenv("HTTP_MAX_HEADER_BYTES", "8192")
	t.Setenv("HTTP_MAX_BODY_BYTES", "2048")
	t.Setenv("DB_MAX_OPEN_CONNS", "4")
	t.Setenv("DB_MAX_IDLE_CONNS", "2")
	t.Setenv("DB_CONN_MAX_LIFETIME", "10m")
	t.Setenv("DB_CONN_MAX_IDLE_TIME", "1m")
	t.Setenv("DB_PING_TIMEOUT", "2s")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9090 || cfg.ReadHeaderTimeout != 2*time.Second || cfg.ReadTimeout != 3*time.Second {
		t.Fatalf("http override missed: %+v", cfg)
	}
	if cfg.WriteTimeout != 4*time.Second || cfg.IdleTimeout != 5*time.Second || cfg.ShutdownTimeout != 6*time.Second {
		t.Fatalf("timeout override missed: %+v", cfg)
	}
	if cfg.API.RequestTimeout != 7*time.Second || cfg.MaxHeaderBytes != 8192 || cfg.API.MaxBodyBytes != 2048 {
		t.Fatalf("api override missed: %+v", cfg)
	}
	if cfg.DB.MaxOpenConns != 4 || cfg.DB.MaxIdleConns != 2 {
		t.Fatalf("pool override missed: %+v", cfg.DB)
	}
	if cfg.DB.ConnMaxLifetime != 10*time.Minute || cfg.DB.ConnMaxIdleTime != time.Minute || cfg.DB.PingTimeout != 2*time.Second {
		t.Fatalf("db duration override missed: %+v", cfg.DB)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/transactions")
	clearOptional(t)

	t.Run("missing database", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		if _, err := Load(); err == nil {
			t.Fatal("missing DATABASE_URL was accepted")
		}
	})
	t.Run("port", func(t *testing.T) {
		t.Setenv("PORT", "70000")
		if _, err := Load(); err == nil {
			t.Fatal("invalid PORT was accepted")
		}
	})
	t.Run("port not int", func(t *testing.T) {
		t.Setenv("PORT", "abc")
		if _, err := Load(); err == nil {
			t.Fatal("non-integer PORT was accepted")
		}
	})
	t.Run("duration", func(t *testing.T) {
		t.Setenv("HTTP_REQUEST_TIMEOUT", "0s")
		if _, err := Load(); err == nil {
			t.Fatal("zero timeout was accepted")
		}
	})
	t.Run("header bytes", func(t *testing.T) {
		t.Setenv("HTTP_MAX_HEADER_BYTES", "0")
		if _, err := Load(); err == nil {
			t.Fatal("zero header bytes were accepted")
		}
	})
	t.Run("body bytes", func(t *testing.T) {
		t.Setenv("HTTP_MAX_BODY_BYTES", "0")
		if _, err := Load(); err == nil {
			t.Fatal("zero body bytes were accepted")
		}
	})
	t.Run("open conns", func(t *testing.T) {
		t.Setenv("DB_MAX_OPEN_CONNS", "0")
		if _, err := Load(); err == nil {
			t.Fatal("zero open conns were accepted")
		}
	})
	t.Run("idle above open", func(t *testing.T) {
		t.Setenv("DB_MAX_OPEN_CONNS", "2")
		t.Setenv("DB_MAX_IDLE_CONNS", "3")
		if _, err := Load(); err == nil {
			t.Fatal("idle connections above open connections were accepted")
		}
	})
}

func clearOptional(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"PORT",
		"HTTP_READ_HEADER_TIMEOUT",
		"HTTP_READ_TIMEOUT",
		"HTTP_WRITE_TIMEOUT",
		"HTTP_IDLE_TIMEOUT",
		"HTTP_SHUTDOWN_TIMEOUT",
		"HTTP_MAX_HEADER_BYTES",
		"HTTP_MAX_BODY_BYTES",
		"HTTP_REQUEST_TIMEOUT",
		"DB_MAX_OPEN_CONNS",
		"DB_MAX_IDLE_CONNS",
		"DB_CONN_MAX_LIFETIME",
		"DB_CONN_MAX_IDLE_TIME",
		"DB_PING_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}
