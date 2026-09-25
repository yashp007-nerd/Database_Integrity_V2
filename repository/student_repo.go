package repository

import (
	"context"
	"fmt"
	"time"

	"vibe-secure-ledger/config"
	"vibe-secure-ledger/models"
)

// timeout wraps every DB call in a reasonable deadline.
const timeout = 5 * time.Second

// ── CREATE ────────────────────────────────────────────────────────────────────

// CreateStudent inserts a new student row and returns the full persisted record.
func CreateStudent(name, subjectCode string, marks int) (*models.Student, error) {
	hash := models.ComputeHash(name, subjectCode, marks)

	sql := `
		INSERT INTO students (student_name, subject_code, marks, hash_footprint)
		VALUES ($1, $2, $3, $4)
		RETURNING id, student_name, subject_code, marks, hash_footprint, created_at, updated_at`

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var s models.Student
	err := config.DB.QueryRow(ctx, sql, name, subjectCode, marks, hash).Scan(
		&s.ID, &s.StudentName, &s.SubjectCode, &s.Marks,
		&s.HashFootprint, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("CreateStudent: %w", err)
	}
	return &s, nil
}

// ── READ ALL ──────────────────────────────────────────────────────────────────

// GetAllStudents fetches every row ordered by ID ascending.
func GetAllStudents() ([]models.Student, error) {
	sql := `
		SELECT id, student_name, subject_code, marks, hash_footprint, created_at, updated_at
		FROM students
		ORDER BY id ASC`

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	rows, err := config.DB.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("GetAllStudents: %w", err)
	}
	defer rows.Close()

	var students []models.Student
	for rows.Next() {
		var s models.Student
		if err := rows.Scan(
			&s.ID, &s.StudentName, &s.SubjectCode, &s.Marks,
			&s.HashFootprint, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("GetAllStudents scan: %w", err)
		}
		students = append(students, s)
	}
	return students, rows.Err()
}

// ── READ ONE ──────────────────────────────────────────────────────────────────

// GetStudentByID fetches a single row by primary key.
func GetStudentByID(id int) (*models.Student, error) {
	sql := `
		SELECT id, student_name, subject_code, marks, hash_footprint, created_at, updated_at
		FROM students WHERE id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var s models.Student
	err := config.DB.QueryRow(ctx, sql, id).Scan(
		&s.ID, &s.StudentName, &s.SubjectCode, &s.Marks,
		&s.HashFootprint, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("GetStudentByID(%d): %w", id, err)
	}
	return &s, nil
}

// ── UPDATE ────────────────────────────────────────────────────────────────────

// UpdateStudent modifies mutable fields and recomputes the hash footprint.
func UpdateStudent(id int, name, subjectCode string, marks int) (*models.Student, error) {
	hash := models.ComputeHash(name, subjectCode, marks)

	sql := `
		UPDATE students
		SET student_name = $1, subject_code = $2, marks = $3, hash_footprint = $4, updated_at = NOW()
		WHERE id = $5
		RETURNING id, student_name, subject_code, marks, hash_footprint, created_at, updated_at`

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var s models.Student
	err := config.DB.QueryRow(ctx, sql, name, subjectCode, marks, hash, id).Scan(
		&s.ID, &s.StudentName, &s.SubjectCode, &s.Marks,
		&s.HashFootprint, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("UpdateStudent(%d): %w", id, err)
	}
	return &s, nil
}

// ── DELETE ────────────────────────────────────────────────────────────────────

// DeleteStudent removes a row permanently by primary key.
// Returns an error if the row does not exist.
func DeleteStudent(id int) error {
	sql := `DELETE FROM students WHERE id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	tag, err := config.DB.Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("DeleteStudent(%d): %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("DeleteStudent(%d): no row found", id)
	}
	return nil
}

// ── VALIDATE ALL ──────────────────────────────────────────────────────────────

// ValidateAllStudents fetches every row and audits each one's hash integrity
// and SQLi signature presence, returning a slice of ValidationResult.
func ValidateAllStudents() ([]models.ValidationResult, error) {
	students, err := GetAllStudents()
	if err != nil {
		return nil, err
	}

	results := make([]models.ValidationResult, 0, len(students))
	for _, s := range students {
		result := auditRow(s)
		results = append(results, result)
	}
	return results, nil
}

// auditRow performs the three-phase integrity check on a single Student row:
//  1. SQLi pattern scan on all string fields → TAMPERED_VIA_SQLI
//  2. Hash recomputation and comparison     → TAMPERED_UNAUTHORIZED
//  3. Both clean                            → SECURE_INTEGRITY_MAINTAINED
func auditRow(s models.Student) models.ValidationResult {
	recomputed := models.ComputeHash(s.StudentName, s.SubjectCode, s.Marks)

	result := models.ValidationResult{
		ID:             s.ID,
		StudentName:    s.StudentName,
		SubjectCode:    s.SubjectCode,
		Marks:          s.Marks,
		StoredHash:     s.HashFootprint,
		RecomputedHash: recomputed,
	}

	// Phase 1 – SQLi scan on live field values
	fields := map[string]string{
		"student_name": s.StudentName,
		"subject_code": s.SubjectCode,
	}
	hits := models.ScanForSQLiDetailed(fields)
	if len(hits) > 0 {
		detail := fmt.Sprintf("SQLi signature detected in field '%s' — pattern: %s — matched: %q",
			hits[0].Field, hits[0].Pattern, hits[0].Match)
		result.Status = models.StatusTamperedSQLi
		result.Detail = detail
		return result
	}

	// Phase 2 – Hash comparison
	if s.HashFootprint != recomputed {
		result.Status = models.StatusTamperedUnauthorised
		result.Detail = fmt.Sprintf(
			"Hash mismatch: stored=%s recomputed=%s — row was modified directly in the database.",
			s.HashFootprint, recomputed,
		)
		return result
	}

	result.Status = models.StatusSecure
	result.Detail = "All integrity checks passed. Hash footprint is valid."
	return result
}
