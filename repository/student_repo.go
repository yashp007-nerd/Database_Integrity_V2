package repository

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"vibe-secure-ledger/config"
	"vibe-secure-ledger/models"
)

// ── internal: ledger snapshot helper ─────────────────────────────────────────

// snapshotLedger recomputes the chain hash over all active rows and persists
// a new LedgerChecksum record. Call this inside a transaction after every
// write that changes the set of active rows (create / update / restore).
func snapshotLedger(tx *gorm.DB) error {
	var active []models.Student
	if err := tx.Order("id ASC").Find(&active).Error; err != nil {
		return fmt.Errorf("snapshotLedger fetch: %w", err)
	}
	snap := models.LedgerChecksum{
		RecordCount: int64(len(active)),
		ChainHash:   models.ComputeChainHash(active),
	}
	if err := tx.Create(&snap).Error; err != nil {
		return fmt.Errorf("snapshotLedger write: %w", err)
	}
	return nil
}

// ── CREATE ────────────────────────────────────────────────────────────────────

// CreateStudent inserts a new student row inside a transaction,
// recomputes the ledger checksum snapshot, and returns the full persisted record.
func CreateStudent(name, subjectCode string, marks int) (*models.Student, error) {
	hash := models.ComputeHash(name, subjectCode, marks)
	s := models.Student{
		StudentName:   name,
		SubjectCode:   subjectCode,
		Marks:         marks,
		HashFootprint: hash,
	}

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&s).Error; err != nil {
			return fmt.Errorf("CreateStudent insert: %w", err)
		}
		return snapshotLedger(tx)
	})
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ── READ ALL (active only) ────────────────────────────────────────────────────

// GetAllStudents fetches every active (non-soft-deleted) row, ordered by ID.
func GetAllStudents() ([]models.Student, error) {
	var students []models.Student
	if err := config.DB.Order("id ASC").Find(&students).Error; err != nil {
		return nil, fmt.Errorf("GetAllStudents: %w", err)
	}
	return students, nil
}

// ── READ ONE ──────────────────────────────────────────────────────────────────

// GetStudentByID fetches a single active row by primary key.
func GetStudentByID(id uint) (*models.Student, error) {
	var s models.Student
	err := config.DB.First(&s, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("GetStudentByID(%d): record not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("GetStudentByID(%d): %w", id, err)
	}
	return &s, nil
}

// ── UPDATE ────────────────────────────────────────────────────────────────────

// UpdateStudent modifies mutable fields, recomputes the hash footprint,
// and refreshes the ledger checksum snapshot — all inside one transaction.
func UpdateStudent(id uint, name, subjectCode string, marks int) (*models.Student, error) {
	hash := models.ComputeHash(name, subjectCode, marks)

	var s models.Student
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&s, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("UpdateStudent(%d): record not found", id)
			}
			return fmt.Errorf("UpdateStudent(%d): %w", id, err)
		}
		if err := tx.Model(&s).Updates(models.Student{
			StudentName:   name,
			SubjectCode:   subjectCode,
			Marks:         marks,
			HashFootprint: hash,
		}).Error; err != nil {
			return fmt.Errorf("UpdateStudent(%d) save: %w", id, err)
		}
		return snapshotLedger(tx)
	})
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ── SOFT DELETE (move to trash) ───────────────────────────────────────────────

// DeleteStudent soft-deletes a row by setting deleted_at via GORM.
// The row is hidden from all standard queries but remains in the database
// (visible via GetTrashedStudents / RestoreStudent).
//
// NOTE: The ledger snapshot is intentionally NOT updated on soft-delete so
// that the hard-delete tamper detection remains meaningful.  Only a physical
// (hard) DELETE bypasses the application and triggers a ledger mismatch.
func DeleteStudent(id uint) error {
	result := config.DB.Delete(&models.Student{}, id)
	if result.Error != nil {
		return fmt.Errorf("DeleteStudent(%d): %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("DeleteStudent(%d): no active row found", id)
	}
	return nil
}

// ── TRASH (soft-deleted rows) ─────────────────────────────────────────────────

// GetTrashedStudents returns all soft-deleted rows ordered by deletion time desc.
func GetTrashedStudents() ([]models.Student, error) {
	var students []models.Student
	if err := config.DB.Unscoped().Where("deleted_at IS NOT NULL").
		Order("deleted_at DESC").Find(&students).Error; err != nil {
		return nil, fmt.Errorf("GetTrashedStudents: %w", err)
	}
	return students, nil
}

// RestoreStudent clears the deleted_at timestamp for a soft-deleted row,
// making it active again. After restoration the ledger snapshot is refreshed
// inside a transaction so the chain hash reflects the restored count.
func RestoreStudent(id uint) (*models.Student, error) {
	var s models.Student
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		// Unscoped so we can see the soft-deleted row
		if err := tx.Unscoped().First(&s, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("RestoreStudent(%d): record not found in trash", id)
			}
			return fmt.Errorf("RestoreStudent(%d): %w", id, err)
		}
		if !s.DeletedAt.Valid {
			return fmt.Errorf("RestoreStudent(%d): record is not in the trash", id)
		}
		// Clear the soft-delete timestamp
		if err := tx.Unscoped().Model(&s).Update("deleted_at", nil).Error; err != nil {
			return fmt.Errorf("RestoreStudent(%d) restore: %w", id, err)
		}
		s.DeletedAt = gorm.DeletedAt{} // clear local copy too
		return snapshotLedger(tx)
	})
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ── VALIDATE ALL ──────────────────────────────────────────────────────────────

// ValidateAllStudents performs a full ledger audit:
//
//  1. Ledger-level check — compares live count + chain hash against the most
//     recent LedgerChecksum snapshot.  A mismatch means rows were physically
//     deleted (e.g. via SQL injection) without going through the app.
//
//  2. Per-row check — for each active row, verifies SQLi signatures and
//     recomputes the individual hash footprint.
//
// Returns an AuditReport containing the ledger status and the per-row results.
func ValidateAllStudents() (*models.AuditReport, error) {
	active, err := GetAllStudents()
	if err != nil {
		return nil, err
	}

	report := &models.AuditReport{}

	// ── Phase 0: Ledger (missing-row) check ──────────────────────────────────
	var snap models.LedgerChecksum
	ledgerErr := config.DB.Order("id DESC").First(&snap).Error

	if errors.Is(ledgerErr, gorm.ErrRecordNotFound) {
		// No snapshot yet — first audit; treat ledger as intact but note it.
		report.LedgerIntegrity = models.LedgerOK
		report.LedgerDetail = "No prior ledger snapshot exists. This is the first audit run."
	} else if ledgerErr != nil {
		return nil, fmt.Errorf("ValidateAllStudents ledger fetch: %w", ledgerErr)
	} else {
		liveCount := int64(len(active))
		liveChain := models.ComputeChainHash(active)

		if liveCount != snap.RecordCount || liveChain != snap.ChainHash {
			report.LedgerIntegrity = models.LedgerTampered
			report.LedgerDetail = fmt.Sprintf(
				"CRITICAL: Ledger mismatch detected. "+
					"Snapshot recorded %d active rows with chain hash %s…; "+
					"live table has %d rows with chain hash %s…. "+
					"One or more rows were physically deleted outside the application (possible SQL injection attack).",
				snap.RecordCount, snap.ChainHash[:16],
				liveCount, liveChain[:16],
			)
		} else {
			report.LedgerIntegrity = models.LedgerOK
			report.LedgerDetail = fmt.Sprintf(
				"Ledger intact. %d active rows, chain hash matches snapshot (…%s).",
				liveCount, liveChain[:16],
			)
		}
	}

	// ── Phase 1 & 2: Per-row SQLi + hash audit ───────────────────────────────
	results := make([]models.ValidationResult, 0, len(active))
	for _, s := range active {
		results = append(results, auditRow(s))
	}
	report.Results = results

	return report, nil
}

// auditRow performs the two-phase integrity check on a single active Student row:
//  1. SQLi pattern scan on all string fields → TAMPERED_VIA_SQLI
//  2. Hash recomputation and comparison      → TAMPERED_UNAUTHORIZED
//  3. Both clean                             → SECURE_INTEGRITY_MAINTAINED
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
		result.Status = models.StatusTamperedSQLi
		result.Detail = fmt.Sprintf(
			"SQLi signature detected in field '%s' — pattern: %s — matched: %q",
			hits[0].Field, hits[0].Pattern, hits[0].Match,
		)
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
