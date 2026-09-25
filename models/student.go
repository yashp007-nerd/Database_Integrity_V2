package models

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ── Salt ─────────────────────────────────────────────────────────────────────
// a_secure_salt is the hardcoded cryptographic salt appended to every hash
// computation. Changing this value will invalidate ALL existing integrity
// signatures — treat it like a production secret.
const a_secure_salt = "R0wGu@rd$3cur3S@lt#2024!vSL"

// ── Data Model ───────────────────────────────────────────────────────────────

// Student represents a single ledger row in the `students` table.
type Student struct {
	ID           int       `json:"id"`
	StudentName  string    `json:"student_name"`
	SubjectCode  string    `json:"subject_code"`
	Marks        int       `json:"marks"`
	HashFootprint string   `json:"hash_footprint"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ValidationResult carries the outcome of a single-row integrity audit.
type ValidationResult struct {
	ID            int    `json:"id"`
	StudentName   string `json:"student_name"`
	SubjectCode   string `json:"subject_code"`
	Marks         int    `json:"marks"`
	StoredHash    string `json:"stored_hash"`
	RecomputedHash string `json:"recomputed_hash"`
	Status        string `json:"status"`
	Detail        string `json:"detail"`
}

// CreateStudentRequest is the validated payload for POST /api/students.
type CreateStudentRequest struct {
	StudentName string `json:"student_name"`
	SubjectCode string `json:"subject_code"`
	Marks       int    `json:"marks"`
}

// UpdateStudentRequest is the validated payload for PUT /api/students/:id.
type UpdateStudentRequest struct {
	StudentName string `json:"student_name"`
	SubjectCode string `json:"subject_code"`
	Marks       int    `json:"marks"`
}

// ── Hash Engine ──────────────────────────────────────────────────────────────

// ComputeHash derives the canonical SHA-256 integrity footprint for a row.
//
// The hash input concatenates the student's three mutable fields with the
// shared salt using a pipe delimiter to prevent accidental collisions caused
// by adjacent string values (e.g. "AB" + "C" ≠ "A" + "BC").
//
//	SHA256( student_name | subject_code | marks | salt )
func ComputeHash(studentName, subjectCode string, marks int) string {
	raw := fmt.Sprintf("%s|%s|%d|%s", studentName, subjectCode, marks, a_secure_salt)
	digest := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", digest)
}

// ── SQL-Injection Detection ───────────────────────────────────────────────────
//
// sqliPatterns covers the most common SQLi attack vectors as compiled regular
// expressions. All patterns are case-insensitive. Matching ANY pattern on a
// user-supplied string causes the entire request to be rejected.
//
// Pattern catalogue:
//   - Tautology attacks     : OR 1=1, OR 'a'='a', OR true
//   - UNION-based exfil     : UNION SELECT, UNION ALL SELECT
//   - Comment stripping     : -- (inline) and /* */ (block)
//   - Stacked queries       : trailing semicolons triggering a second statement
//   - Sleep / benchmark     : time-based blind injection probes
//   - DROP / ALTER / EXEC   : DDL and stored-procedure invocation
//   - Hex encoding bypass   : 0x… literals used to smuggle strings
//   - Subquery injection    : SELECT inside parentheses
//   - xp_cmdshell           : MSSQL OS command execution
var sqliPatterns = []*regexp.Regexp{
	// Tautology
	regexp.MustCompile(`(?i)\bOR\b\s+['"]?\w+['"]?\s*=\s*['"]?\w+['"]?`),
	regexp.MustCompile(`(?i)\bOR\b\s+\d+\s*=\s*\d+`),
	regexp.MustCompile(`(?i)\bOR\b\s+true\b`),
	regexp.MustCompile(`(?i)\bAND\b\s+\d+\s*=\s*\d+`),

	// UNION-based
	regexp.MustCompile(`(?i)\bUNION\b.*\bSELECT\b`),

	// Comment stripping
	regexp.MustCompile(`--`),
	regexp.MustCompile(`/\*.*?\*/`),
	regexp.MustCompile(`#`), // MySQL single-line comment

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

// SQLiSignatureDetail is a structured match report returned when injection is
// detected during the post-hoc validation audit on stored data.
type SQLiSignatureDetail struct {
	Field   string
	Pattern string
	Match   string
}

// ScanForSQLi checks every supplied field value against sqliPatterns.
// It returns a human-readable description of the first matched pattern,
// or an empty string if the input is clean.
//
// Usage: pass every user-supplied string field; reject the request on any hit.
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

// ScanForSQLiDetailed performs a full scan across all fields and returns every
// pattern hit. Used by the /validate endpoint to categorise stored rows as
// TAMPERED_VIA_SQLI when raw database data contains injection signatures.
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
	// StatusSecure indicates that the recomputed hash matches the stored hash
	// and no SQLi patterns were detected in the live row data.
	StatusSecure = "SECURE_INTEGRITY_MAINTAINED"

	// StatusTamperedSQLi indicates that one or more SQLi signature patterns
	// were detected in the stored row fields, pointing to direct database
	// manipulation via injection.
	StatusTamperedSQLi = "TAMPERED_VIA_SQLI"

	// StatusTamperedUnauthorised indicates that the stored hash does NOT match
	// the recomputed hash, meaning the row was altered directly in the database
	// without going through the application's hash-generation pipeline.
	StatusTamperedUnauthorised = "TAMPERED_UNAUTHORIZED"
)

// ── Field Sanitisation ────────────────────────────────────────────────────────

// SanitiseString trims leading/trailing whitespace from a user-supplied string.
// This is applied BEFORE the SQLi scan so that whitespace-padded payloads are
// normalised prior to pattern matching.
func SanitiseString(s string) string {
	return strings.TrimSpace(s)
}
