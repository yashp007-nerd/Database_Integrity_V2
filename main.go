package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/julienschmidt/httprouter"

	"vibe-secure-ledger/config"
	"vibe-secure-ledger/controllers"
	"vibe-secure-ledger/models"
)

//go:embed web
var webFS embed.FS

func main() {
	// ── Database ─────────────────────────────────────────────────────────────
	// AutoMigrate runs on startup: creates / alters the students table (adding
	// the deleted_at column for soft-delete) and creates ledger_checksums.
	config.ConnectDB(
		&models.Student{},
		&models.LedgerChecksum{},
	)
	defer config.CloseDB()

	// ── Router ───────────────────────────────────────────────────────────────
	router := httprouter.New()

	// ── RESTful API routes (/student/*) ──────────────────────────────────────
	//
	// Resource: /student
	//   GET    /student/all            → List all active records (verify integrity)
	//   POST   /student/create         → Add a new record and generate initial hash
	//   PATCH  /student/update/:id     → Modify a record and refresh hash/tampered status
	//   DELETE /student/delete/:id     → Soft-delete a record (move to trash)
	//
	// Trash / Recycle-bin:
	//   GET    /student/trash          → List all trashed (soft-deleted) records
	//   POST   /student/restore/:id    → Restore a trashed record by ID
	//
	// Integrity audit:
	//   POST   /student/validate       → Full ledger + per-row integrity audit

	router.GET("/student/all", controllers.GetAllStudents)
	router.POST("/student/create", controllers.CreateStudent)
	router.PATCH("/student/update/:id", controllers.UpdateStudent)
	router.DELETE("/student/delete/:id", controllers.DeleteStudent)
	router.GET("/student/trash", controllers.GetTrashedStudents)
	router.POST("/student/restore/:id", controllers.RestoreStudent)
	router.POST("/student/validate", controllers.ValidateStudents)

	// Static frontend — serve the embedded web/ subtree
	webSubFS, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("[main] Failed to sub embed FS: %v", err)
	}
	fileServer := http.FileServer(http.FS(webSubFS))
	router.NotFound = fileServer // catches GET / and any static asset

	// ── CORS middleware (dev convenience) ────────────────────────────────────
	handler := corsMiddleware(router)

	// ── Server ───────────────────────────────────────────────────────────────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("[main] RowGuard running → http://localhost:%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[main] Server error: %v", err)
		}
	}()

	<-quit
	log.Println("[main] Shutdown signal received. Draining connections…")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("[main] Forced shutdown: %v", err)
	}
	log.Println("[main] Server exited cleanly.")
}

// corsMiddleware adds permissive CORS headers so the Vite dev proxy (or any
// localhost frontend) can reach the API during development.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
