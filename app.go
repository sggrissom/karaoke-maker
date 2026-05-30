package karaoke

import (
	"encoding/json"
	"fmt"
	"io"
	"karaoke/backend"
	"karaoke/cfg"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

	app.HandleFunc("POST /upload", func(w http.ResponseWriter, r *http.Request) {
		uploadAudio(app.DB, w, r)
	})

	return app
}

var allowedAudioExts = map[string]bool{
	".mp3": true, ".wav": true, ".flac": true,
	".ogg": true, ".m4a": true, ".aac": true, ".opus": true,
}

func uploadAudio(db *vbolt.DB, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 500<<20) // 500 MB
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "request too large or malformed", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "missing audio field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedAudioExts[ext] {
		http.Error(w, "unsupported file type; allowed: mp3, wav, flac, ogg, m4a, aac, opus", http.StatusBadRequest)
		return
	}

	// Sanitize filename: strip path separators, keep everything else.
	baseName := strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	safeName := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, baseName)
	if safeName == "" {
		safeName = "upload"
	}

	pitchShift, _ := strconv.Atoi(r.FormValue("pitchShift"))
	speedAdjust, _ := strconv.ParseFloat(r.FormValue("speedAdjust"), 64)
	if speedAdjust == 0 {
		speedAdjust = 1.0
	}

	id := fmt.Sprintf("%020d", time.Now().UnixNano())
	jobDir := filepath.Join(cfg.JobsDir, id)
	if mkErr := os.MkdirAll(jobDir, 0755); mkErr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	audioPath := filepath.Join(jobDir, safeName+ext)
	dst, createErr := os.Create(audioPath)
	if createErr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, copyErr := io.Copy(dst, file); copyErr != nil {
		dst.Close()
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	dst.Close()

	job := backend.Job{
		ID:          id,
		Title:       safeName,
		AudioFile:   audioPath,
		PitchShift:  pitchShift,
		SpeedAdjust: speedAdjust,
		Status:      backend.StatusQueued,
		CreatedAt:   time.Now(),
	}

	vbolt.WithWriteTx(db, func(tx *vbolt.Tx) {
		vbolt.Write(tx, backend.JobBucket, id, &job)
		vbolt.TxCommit(tx)
	})

	backend.EnqueueJob(id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"JobID": id})
}

func serveJobFile(db *vbolt.DB, w http.ResponseWriter, r *http.Request) {
	// URL pattern: /jobs/{id}/{filename}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/jobs/"), "/")
	if len(parts) != 2 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jobID, filename := parts[0], parts[1]
	allowed := map[string]bool{
		"no_vocals.wav": true, "vocals.wav": true,
		"no_vocals.mp3": true, "vocals.mp3": true,
	}
	if !allowed[filename] {
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
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	http.ServeFile(w, r, filePath)
}
