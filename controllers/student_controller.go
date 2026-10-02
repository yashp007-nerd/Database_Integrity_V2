package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/julienschmidt/httprouter"

	"vibe-secure-ledger/models"
	"vibe-secure-ledger/repository"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseUintParam(ps httprouter.Params, name string) (uint, bool) {
	v, err := strconv.ParseUint(ps.ByName(name), 10, 64)
	if err != nil || v == 0 {
		return 0, false
	}
	return uint(v), true
}

// ── POST /student/create ──────────────────────────────────────────────────────

func CreateStudent(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req models.CreateStudentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload.")
		return
	}

	req.StudentName = models.SanitiseString(req.StudentName)
	req.SubjectCode = models.SanitiseString(req.SubjectCode)

	if req.StudentName == "" {
		writeError(w, http.StatusBadRequest, "student_name is required.")
		return
	}
	if req.SubjectCode == "" {
		writeError(w, http.StatusBadRequest, "subject_code is required.")
		return
	}
	if req.Marks < 0 || req.Marks > 100 {
		writeError(w, http.StatusBadRequest, "marks must be between 0 and 100.")
		return
	}

	// Aggressive SQLi scan on all string inputs
	fields := map[string]string{
		"student_name": req.StudentName,
		"subject_code": req.SubjectCode,
	}
	if detected, pattern, field := models.ScanForSQLi(fields); detected {
		writeError(w, http.StatusBadRequest,
			"SQL injection pattern detected in field '"+field+"'. Pattern: "+pattern)
		return
	}

	student, err := repository.CreateStudent(req.StudentName, req.SubjectCode, req.Marks)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create record: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, student)
}

// ── GET /student/all ──────────────────────────────────────────────────────────

func GetAllStudents(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	students, err := repository.GetAllStudents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to fetch records: "+err.Error())
		return
	}

	// Return empty array instead of null for clean frontend consumption
	if students == nil {
		students = []models.Student{}
	}
	writeJSON(w, http.StatusOK, students)
}

// ── PATCH /student/update/:id ─────────────────────────────────────────────────

func UpdateStudent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id, ok := parseUintParam(ps, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid record ID.")
		return
	}

	var req models.UpdateStudentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload.")
		return
	}

	req.StudentName = models.SanitiseString(req.StudentName)
	req.SubjectCode = models.SanitiseString(req.SubjectCode)

	if req.StudentName == "" {
		writeError(w, http.StatusBadRequest, "student_name is required.")
		return
	}
	if req.SubjectCode == "" {
		writeError(w, http.StatusBadRequest, "subject_code is required.")
		return
	}
	if req.Marks < 0 || req.Marks > 100 {
		writeError(w, http.StatusBadRequest, "marks must be between 0 and 100.")
		return
	}

	fields := map[string]string{
		"student_name": req.StudentName,
		"subject_code": req.SubjectCode,
	}
	if detected, pattern, field := models.ScanForSQLi(fields); detected {
		writeError(w, http.StatusBadRequest,
			"SQL injection pattern detected in field '"+field+"'. Pattern: "+pattern)
		return
	}

	student, err := repository.UpdateStudent(id, req.StudentName, req.SubjectCode, req.Marks)
	if err != nil {
		writeError(w, http.StatusNotFound, "Record not found or update failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, student)
}

// ── DELETE /student/delete/:id (soft-delete → trash) ─────────────────────────

func DeleteStudent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id, ok := parseUintParam(ps, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid record ID.")
		return
	}

	if err := repository.DeleteStudent(id); err != nil {
		writeError(w, http.StatusNotFound, "Record not found: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Record moved to trash. Use the restore endpoint to recover it.",
	})
}

// ── GET /student/trash ────────────────────────────────────────────────────────

func GetTrashedStudents(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	students, err := repository.GetTrashedStudents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to fetch trash: "+err.Error())
		return
	}
	if students == nil {
		students = []models.Student{}
	}
	writeJSON(w, http.StatusOK, students)
}

// ── POST /student/restore/:id ─────────────────────────────────────────────────

func RestoreStudent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id, ok := parseUintParam(ps, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid record ID.")
		return
	}

	student, err := repository.RestoreStudent(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Restore failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "Record restored successfully.",
		"record":  student,
	})
}

// ── POST /student/validate ────────────────────────────────────────────────────

func ValidateStudents(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	report, err := repository.ValidateAllStudents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Validation failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}
