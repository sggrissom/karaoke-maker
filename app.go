package karaoke

import (
	"karaoke/backend"
	"karaoke/cfg"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"go.hasen.dev/vbeam"
	"go.hasen.dev/vbolt"
)

func OpenDB(dbpath string) *vbolt.DB {
	return vbolt.Open(dbpath)
}

func MakeApplication() *vbeam.Application {
	_ = godotenv.Load()

	if os.Getenv("PROD") == "true" || os.Getenv("ENVIRONMENT") == "production" {
		_ = godotenv.Load("/srv/apps/karaoke/shared/.env")
	}
	cfg.ApplyEnv()

	db := OpenDB(cfg.DBPath)
	app := vbeam.NewApplication("KaraokeMaker", db)

	app.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	backend.RegisterMethods(app)

	app.HandleFunc("GET /jobs/", func(w http.ResponseWriter, r *http.Request) {
		serveJobFile(app.DB, w, r)
	})

	return app
}

func serveJobFile(db *vbolt.DB, w http.ResponseWriter, r *http.Request) {
	// URL pattern: /jobs/{id}/{filename}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/jobs/"), "/")
	if len(parts) != 2 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jobID, filename := parts[0], parts[1]
	if filename != "no_vocals.wav" && filename != "vocals.wav" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var job backend.Job
	vbolt.WithReadTx(db, func(tx *vbolt.Tx) {
		vbolt.Read(tx, backend.JobBucket, jobID, &job)
	})
	if job.Status != backend.StatusDone {
		http.Error(w, "not ready", http.StatusNotFound)
		return
	}

	filePath := filepath.Join(cfg.JobsDir, jobID, "htdemucs", job.Title, filename)
	http.ServeFile(w, r, filePath)
}
