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

// ── POST /api/students ────────────────────────────────────────────────────────

func CreateStudent(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req models.CreateStudentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload.")
		return
	}

	// Field presence validation
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

// ── GET /api/students ─────────────────────────────────────────────────────────

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

// ── PUT /api/students/:id ─────────────────────────────────────────────────────

func UpdateStudent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id, err := strconv.Atoi(ps.ByName("id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid student ID.")
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

	// SQLi guard on update inputs
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

// ── DELETE /api/students/:id ──────────────────────────────────────────────────

func DeleteStudent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id, err := strconv.Atoi(ps.ByName("id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid student ID.")
		return
	}

	if err := repository.DeleteStudent(id); err != nil {
		writeError(w, http.StatusNotFound, "Record not found: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Record deleted successfully."})
}

// ── POST /api/students/validate ───────────────────────────────────────────────

func ValidateStudents(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	results, err := repository.ValidateAllStudents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Validation failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}
