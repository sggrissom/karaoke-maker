package backend

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.hasen.dev/vbolt"
	"karaoke/cfg"
)

var jobQueue chan string

func StartWorker(db *vbolt.DB) {
	jobQueue = make(chan string, 100)
	go runWorker(db)
}

func EnqueueJob(id string) {
	jobQueue <- id
}

func runWorker(db *vbolt.DB) {
	for id := range jobQueue {
		processJob(db, id)
	}
}

func updateJob(db *vbolt.DB, id string, fn func(*Job)) {
	vbolt.WithWriteTx(db, func(tx *vbolt.Tx) {
		var job Job
		if vbolt.Read(tx, JobBucket, id, &job) {
			fn(&job)
			vbolt.Write(tx, JobBucket, id, &job)
		}
		vbolt.TxCommit(tx)
	})
}

func processJob(db *vbolt.DB, id string) {
	var job Job
	vbolt.WithReadTx(db, func(tx *vbolt.Tx) {
		vbolt.Read(tx, JobBucket, id, &job)
	})
	if job.ID == "" {
		log.Println("worker: job not found:", id)
		return
	}

	log.Printf("worker: starting job %s: %s", id, job.URL)
	jobDir := filepath.Join(cfg.JobsDir, id)
	os.MkdirAll(jobDir, 0755)

	updateJob(db, id, func(j *Job) { j.Status = StatusRunning })

	// Step 1: download audio
	ytArgs := []string{
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "0",
		"--output", filepath.Join(jobDir, "%(title)s.%(ext)s"),
		"--print", "after_move:filepath",
	}
	nodePath := cfg.NodeCmd
	if nodePath == "" {
		nodePath, _ = exec.LookPath("node")
	}
	if nodePath != "" {
		log.Printf("worker: using node at %s", nodePath)
		ytArgs = append(ytArgs, "--js-runtimes", "node:"+nodePath)
	} else {
		log.Println("worker: node not found, yt-dlp may fail without a JS runtime")
	}
	ytArgs = append(ytArgs, job.URL)
	var ytStderr bytes.Buffer
	ytCmd := exec.Command(cfg.YtDlpCmd, ytArgs...)
	ytCmd.Stderr = &ytStderr
	ytOut, err := ytCmd.Output()
	if err != nil {
		errMsg := fmt.Sprintf("download failed: %s", strings.TrimSpace(ytStderr.String()))
		log.Println("worker:", errMsg)
		updateJob(db, id, func(j *Job) {
			j.Status = StatusError
			j.Error = errMsg
			j.CompletedAt = time.Now()
		})
		return
	}

	audioFile := strings.TrimSpace(string(ytOut))
	title := strings.TrimSuffix(filepath.Base(audioFile), ".mp3")
	log.Printf("worker: downloaded %q", audioFile)

	updateJob(db, id, func(j *Job) {
		j.Title = title
		j.AudioFile = audioFile
	})

	// Step 2: separate stems
	demucsArgs := []string{"-m", "demucs", "--two-stems=vocals", "--out", jobDir, audioFile}
	var demucsStderr bytes.Buffer
	demucsCmd := exec.Command(cfg.PythonCmd, demucsArgs...)
	demucsCmd.Stdout = os.Stdout
	demucsCmd.Stderr = &demucsStderr
	if err := demucsCmd.Run(); err != nil {
		errMsg := fmt.Sprintf("separation failed: %s", strings.TrimSpace(demucsStderr.String()))
		log.Println("worker:", errMsg)
		updateJob(db, id, func(j *Job) {
			j.Status = StatusError
			j.Error = errMsg
			j.CompletedAt = time.Now()
		})
		return
	}

	log.Printf("worker: job %s done", id)
	updateJob(db, id, func(j *Job) {
		j.Status = StatusDone
		j.CompletedAt = time.Now()
	})
}
