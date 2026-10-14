// Package config loads process settings from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/caroline-amorim/test-case-pismo/internal/api"
	"github.com/caroline-amorim/test-case-pismo/internal/db/postgres"
)

const (
	maxBodyBytes   = 1 << 20
	maxHeaderBytes = 1 << 20
	maxOpenConns   = 100
)

// Config is the process configuration. Defaults match the local case.
type Config struct {
	DatabaseURL       string
	Port              int
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
	API               api.Settings
	DB                postgres.Settings
}

// Addr is the HTTP listen address.
func (c Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}

// Load reads configuration from the environment.
// DATABASE_URL is required. Every other variable falls back to the default when empty.
func Load() (Config, error) {
	cfg := defaults()

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	port, err := envInt("PORT", cfg.Port)
	if err != nil {
		return Config{}, err
	}
	if port < 1 || port > 65535 {
		return Config{}, errors.New("PORT must be between 1 and 65535")
	}
	cfg.Port = port

	if err := overlayDurations(&cfg); err != nil {
		return Config{}, err
	}
	if err := overlaySizes(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaults() Config {
	return Config{
		Port:              8080,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   10 * time.Second,
		MaxHeaderBytes:    1 << 16,
		API:               api.DefaultSettings(),
		DB:                postgres.DefaultSettings(),
	}
}

func overlayDurations(cfg *Config) error {
	var err error
	if cfg.ReadHeaderTimeout, err = envDuration("HTTP_READ_HEADER_TIMEOUT", cfg.ReadHeaderTimeout); err != nil {
		return err
	}
	if cfg.ReadTimeout, err = envDuration("HTTP_READ_TIMEOUT", cfg.ReadTimeout); err != nil {
		return err
	}
	if cfg.WriteTimeout, err = envDuration("HTTP_WRITE_TIMEOUT", cfg.WriteTimeout); err != nil {
		return err
	}
	if cfg.IdleTimeout, err = envDuration("HTTP_IDLE_TIMEOUT", cfg.IdleTimeout); err != nil {
		return err
	}
	if cfg.ShutdownTimeout, err = envDuration("HTTP_SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return err
	}
	if cfg.API.RequestTimeout, err = envDuration("HTTP_REQUEST_TIMEOUT", cfg.API.RequestTimeout); err != nil {
		return err
	}
	if cfg.DB.ConnMaxLifetime, err = envDuration("DB_CONN_MAX_LIFETIME", cfg.DB.ConnMaxLifetime); err != nil {
		return err
	}
	if cfg.DB.ConnMaxIdleTime, err = envDuration("DB_CONN_MAX_IDLE_TIME", cfg.DB.ConnMaxIdleTime); err != nil {
		return err
	}
	if cfg.DB.PingTimeout, err = envDuration("DB_PING_TIMEOUT", cfg.DB.PingTimeout); err != nil {
		return err
	}
	return nil
}

func overlaySizes(cfg *Config) error {
	header, err := envInt("HTTP_MAX_HEADER_BYTES", cfg.MaxHeaderBytes)
	if err != nil {
		return err
	}
	if header < 1 || header > maxHeaderBytes {
		return errors.New("HTTP_MAX_HEADER_BYTES is out of range")
	}
	cfg.MaxHeaderBytes = header

	body, err := envInt("HTTP_MAX_BODY_BYTES", int(cfg.API.MaxBodyBytes))
	if err != nil {
		return err
	}
	if body < 1 || body > maxBodyBytes {
		return errors.New("HTTP_MAX_BODY_BYTES is out of range")
	}
	cfg.API.MaxBodyBytes = int64(body)

	open, err := envInt("DB_MAX_OPEN_CONNS", cfg.DB.MaxOpenConns)
	if err != nil {
		return err
	}
	if open < 1 || open > maxOpenConns {
		return errors.New("DB_MAX_OPEN_CONNS is out of range")
	}
	cfg.DB.MaxOpenConns = open

	idle, err := envInt("DB_MAX_IDLE_CONNS", cfg.DB.MaxIdleConns)
	if err != nil {
		return err
	}
	if idle < 0 || idle > cfg.DB.MaxOpenConns {
		return errors.New("DB_MAX_IDLE_CONNS is out of range")
	}
	cfg.DB.MaxIdleConns = idle
	return nil
}

func envInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return n, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return d, nil
}
