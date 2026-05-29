//go:build !release

package cfg

import "os"

const IsRelease = false
const DBPath    = ".serve/db.bolt"
const StaticDir = ".serve/static/"

var (
	JobsDir        = ".serve/jobs"
	YtDlpCmd       = os.ExpandEnv("$HOME/demucs-env/bin/yt-dlp")
	PythonCmd      = os.ExpandEnv("$HOME/demucs-env/bin/python")
	NodeCmd        = os.ExpandEnv("$HOME/.local/share/mise/shims/node")
	CookiesBrowser = ""
	CookiesFile    = ""
	Workers        = 1
)
