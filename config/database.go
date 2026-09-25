package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is the global connection pool shared across all repository operations.
var DB *pgxpool.Pool

// ConnectDB reads connection parameters from environment variables,
// constructs a pgx connection pool with sane production defaults, and
// validates the pool with a ping before returning.
//
// Environment variables consumed:
//
//	PGHOST     – PostgreSQL host          (default: localhost)
//	PGPORT     – PostgreSQL port          (default: 5432)
//	PGUSER     – Database user            (default: postgres)
//	PGPASSWORD – Database password        (required)
//	PGDATABASE – Target database name     (required)
//	PGSSLMODE  – SSL mode                 (default: disable)
func ConnectDB() {
	host := envOrDefault("PGHOST", "localhost")
	port := envOrDefault("PGPORT", "5432")
	user := envOrDefault("PGUSER", "postgres")
	password := mustEnv("PGPASSWORD")
	dbname := mustEnv("PGDATABASE")
	sslmode := envOrDefault("PGSSLMODE", "disable")

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode,
	)

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("[config] Failed to parse DSN: %v", err)
	}

	// ── Pool tuning ──────────────────────────────────────────────────────────
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 10 * time.Minute
	cfg.HealthCheckPeriod = 1 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Fatalf("[config] Unable to create connection pool: %v", err)
	}

	// Validate the pool is reachable before accepting traffic.
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("[config] Database ping failed: %v", err)
	}

	DB = pool
	log.Printf("[config] PostgreSQL pool established → %s:%s/%s (maxConns=%d)", host, port, dbname, cfg.MaxConns)
}

// CloseDB gracefully drains the connection pool.
// Call this via defer in main() to ensure clean shutdown.
func CloseDB() {
	if DB != nil {
		DB.Close()
		log.Println("[config] PostgreSQL pool closed.")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("[config] Required environment variable %q is not set.", key)
	}
	return v
}
