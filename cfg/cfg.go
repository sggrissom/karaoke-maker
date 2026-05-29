package cfg

import (
	"os"
	"strconv"
)

const Backport = 12868

// ApplyEnv overrides cfg vars from environment variables after .env is loaded.
func ApplyEnv() {
	if v := os.Getenv("YTDLP_CMD"); v != "" {
		YtDlpCmd = v
	}
	if v := os.Getenv("PYTHON_CMD"); v != "" {
		PythonCmd = v
	}
	if v := os.Getenv("JOBS_DIR"); v != "" {
		JobsDir = v
	}
	if v := os.Getenv("NODE_CMD"); v != "" {
		NodeCmd = v
	}
	if v := os.Getenv("WORKER_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			Workers = n
		}
	}
}
