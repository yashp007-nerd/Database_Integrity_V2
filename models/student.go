package models

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ── Salt ─────────────────────────────────────────────────────────────────────
// a_secure_salt is the hardcoded cryptographic salt appended to every hash
// computation. Changing this value will invalidate ALL existing integrity
// signatures — treat it like a production secret.
const a_secure_salt = "R0wGu@rd$3cur3S@lt#2024!vSL"

// ── Data Model ───────────────────────────────────────────────────────────────

// Student represents a single ledger row in the `students` table.
// gorm.Model embeds ID (uint, PK), CreatedAt, UpdatedAt, and DeletedAt
// (soft-delete via gorm.DeletedAt — rows are hidden, never physically removed).
type Student struct {
	gorm.Model
	StudentName   string `json:"student_name"   gorm:"column:student_name;not null"`
	SubjectCode   string `json:"subject_code"   gorm:"column:subject_code;not null"`
	Marks         int    `json:"marks"          gorm:"column:marks;not null"`
	HashFootprint string `json:"hash_footprint" gorm:"column:hash_footprint;not null"`
}

// MarshalJSON produces a flat JSON shape identical to the old raw-SQL output
// so the existing frontend continues to work without any changes.
func (s Student) MarshalJSON() ([]byte, error) {
	type Alias struct {
		ID            uint       `json:"id"`
		StudentName   string     `json:"student_name"`
		SubjectCode   string     `json:"subject_code"`
		Marks         int        `json:"marks"`
		HashFootprint string     `json:"hash_footprint"`
		CreatedAt     time.Time  `json:"created_at"`
		UpdatedAt     time.Time  `json:"updated_at"`
		DeletedAt     *time.Time `json:"deleted_at,omitempty"`
	}
	var da *time.Time
	if s.DeletedAt.Valid {
		da = &s.DeletedAt.Time
	}
	return json.Marshal(Alias{
		ID:            s.ID,
		StudentName:   s.StudentName,
		SubjectCode:   s.SubjectCode,
		Marks:         s.Marks,
		HashFootprint: s.HashFootprint,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
		DeletedAt:     da,
	})
}

// ── Ledger Checksum (missing-row / SQL-injection deletion tamper detection) ───

// LedgerChecksum stores a cryptographic snapshot of the entire active table
// state after every write operation (create / update / restore / hard-delete).
//
// During an integrity audit the system:
//  1. Fetches ALL active (non-soft-deleted) rows ordered by ID.
//  2. Recomputes the chain hash over those rows.
//  3. Compares against the most-recent LedgerChecksum snapshot.
//
// If an attacker uses SQL injection to DELETE a row directly (bypassing the
// application), the live count and chain hash will diverge from the snapshot,
// triggering a LEDGER_TAMPERED_ROWS_MISSING event — even though individual
// row hashes are still internally consistent.
//
// Chain hash formula:
//
//	SHA256( "id1:hash1|id2:hash2|…" + salt )
type LedgerChecksum struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	RecordCount int64     `gorm:"column:record_count;not null" json:"record_count"`
	ChainHash   string    `gorm:"column:chain_hash;not null" json:"chain_hash"`
	CreatedAt   time.Time `json:"created_at"`
}

// ── Request / Response DTOs ───────────────────────────────────────────────────

// CreateStudentRequest is the validated payload for POST /api/records.
type CreateStudentRequest struct {
	StudentName string `json:"student_name"`
	SubjectCode string `json:"subject_code"`
	Marks       int    `json:"marks"`
}

// UpdateStudentRequest is the validated payload for PUT /api/records/:id.
type UpdateStudentRequest struct {
	StudentName string `json:"student_name"`
	SubjectCode string `json:"subject_code"`
	Marks       int    `json:"marks"`
}

// ValidationResult carries the outcome of a single-row integrity audit.
type ValidationResult struct {
	ID             uint   `json:"id"`
	StudentName    string `json:"student_name"`
	SubjectCode    string `json:"subject_code"`
	Marks          int    `json:"marks"`
	StoredHash     string `json:"stored_hash"`
	RecomputedHash string `json:"recomputed_hash"`
	Status         string `json:"status"`
	Detail         string `json:"detail"`
}

// AuditReport wraps per-row results with a top-level ledger integrity status
// that catches missing / injected-deleted rows even before inspecting individual
// row hashes.
type AuditReport struct {
	LedgerIntegrity string             `json:"ledger_integrity"`
	LedgerDetail    string             `json:"ledger_detail"`
	Results         []ValidationResult `json:"results"`
}

// ── Hash Engine ──────────────────────────────────────────────────────────────

// ComputeHash derives the canonical SHA-256 integrity footprint for a single row.
//
//	SHA256( student_name | subject_code | marks | salt )
func ComputeHash(studentName, subjectCode string, marks int) string {
	raw := fmt.Sprintf("%s|%s|%d|%s", studentName, subjectCode, marks, a_secure_salt)
	digest := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", digest)
}

// ComputeChainHash computes a deterministic hash over an ordered slice of
// active rows to represent the complete table state at a point in time.
//
//	chain = SHA256( "id1:hash1|id2:hash2|…" + salt )
func ComputeChainHash(rows []Student) string {
	var sb strings.Builder
	for _, s := range rows {
		sb.WriteString(fmt.Sprintf("%d:%s|", s.ID, s.HashFootprint))
	}
	sb.WriteString(a_secure_salt)
	digest := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("%x", digest)
}

// ── SQL-Injection Detection ───────────────────────────────────────────────────

// sqliPatterns covers the most common SQLi attack vectors.
// All patterns are case-insensitive. Matching ANY pattern causes request rejection.
var sqliPatterns = []*regexp.Regexp{
	// Tautology attacks
	regexp.MustCompile(`(?i)\bOR\b\s+['"']?\w+['"']?\s*=\s*['"']?\w+['"']?`),
	regexp.MustCompile(`(?i)\bOR\b\s+\d+\s*=\s*\d+`),
	regexp.MustCompile(`(?i)\bOR\b\s+true\b`),
	regexp.MustCompile(`(?i)\bAND\b\s+\d+\s*=\s*\d+`),
	// UNION-based exfil
	regexp.MustCompile(`(?i)\bUNION\b.*\bSELECT\b`),
	// Comment stripping
	regexp.MustCompile(`--`),
	regexp.MustCompile(`/\*.*?\*/`),
	regexp.MustCompile(`#`),
	// Stacked queries
	regexp.MustCompile(`;`),
	// Blind / time-based
	regexp.MustCompile(`(?i)\bSLEEP\s*\(`),
	regexp.MustCompile(`(?i)\bBENCHMARK\s*\(`),
	regexp.MustCompile(`(?i)\bWAITFOR\s+DELAY\b`),
	regexp.MustCompile(`(?i)\bpg_sleep\s*\(`),
	// DDL & dangerous statements
	regexp.MustCompile(`(?i)\bDROP\s+(TABLE|DATABASE|SCHEMA|INDEX)\b`),
	regexp.MustCompile(`(?i)\bALTER\s+(TABLE|DATABASE)\b`),
	regexp.MustCompile(`(?i)\bTRUNCATE\s+TABLE\b`),
	regexp.MustCompile(`(?i)\bEXEC(UTE)?\s*\(`),
	regexp.MustCompile(`(?i)\bINSERT\s+INTO\b`),
	regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`),
	regexp.MustCompile(`(?i)\bUPDATE\b.+\bSET\b`),
	// Encoding bypass
	regexp.MustCompile(`(?i)0x[0-9a-fA-F]{2,}`),
	// Subquery injection
	regexp.MustCompile(`(?i)\bSELECT\b.+\bFROM\b`),
	// Stored-procedure abuse
	regexp.MustCompile(`(?i)\bxp_cmdshell\b`),
	regexp.MustCompile(`(?i)\bsp_executesql\b`),
	// INFORMATION_SCHEMA / system tables
	regexp.MustCompile(`(?i)\bINFORMATION_SCHEMA\b`),
	regexp.MustCompile(`(?i)\bpg_catalog\b`),
	regexp.MustCompile(`(?i)\bsys\.(tables|columns|objects)\b`),
}

// SQLiSignatureDetail is a structured match report for a single pattern hit.
type SQLiSignatureDetail struct {
	Field   string
	Pattern string
	Match   string
}

// ScanForSQLi checks every supplied field value against sqliPatterns.
// Returns on the first hit; used to gate incoming requests.
func ScanForSQLi(fields map[string]string) (detected bool, matchedPattern string, matchedField string) {
	for fieldName, value := range fields {
		for _, re := range sqliPatterns {
			if loc := re.FindString(value); loc != "" {
				return true, re.String(), fieldName
			}
		}
	}
	return false, "", ""
}

// ScanForSQLiDetailed performs a full scan and returns every pattern hit.
// Used by the /api/records/validate endpoint to categorise stored rows.
func ScanForSQLiDetailed(fields map[string]string) []SQLiSignatureDetail {
	var hits []SQLiSignatureDetail
	for fieldName, value := range fields {
		for _, re := range sqliPatterns {
			if loc := re.FindString(value); loc != "" {
				hits = append(hits, SQLiSignatureDetail{
					Field:   fieldName,
					Pattern: re.String(),
					Match:   loc,
				})
			}
		}
	}
	return hits
}

// ── Integrity Status Constants ────────────────────────────────────────────────

const (
	// StatusSecure — all checks passed, hash footprint is valid.
	StatusSecure = "SECURE_INTEGRITY_MAINTAINED"

	// StatusTamperedSQLi — SQLi signature detected in a stored field value.
	StatusTamperedSQLi = "TAMPERED_VIA_SQLI"

	// StatusTamperedUnauthorised — hash mismatch; row was altered directly in DB.
	StatusTamperedUnauthorised = "TAMPERED_UNAUTHORIZED"

	// StatusTamperedRowDeleted — per-row placeholder when the ledger checksum
	// indicates rows are missing (caught at the ledger level before row iteration).
	StatusTamperedRowDeleted = "TAMPERED_ROW_DELETED"

	// LedgerOK — live chain hash matches the stored snapshot; no rows missing.
	LedgerOK = "LEDGER_INTACT"

	// LedgerTampered — chain hash divergence; one or more rows were deleted
	// directly via SQL without going through the application's soft-delete.
	LedgerTampered = "LEDGER_TAMPERED_ROWS_MISSING"
)

// ── Field Sanitisation ────────────────────────────────────────────────────────

// SanitiseString trims leading/trailing whitespace from a user-supplied string.
// Applied BEFORE the SQLi scan so whitespace-padded payloads are normalised.
func SanitiseString(s string) string {
	return strings.TrimSpace(s)
}
