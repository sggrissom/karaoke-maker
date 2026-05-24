package backend

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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

// watchYtDlpProgress reads yt-dlp stderr and sends download percentage values on ch.
func watchYtDlpProgress(r io.Reader, ch chan<- int) {
	defer close(ch)
	scanner := bufio.NewScanner(r)
	re := regexp.MustCompile(`\[download\]\s+([\d.]+)%`)
	for scanner.Scan() {
		if m := re.FindStringSubmatch(scanner.Text()); m != nil {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				select {
				case ch <- int(f):
				default:
				}
			}
		}
	}
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

	updateJob(db, id, func(j *Job) {
		j.Status = StatusRunning
		j.Step = "downloading"
		j.Progress = 0
	})

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

	var ytStdout, ytStderr bytes.Buffer
	ytCmd := exec.Command(cfg.YtDlpCmd, ytArgs...)
	ytCmd.Stdout = &ytStdout
	stderrPipe, pipeErr := ytCmd.StderrPipe()

	if err := ytCmd.Start(); err != nil {
		errMsg := fmt.Sprintf("download failed: %s", err)
		log.Println("worker:", errMsg)
		updateJob(db, id, func(j *Job) {
			j.Status = StatusError
			j.Error = errMsg
			j.CompletedAt = time.Now()
		})
		return
	}

	if pipeErr == nil {
		progressCh := make(chan int, 1)
		go watchYtDlpProgress(stderrPipe, progressCh)

		ticker := time.NewTicker(time.Second)
		lastPct := 0
		for open := true; open; {
			select {
			case pct, ok := <-progressCh:
				if !ok {
					open = false
				} else {
					lastPct = pct
				}
			case <-ticker.C:
				p := lastPct / 5 // map 0-100% → 0-20 overall
				updateJob(db, id, func(j *Job) { j.Progress = p })
			}
		}
		ticker.Stop()
	} else {
		ytCmd.Stderr = &ytStderr
	}

	if err := ytCmd.Wait(); err != nil {
		errMsg := fmt.Sprintf("download failed: %s", strings.TrimSpace(ytStderr.String()))
		if errMsg == "download failed: " {
			errMsg = fmt.Sprintf("download failed: %s", err)
		}
		log.Println("worker:", errMsg)
		updateJob(db, id, func(j *Job) {
			j.Status = StatusError
			j.Error = errMsg
			j.CompletedAt = time.Now()
		})
		return
	}

	audioFile := strings.TrimSpace(ytStdout.String())
	title := strings.TrimSuffix(filepath.Base(audioFile), ".mp3")
	log.Printf("worker: downloaded %q", audioFile)

	updateJob(db, id, func(j *Job) {
		j.Title = title
		j.AudioFile = audioFile
		j.Step = "separating"
		j.Progress = 20
	})

	// Step 2: separate stems
	demucsArgs := []string{"-m", "demucs", "--two-stems=vocals", "--segment", "7", "--out", jobDir, audioFile}
	var demucsStderr bytes.Buffer
	demucsCmd := exec.Command(cfg.PythonCmd, demucsArgs...)
	demucsCmd.Stdout = os.Stdout
	demucsCmd.Stderr = io.MultiWriter(os.Stderr, &demucsStderr)
	demucsCmd.Env = append(os.Environ(), "OMP_NUM_THREADS=1")

	if err := demucsCmd.Start(); err != nil {
		errMsg := fmt.Sprintf("separation failed: %s", err)
		log.Println("worker:", errMsg)
		updateJob(db, id, func(j *Job) {
			j.Status = StatusError
			j.Error = errMsg
			j.CompletedAt = time.Now()
		})
		return
	}

	// Time-based progress: asymptotically approach 95% over ~2 min tau
	stopProgress := make(chan struct{})
	go func() {
		start := time.Now()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-t.C:
				elapsed := time.Since(start).Seconds()
				pct := 20 + int(75.0*(1.0-math.Exp(-elapsed/120.0)))
				if pct > 95 {
					pct = 95
				}
				updateJob(db, id, func(j *Job) { j.Progress = pct })
			}
		}
	}()

	err := demucsCmd.Wait()
	close(stopProgress)

	if err != nil {
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
		j.Step = ""
		j.Progress = 100
		j.CompletedAt = time.Now()
	})
}
