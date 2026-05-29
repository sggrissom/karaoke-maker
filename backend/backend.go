package backend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.hasen.dev/vbeam"
	"go.hasen.dev/vbolt"
	"karaoke/cfg"
)

func RegisterMethods(app *vbeam.Application) {
	vbolt.WithWriteTx(app.DB, func(tx *vbolt.Tx) {
		vbolt.EnsureBuckets(tx, &DbInfo)
		vbolt.TxCommit(tx)
	})

	StartWorker(app.DB)

	vbeam.RegisterProc(app, SubmitJob)
	vbeam.RegisterProc(app, GetJob)
	vbeam.RegisterProc(app, ListJobs)
	vbeam.RegisterProc(app, DeleteJob)
}

type SubmitJobRequest struct {
	URL        string
	PitchShift int // semitones, -12 to +12
}

type SubmitJobResponse struct {
	JobID string
}

func SubmitJob(ctx *vbeam.Context, req SubmitJobRequest) (SubmitJobResponse, error) {
	if req.URL == "" {
		return SubmitJobResponse{}, errors.New("URL is required")
	}
	if !isYouTubeURL(req.URL) {
		return SubmitJobResponse{}, errors.New("URL must be a YouTube URL")
	}

	if req.PitchShift < -12 {
		req.PitchShift = -12
	} else if req.PitchShift > 12 {
		req.PitchShift = 12
	}

	// Dedup: return existing job if the URL + pitch shift is already queued, running, or done
	var existingID string
	vbolt.IterateAll(ctx.Tx, JobBucket, func(id string, job Job) bool {
		if job.URL == req.URL && job.PitchShift == req.PitchShift && job.Status != StatusError {
			existingID = id
			return false
		}
		return true
	})
	if existingID != "" {
		return SubmitJobResponse{JobID: existingID}, nil
	}

	id := fmt.Sprintf("%020d", time.Now().UnixNano())
	job := Job{
		ID:         id,
		URL:        req.URL,
		PitchShift: req.PitchShift,
		Status:     StatusQueued,
		CreatedAt:  time.Now(),
	}

	vbeam.UseWriteTx(ctx)
	vbolt.Write(ctx.Tx, JobBucket, id, &job)
	vbolt.TxCommit(ctx.Tx)

	EnqueueJob(id)

	return SubmitJobResponse{JobID: id}, nil
}

type GetJobRequest struct {
	JobID string
}

func GetJob(ctx *vbeam.Context, req GetJobRequest) (Job, error) {
	var job Job
	if !vbolt.Read(ctx.Tx, JobBucket, req.JobID, &job) {
		return job, errors.New("job not found")
	}
	return job, nil
}

type ListJobsResponse struct {
	Jobs []Job
}

func ListJobs(ctx *vbeam.Context, _ vbeam.Empty) (ListJobsResponse, error) {
	jobs := make([]Job, 0)
	vbolt.IterateAllReverse(ctx.Tx, JobBucket, func(_ string, job Job) bool {
		jobs = append(jobs, job)
		return true
	})
	return ListJobsResponse{Jobs: jobs}, nil
}

type DeleteJobRequest struct {
	JobID string
}

func DeleteJob(ctx *vbeam.Context, req DeleteJobRequest) (vbeam.Empty, error) {
	var job Job
	if !vbolt.Read(ctx.Tx, JobBucket, req.JobID, &job) {
		return vbeam.Empty{}, errors.New("job not found")
	}
	if job.Status == StatusQueued || job.Status == StatusRunning {
		return vbeam.Empty{}, errors.New("cannot delete an active job")
	}

	vbeam.UseWriteTx(ctx)
	vbolt.Delete(ctx.Tx, JobBucket, req.JobID)
	vbolt.TxCommit(ctx.Tx)

	os.RemoveAll(filepath.Join(cfg.JobsDir, req.JobID))

	return vbeam.Empty{}, nil
}

func isYouTubeURL(url string) bool {
	return strings.Contains(url, "youtube.com/") || strings.Contains(url, "youtu.be/")
}
