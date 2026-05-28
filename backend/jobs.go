package backend

import (
	"time"

	"go.hasen.dev/vbolt"
	"go.hasen.dev/vpack"
)

const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusError   = "error"
)

type Job struct {
	ID            string
	URL           string
	Status        string
	Step          string // "downloading" | "separating" | "analyzing" | ""
	Progress      int    // 0-100
	Title         string
	AudioFile     string `json:"-"`
	CreatedAt     time.Time
	CompletedAt   time.Time
	Error         string
	StepStartedAt time.Time
	BPM           float64
	Key           string
}

var DbInfo vbolt.Info
var JobBucket = vbolt.Bucket[string, Job](&DbInfo, "jobs", vpack.String, packJob)

func packJob(j *Job, buf *vpack.Buffer) {
	version := vpack.Version(2, buf)
	vpack.String(&j.ID, buf)
	vpack.String(&j.URL, buf)
	vpack.String(&j.Status, buf)
	vpack.String(&j.Title, buf)
	vpack.String(&j.AudioFile, buf)
	vpack.Time(&j.CreatedAt, buf)
	vpack.Time(&j.CompletedAt, buf)
	vpack.String(&j.Error, buf)
	vpack.String(&j.Step, buf)
	vpack.Int(&j.Progress, buf)
	vpack.Time(&j.StepStartedAt, buf)
	if version >= 2 {
		vpack.Float64(&j.BPM, buf)
		vpack.String(&j.Key, buf)
	}
}
