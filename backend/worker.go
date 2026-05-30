package backend

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.hasen.dev/vbolt"
	"karaoke/cfg"
)

//go:embed analyze.py
var analyzeScript []byte

//go:embed transcribe.py
var transcribeScript []byte

var jobQueue chan string

func StartWorker(db *vbolt.DB) {
	jobQueue = make(chan string, 100)

	// Re-enqueue any jobs that were running or queued when the server last stopped.
	vbolt.WithWriteTx(db, func(tx *vbolt.Tx) {
		vbolt.IterateAll(tx, JobBucket, func(id string, job Job) bool {
			if job.Status == StatusRunning || job.Status == StatusQueued {
				job.Status = StatusQueued
				job.Step = ""
				job.Progress = 0
				vbolt.Write(tx, JobBucket, id, &job)
				jobQueue <- id
			}
			return true
		})
		vbolt.TxCommit(tx)
	})

	for i := 0; i < cfg.Workers; i++ {
		go runWorker(db)
	}
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

// splitOnCRorLF splits on \n or \r so tqdm overwrite lines are treated as separate tokens.
func splitOnCRorLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// watchDemucsProgress reads Demucs stderr, logs it, writes to errBuf, and sends tqdm percentages on ch.
func watchDemucsProgress(r io.Reader, errBuf *bytes.Buffer, ch chan<- int) {
	defer close(ch)
	scanner := bufio.NewScanner(r)
	scanner.Split(splitOnCRorLF)
	re := regexp.MustCompile(`(\d+)%\|`)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.Contains(line, "NNPACK") {
			continue
		}
		fmt.Fprintln(os.Stderr, line)
		errBuf.WriteString(line + "\n")
		if m := re.FindStringSubmatch(line); m != nil {
			if pct, err := strconv.Atoi(m[1]); err == nil {
				select {
				case ch <- pct:
				default:
				}
			}
		}
	}
}

// watchYtDlpProgress reads yt-dlp stderr, writes all lines to errBuf, and sends download percentage values on ch.
func watchYtDlpProgress(r io.Reader, errBuf *bytes.Buffer, ch chan<- int) {
	defer close(ch)
	scanner := bufio.NewScanner(r)
	re := regexp.MustCompile(`\[download\]\s+([\d.]+)%`)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			errBuf.WriteString(line + "\n")
		}
		if m := re.FindStringSubmatch(line); m != nil {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				select {
				case ch <- int(f):
				default:
				}
			}
		}
	}
}

// ytNeedsCookies returns true when the yt-dlp error text suggests that
// authentication via browser cookies might help (age gate, login wall, etc.).
func ytNeedsCookies(errText string) bool {
	for _, phrase := range []string{
		"Sign in", "sign in",
		"age-restricted", "age restricted",
		"members-only", "members only",
		"This video requires payment",
		"Private video",
		"login", "Login",
	} {
		if strings.Contains(errText, phrase) {
			return true
		}
	}
	return false
}

func processJob(db *vbolt.DB, id string) { //nolint:gocyclo
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

	var audioFile, title string
	var audioFiles []string

	if job.AudioFile != "" {
		// Uploaded file — skip download step entirely
		audioFile = job.AudioFile
		title = strings.TrimSuffix(filepath.Base(audioFile), filepath.Ext(audioFile))
		audioFiles = []string{audioFile}
		os.MkdirAll(jobDir, 0755)
		updateJob(db, id, func(j *Job) {
			j.Status = StatusRunning
			j.Step = "separating"
			j.Progress = 20
			j.StepStartedAt = time.Now()
		})
	} else {
		os.RemoveAll(jobDir)
		os.MkdirAll(jobDir, 0755)

		updateJob(db, id, func(j *Job) {
			j.Status = StatusRunning
			j.Step = "downloading"
			j.Progress = 0
		})

		// Step 1: download audio
		baseYtArgs := []string{
			"--no-playlist",
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
			baseYtArgs = append(baseYtArgs, "--js-runtimes", "node:"+nodePath)
		} else {
			log.Println("worker: node not found, yt-dlp may fail without a JS runtime")
		}

		// Build the ordered list of download attempts.
		// Each attempt is a set of extra args to append to baseYtArgs.
		type attempt struct {
			label     string
			extraArgs []string
			authOnly  bool // skip unless a prior attempt failed with an auth error
		}
		var attempts []attempt
		if cfg.CookiesBrowser != "" {
			attempts = []attempt{
				{label: "browser:" + cfg.CookiesBrowser, extraArgs: []string{"--cookies-from-browser", cfg.CookiesBrowser}},
			}
		} else {
			attempts = []attempt{
				{label: "default", extraArgs: nil},
				{label: "android", extraArgs: []string{"--extractor-args", "youtube:player_client=android"}},
				{label: "tv_embedded", extraArgs: []string{"--extractor-args", "youtube:player_client=tv_embedded"}},
			}
			if cfg.CookiesFile != "" {
				attempts = append(attempts,
					attempt{label: "cookies+default", authOnly: true, extraArgs: []string{"--cookies", cfg.CookiesFile}},
					attempt{label: "cookies+android", authOnly: true, extraArgs: []string{"--cookies", cfg.CookiesFile, "--extractor-args", "youtube:player_client=android"}},
				)
			} else {
				// Browser-cookie fallbacks only make sense on a dev machine.
				browserFallbacks := []string{"chrome", "firefox"}
				if runtime.GOOS == "darwin" {
					browserFallbacks = []string{"chrome", "safari"}
				}
				for _, br := range browserFallbacks {
					attempts = append(attempts, attempt{
						label:    "browser:" + br,
						authOnly: true,
						extraArgs: []string{"--cookies-from-browser", br},
					})
				}
			}
		}

		var lastErrMsg string
		needsAuth := false
		for _, att := range attempts {
		if att.authOnly && !needsAuth {
			continue
		}
		ytArgs := append(append([]string(nil), baseYtArgs...), att.extraArgs...)
		ytArgs = append(ytArgs, job.URL)

		var ytStdout, ytStderr bytes.Buffer
		ytCmd := exec.Command(cfg.YtDlpCmd, ytArgs...)
		ytCmd.Stdout = &ytStdout
		stderrPipe, pipeErr := ytCmd.StderrPipe()

		if err := ytCmd.Start(); err != nil {
			lastErrMsg = fmt.Sprintf("download failed: %s", err)
			log.Println("worker:", lastErrMsg)
			continue
		}

		if pipeErr == nil {
			progressCh := make(chan int, 1)
			go watchYtDlpProgress(stderrPipe, &ytStderr, progressCh)

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
			errText := strings.TrimSpace(ytStderr.String())
			lastErrMsg = fmt.Sprintf("download failed: %s", errText)
			if lastErrMsg == "download failed: " {
				lastErrMsg = fmt.Sprintf("download failed: %s", err)
			}
			log.Printf("worker: attempt %q failed: %s", att.label, lastErrMsg)
			if ytNeedsCookies(errText) {
				needsAuth = true
			}
			continue
		}

		// Collect all downloaded file paths (playlists print one path per line).
		for _, line := range strings.Split(ytStdout.String(), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				audioFiles = append(audioFiles, line)
			}
		}
		if len(audioFiles) > 0 {
			audioFile = audioFiles[0]
			break
		}
	}

		if len(audioFiles) == 0 {
			// yt-dlp may have succeeded but not printed to stdout; scan jobDir as fallback.
			entries, _ := os.ReadDir(jobDir)
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".mp3") {
					audioFiles = append(audioFiles, filepath.Join(jobDir, e.Name()))
				}
			}
		}

		if len(audioFiles) == 0 {
			if lastErrMsg == "" {
				lastErrMsg = "download failed: no audio files found"
			}
			log.Println("worker:", lastErrMsg)
			updateJob(db, id, func(j *Job) {
				j.Status = StatusError
				j.Error = lastErrMsg
				j.CompletedAt = time.Now()
			})
			return
		}

		audioFile = audioFiles[0]
		title = strings.TrimSuffix(filepath.Base(audioFile), ".mp3")
		log.Printf("worker: downloaded %d file(s), first: %q", len(audioFiles), audioFile)

		updateJob(db, id, func(j *Job) {
			j.Title = title
			j.AudioFile = audioFile
			j.Step = "separating"
			j.Progress = 20
			j.StepStartedAt = time.Now()
		})
	} // end else (download branch)

	// Step 2: separate stems
	// -u forces unbuffered output so tqdm progress lines reach the pipe promptly
	demucsArgs := append([]string{"-u", "-m", "demucs", "--two-stems=vocals", "--segment", "7", "--out", jobDir}, audioFiles...)
	var demucsStderr bytes.Buffer
	demucsCmd := exec.Command(cfg.PythonCmd, demucsArgs...)
	demucsCmd.Stdout = os.Stdout
	demucsCmd.Env = append(os.Environ(), "OMP_NUM_THREADS=1")

	stderrPipe2, pipeErr2 := demucsCmd.StderrPipe()
	if pipeErr2 != nil {
		demucsCmd.Stderr = io.MultiWriter(os.Stderr, &demucsStderr)
	}

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

	var stopFallback chan struct{}
	if pipeErr2 == nil {
		progressCh2 := make(chan int, 1)
		go watchDemucsProgress(stderrPipe2, &demucsStderr, progressCh2)

		ticker2 := time.NewTicker(time.Second)
		lastPct2 := 20
		for open := true; open; {
			select {
			case pct, ok := <-progressCh2:
				if !ok {
					open = false
				} else {
					overall := 20 + pct*75/100
					if overall > 95 {
						overall = 95
					}
					lastPct2 = overall
				}
			case <-ticker2.C:
				updateJob(db, id, func(j *Job) { j.Progress = lastPct2 })
			}
		}
		ticker2.Stop()
	} else {
		// Fallback: time-based exponential with a 600s time constant
		stopFallback = make(chan struct{})
		go func() {
			start := time.Now()
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-stopFallback:
					return
				case <-t.C:
					elapsed := time.Since(start).Seconds()
					pct := 20 + int(75.0*(1.0-math.Exp(-elapsed/600.0)))
					if pct > 95 {
						pct = 95
					}
					updateJob(db, id, func(j *Job) { j.Progress = pct })
				}
			}
		}()
	}

	err := demucsCmd.Wait()
	if stopFallback != nil {
		close(stopFallback)
	}

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

	// Step 3: pitch shift and/or speed adjustment (if requested)
	stemDir := filepath.Join(jobDir, "htdemucs", title)
	needsSpeed := job.SpeedAdjust != 0 && job.SpeedAdjust != 1.0
	if job.PitchShift != 0 || needsSpeed {
		updateJob(db, id, func(j *Job) {
			j.Step = "shifting"
			j.Progress = 95
		})
		for _, stem := range []string{"vocals", "no_vocals"} {
			wavPath := filepath.Join(stemDir, stem+".wav")
			shiftedPath := filepath.Join(stemDir, stem+"_shifted.wav")
			rbArgs := []string{"-c", "6"}
			if job.PitchShift != 0 {
				rbArgs = append(rbArgs, "-p", strconv.Itoa(job.PitchShift))
			}
			if needsSpeed {
				// rubberband -t is the time-stretch ratio (duration multiplier); speed = 1/ratio
				ratio := 1.0 / job.SpeedAdjust
				rbArgs = append(rbArgs, "-t", strconv.FormatFloat(ratio, 'f', 4, 64))
			}
			rbArgs = append(rbArgs, wavPath, shiftedPath)
			rbCmd := exec.Command("rubberband", rbArgs...)
			if rbErr := rbCmd.Run(); rbErr != nil {
				log.Printf("worker: rubberband %s failed: %s", stem, rbErr)
			} else {
				os.Remove(wavPath)
				os.Rename(shiftedPath, wavPath)
			}
		}
	}

	// Step 4: convert WAV stems to MP3
	updateJob(db, id, func(j *Job) {
		j.Step = "converting"
		j.Progress = 96
	})

	for _, stem := range []string{"vocals", "no_vocals"} {
		wavPath := filepath.Join(stemDir, stem+".wav")
		mp3Path := filepath.Join(stemDir, stem+".mp3")
		ffCmd := exec.Command("ffmpeg", "-y", "-i", wavPath, "-b:a", "320k", mp3Path)
		if ffErr := ffCmd.Run(); ffErr != nil {
			log.Printf("worker: ffmpeg %s failed: %s", stem, ffErr)
		} else {
			os.Remove(wavPath)
		}
	}

	// Step 5: transcribe lyrics from vocals
	updateJob(db, id, func(j *Job) {
		j.Step = "transcribing"
		j.Progress = 97
	})

	vocalsMP3 := filepath.Join(stemDir, "vocals.mp3")
	transcriptPath := filepath.Join(jobDir, "transcribe.py")
	if writeErr := os.WriteFile(transcriptPath, transcribeScript, 0644); writeErr == nil {
		transcribeCmd := exec.Command(cfg.PythonCmd, transcriptPath, vocalsMP3)
		transcribeCmd.Env = append(os.Environ(), "OMP_NUM_THREADS=1")
		if out, transcribeErr := transcribeCmd.Output(); transcribeErr == nil {
			var result struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(out, &result) == nil && result.Text != "" {
				updateJob(db, id, func(j *Job) {
					j.Lyrics = result.Text
				})
			} else {
				log.Printf("worker: transcribe parse error for job %s: %s", id, out)
			}
		} else {
			log.Printf("worker: transcribe failed for job %s: %s", id, transcribeErr)
		}
	}

	// Step 6: analyze BPM and key
	updateJob(db, id, func(j *Job) {
		j.Step = "analyzing"
		j.Progress = 98
	})

	scriptPath := filepath.Join(jobDir, "analyze.py")
	if writeErr := os.WriteFile(scriptPath, analyzeScript, 0644); writeErr == nil {
		analyzeCmd := exec.Command(cfg.PythonCmd, scriptPath, audioFile)
		analyzeCmd.Env = append(os.Environ(), "OMP_NUM_THREADS=1")
		if out, analyzeErr := analyzeCmd.Output(); analyzeErr == nil {
			var result struct {
				BPM float64 `json:"bpm"`
				Key string  `json:"key"`
			}
			if json.Unmarshal(out, &result) == nil {
				updateJob(db, id, func(j *Job) {
					j.BPM = result.BPM
					j.Key = result.Key
				})
			} else {
				log.Printf("worker: analyze parse error for job %s: %s", id, out)
			}
		} else {
			log.Printf("worker: analyze failed for job %s: %s", id, analyzeErr)
		}
	}

	log.Printf("worker: job %s done", id)
	updateJob(db, id, func(j *Job) {
		j.Status = StatusDone
		j.Step = ""
		j.Progress = 100
		j.CompletedAt = time.Now()
	})
}
