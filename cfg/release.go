//go:build release

package cfg

import "os"

const IsRelease = true
const DBPath    = "/srv/apps/karaoke/shared/data/db.bolt"
const StaticDir = "static/"

var (
	JobsDir   = "/srv/apps/karaoke/shared/jobs"
	YtDlpCmd  = os.ExpandEnv("$HOME/demucs-env/bin/yt-dlp")
	PythonCmd = os.ExpandEnv("$HOME/demucs-env/bin/python")
)
