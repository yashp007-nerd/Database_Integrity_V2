# RowGuard — Product Requirements Document
> **Module:** `vibe-secure-ledger` · **Language:** Go 1.22 · **DB:** PostgreSQL (pgx v5)

---

## 1. Project Overview

**RowGuard** is a tamper-evident student-marks ledger. Every time a record is created or updated through the API, the system computes a SHA-256 cryptographic hash of the row's mutable fields and stores it alongside the data. A dedicated audit endpoint re-derives every hash on demand and classifies each row as one of three integrity states:

| Status | Meaning |
|---|---|
| `SECURE_INTEGRITY_MAINTAINED` | Row is pristine; hash matches and no SQL injection signatures present |
| `TAMPERED_UNAUTHORIZED` | Row was modified directly in the database, bypassing the API |
| `TAMPERED_VIA_SQLI` | Row contains SQL injection signatures indicating a direct-DB attack |

The project is distributed as a **single self-contained binary** (`vibe-secure-ledger.exe`) that embeds the frontend (`web/`) and connects to a PostgreSQL database via environment variables.

---

## 2. Goals & Non-Goals

### Goals
- Provide a REST API for CRUD operations on student records with automatic hash generation.
- Detect and block SQL injection at the API layer (input validation).
- Detect tampered rows at the database layer (post-hoc audit via `/api/students/validate`).
- Serve the embedded frontend as static files from the same binary (no separate web server).
- Graceful shutdown — drain in-flight requests before exit.

### Non-Goals
- Authentication / authorisation (no JWT, sessions, or API keys in scope).
- Multi-tenant or multi-table support (single `students` table).
- Admin UI for managing salts or rotating hash keys.
- Rate limiting or request throttling.

---

## 3. Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        vibe-secure-ledger binary                    │
│                                                                     │
│  ┌──────────┐    ┌─────────────────────┐    ┌──────────────────┐  │
│  │  web/    │    │   httprouter        │    │  CORS Middleware  │  │
│  │ (embed)  │◄───│   (routing layer)   │◄───│  (dev helper)    │  │
│  └──────────┘    └─────────┬───────────┘    └──────────────────┘  │
│                             │                                       │
│                    ┌────────▼────────┐                             │
│                    │   controllers/  │                             │
│                    │ student_ctrl.go │                             │
│                    └────────┬────────┘                             │
│                             │  validates + sanitises input         │
│                    ┌────────▼────────┐                             │
│                    │    models/      │                             │
│                    │  student.go     │  Hash engine + SQLi scan    │
│                    └────────┬────────┘                             │
│                             │  raw DB operations                   │
│                    ┌────────▼────────┐                             │
│                    │   repository/   │                             │
│                    │ student_repo.go │                             │
│                    └────────┬────────┘                             │
│                             │                                       │
│                    ┌────────▼────────┐                             │
│                    │   config/       │                             │
│                    │  database.go    │  pgxpool, env vars          │
│                    └─────────────────┘                             │
└─────────────────────────────────────────────────────────────────────┘
                             │
                     ┌───────▼──────┐
                     │  PostgreSQL  │
                     │  (students)  │
                     └──────────────┘
```

### Layer Responsibilities

| Layer | Package | Responsibility |
|---|---|---|
| Entry point | `main` | Router setup, embed FS, graceful shutdown |
| HTTP handlers | `controllers` | Parse/validate request bodies, write JSON responses |
| Domain logic | `models` | Hash computation, SQLi pattern library, sanitisation |
| Data access | `repository` | SQL queries, hash recomputation for audits |
| Infrastructure | `config` | PostgreSQL connection pool (pgxpool), env var parsing |

---

## 4. Data Model

### `students` table

| Column | Type | Notes |
|---|---|---|
| `id` | `SERIAL PRIMARY KEY` | Auto-incrementing row identifier |
| `student_name` | `TEXT NOT NULL` | Mutable; included in hash |
| `subject_code` | `TEXT NOT NULL` | Mutable; included in hash |
| `marks` | `INTEGER NOT NULL` | Range 0-100; included in hash |
| `hash_footprint` | `TEXT NOT NULL` | SHA-256 hex digest of the three mutable fields |
| `created_at` | `TIMESTAMPTZ` | Set on insert |
| `updated_at` | `TIMESTAMPTZ` | Updated on every `PUT` |

### Hash Formula

```
SHA256( student_name + "|" + subject_code + "|" + marks + "|" + SALT )
```

The pipe (`|`) delimiter prevents collision attacks from adjacent field concatenation.
The salt constant (`R0wGu@rd$3cur3S@lt#2024!vSL`) is compile-time hardcoded — treat it as a production secret.

---

## 5. API Reference

Base URL: `http://localhost:8080`

### 5.1 Create Student
```
POST /api/students
Content-Type: application/json

{
  "student_name": "Alice",
  "subject_code": "CS101",
  "marks": 92
}
```
**Responses**
- `201 Created` — full `Student` object with generated `hash_footprint`
- `400 Bad Request` — missing/invalid fields, marks out of range, or SQLi detected

### 5.2 Get All Students
```
GET /api/students
```
**Responses**
- `200 OK` — JSON array of `Student` objects (empty array `[]` if none)
- `500 Internal Server Error` — database error

### 5.3 Update Student
```
PUT /api/students/:id
Content-Type: application/json

{
  "student_name": "Alice",
  "subject_code": "CS201",
  "marks": 95
}
```
**Responses**
- `200 OK` — updated `Student` object with recomputed `hash_footprint`
- `400 Bad Request` — invalid `id`, invalid payload, or SQLi detected
- `404 Not Found` — student with given `id` does not exist

### 5.4 Delete Student
```
DELETE /api/students/:id
```
**Responses**
- `200 OK` — `{ "message": "Record deleted successfully." }`
- `400 Bad Request` — invalid `id`
- `404 Not Found` — student does not exist

### 5.5 Validate All (Integrity Audit)
```
POST /api/students/validate
```
Fetches all rows, re-derives every hash, and scans stored fields for SQLi patterns.

**Response — `200 OK`**
```json
[
  {
    "id": 1,
    "student_name": "Alice",
    "subject_code": "CS101",
    "marks": 92,
    "stored_hash": "abc123...",
    "recomputed_hash": "abc123...",
    "status": "SECURE_INTEGRITY_MAINTAINED",
    "detail": "All integrity checks passed. Hash footprint is valid."
  }
]
```

---

## 6. Security Design

### 6.1 Input Sanitisation
All user-supplied strings are trimmed with `models.SanitiseString` (leading/trailing whitespace)
before any further processing.

### 6.2 SQL Injection Prevention (Dual Layer)

**Layer 1 — API input scan (`ScanForSQLi`)**
Every `POST`/`PUT` request scans all string fields against 26 compiled regex patterns before the
record reaches the database. A match returns `400 Bad Request` immediately.

**Layer 2 — Post-hoc audit scan (`ScanForSQLiDetailed`)**
`/api/students/validate` runs the same patterns against *stored* data to catch rows that were
injected directly at the database level (bypassing the API).

### 6.3 SQL Injection Pattern Categories

| Category | Example Attack |
|---|---|
| Tautology | `' OR 1=1 --` |
| UNION-based exfiltration | `' UNION SELECT * FROM users` |
| Comment stripping | `--`, `/* */`, `#` |
| Stacked queries | `; DROP TABLE students` |
| Time-based blind | `SLEEP(5)`, `BENCHMARK(...)`, `pg_sleep(...)` |
| DDL / DML abuse | `DROP TABLE`, `ALTER TABLE`, `TRUNCATE`, `INSERT INTO` |
| Encoding bypass | `0x41 42 43` |
| Subquery injection | `(SELECT ... FROM ...)` |
| MSSQL OS commands | `xp_cmdshell`, `sp_executesql` |
| Schema enumeration | `INFORMATION_SCHEMA`, `pg_catalog`, `sys.tables` |

### 6.4 Hash Integrity Audit
On every `PUT`, the hash is **recomputed** with the new values — the old hash is never reused.
The validate endpoint catches out-of-band database modifications by comparing stored vs.
recomputed hashes.

---

## 7. Environment Configuration

| Variable | Default | Required | Description |
|---|---|---|---|
| `PGHOST` | `localhost` | No | PostgreSQL host |
| `PGPORT` | `5432` | No | PostgreSQL port |
| `PGUSER` | `postgres` | No | Database user |
| `PGPASSWORD` | — | **Yes** | Database password |
| `PGDATABASE` | — | **Yes** | Target database name |
| `PGSSLMODE` | `disable` | No | SSL mode |
| `PORT` | `8080` | No | HTTP server listen port |

---

## 8. Database Connection Pool

Managed by `pgxpool` with the following tuning parameters:

| Parameter | Value |
|---|---|
| `MaxConns` | 20 |
| `MinConns` | 2 |
| `MaxConnLifetime` | 30 minutes |
| `MaxConnIdleTime` | 10 minutes |
| `HealthCheckPeriod` | 1 minute |
| Query timeout | 5 seconds (per operation) |
| Connect timeout | 10 seconds |

---

## 9. Server Configuration

| Parameter | Value |
|---|---|
| HTTP read timeout | 10 seconds |
| HTTP write timeout | 15 seconds |
| HTTP idle timeout | 60 seconds |
| Graceful shutdown drain | 10 seconds |
| Shutdown signals | `SIGINT`, `SIGTERM` |

---

## 10. Project Structure

```
RowGuard Golang/
├── main.go                   # Entry point: router, embed FS, server lifecycle
├── go.mod                    # Module: vibe-secure-ledger, Go 1.22
├── go.sum
├── CLAUDE.md                 # This document (PRD)
│
├── config/
│   └── database.go           # pgxpool setup, env var helpers
│
├── controllers/
│   └── student_controller.go # HTTP handlers (Create, GetAll, Update, Delete, Validate)
│
├── models/
│   └── student.go            # Domain structs, ComputeHash, SQLi patterns & scanners
│
├── repository/
│   └── student_repo.go       # SQL CRUD + ValidateAllStudents / auditRow
│
└── web/                      # Embedded static frontend (served by the binary)
```

---

## 11. Key Dependencies

| Package | Version | Purpose |
|---|---|---|
| `github.com/jackc/pgx/v5` | v5.6.0 | PostgreSQL driver + connection pool |
| `github.com/julienschmidt/httprouter` | v1.3.0 | Lightweight, high-performance HTTP router |
| `golang.org/x/crypto` | v0.17.0 | Indirect (pgx dependency) |

---

## 12. Build & Run

### Prerequisites
- Go 1.22+
- PostgreSQL instance (local or remote)

### Setup (PowerShell)
```powershell
# Set required environment variables
$env:PGPASSWORD = "your_password"
$env:PGDATABASE = "rowguard"

# Optional overrides
$env:PGHOST = "localhost"
$env:PGPORT = "5432"
$env:PGUSER = "postgres"
$env:PORT   = "8080"

# Run in dev
go run ./main.go

# Build and run
go build -o vibe-secure-ledger.exe .
./vibe-secure-ledger.exe
```

### Expected Database Schema
```sql
CREATE TABLE IF NOT EXISTS students (
    id             SERIAL PRIMARY KEY,
    student_name   TEXT        NOT NULL,
    subject_code   TEXT        NOT NULL,
    marks          INTEGER     NOT NULL CHECK (marks BETWEEN 0 AND 100),
    hash_footprint TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 13. Development Guidelines

### Code Conventions
- All exported functions must have Go doc comments.
- Use structured error wrapping: `fmt.Errorf("FunctionName: %w", err)`.
- Use `context.WithTimeout` for all database operations (5 s default).
- Return empty `[]T{}` instead of `nil` slices in JSON responses.
- Sanitise all user strings with `models.SanitiseString` before any validation.

### Adding New Features
1. Define domain structs / constants in `models/`.
2. Add SQL operations in `repository/`.
3. Add HTTP handlers in `controllers/`.
4. Register routes in `main.go`.
5. If new user-supplied string fields are added, include them in the `fields` map passed to `ScanForSQLi`.

### Security Checklist (for every new endpoint)
- [ ] Sanitise all string inputs (`SanitiseString`)
- [ ] Validate field presence and type constraints
- [ ] Run `ScanForSQLi` on all user-supplied string fields
- [ ] Use parameterised queries in repository (no string interpolation in SQL)
- [ ] Wrap DB calls in `context.WithTimeout`

---

## 14. Integrity Status Reference

| Constant | Value | Trigger |
|---|---|---|
| `StatusSecure` | `SECURE_INTEGRITY_MAINTAINED` | Hash matches, no SQLi signatures |
| `StatusTamperedSQLi` | `TAMPERED_VIA_SQLI` | SQLi signature found in stored field values |
| `StatusTamperedUnauthorised` | `TAMPERED_UNAUTHORIZED` | Stored hash != recomputed hash |

> **Note:** The SQLi check runs **before** the hash check. A row that both has SQLi patterns
> and a hash mismatch will be classified as `TAMPERED_VIA_SQLI`.
