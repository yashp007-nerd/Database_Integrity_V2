package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB is the global GORM database handle shared across all repository operations.
var DB *gorm.DB

// ConnectDB reads connection parameters from environment variables,
// opens a GORM/pgx connection pool with sane production defaults, runs
// AutoMigrate to keep the schema current, and validates the pool with
// a ping before returning.
//
// Environment variables consumed:
//
//	PGHOST     – PostgreSQL host          (default: localhost)
//	PGPORT     – PostgreSQL port          (default: 5433 for Docker dev)
//	PGUSER     – Database user            (default: myuser for Docker dev)
//	PGPASSWORD – Database password        (default: mypassword for Docker dev)
//	PGDATABASE – Target database name     (default: rowguard_db for Docker dev)
//	PGSSLMODE  – SSL mode                 (default: disable)
func ConnectDB(models ...interface{}) {
	// Fallbacks match the local Docker Compose setup
	host := envOrDefault("PGHOST", "localhost")
	port := envOrDefault("PGPORT", "5433")
	user := envOrDefault("PGUSER", "myuser")
	password := envOrDefault("PGPASSWORD", "mypassword")
	dbname := envOrDefault("PGDATABASE", "rowguard_db")
	sslmode := envOrDefault("PGSSLMODE", "disable")

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=Asia/Kolkata",
		host, port, user, password, dbname, sslmode,
	)

	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	}

	db, err := gorm.Open(postgres.Open(dsn), gormCfg)
	if err != nil {
		log.Fatalf("[config] Failed to connect to database: %v", err)
	}

	// ── Pool tuning via the underlying *sql.DB ────────────────────────────────
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("[config] Failed to get underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	// Validate the pool is reachable before accepting traffic.
	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("[config] Database ping failed: %v", err)
	}

	// ── Auto-migrate all registered models ───────────────────────────────────
	if len(models) > 0 {
		if err := db.AutoMigrate(models...); err != nil {
			log.Fatalf("[config] AutoMigrate failed: %v", err)
		}
		log.Printf("[config] AutoMigrate completed for %d model(s).", len(models))
	}

	DB = db
	log.Printf("[config] GORM/PostgreSQL pool established → %s:%s/%s (maxConns=20)", host, port, dbname)
}

// CloseDB gracefully drains the underlying connection pool.
// Call this via defer in main() to ensure clean shutdown.
func CloseDB() {
	if DB != nil {
		sqlDB, err := DB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
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
