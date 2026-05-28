//go:build release

package cfg

const IsRelease = true
const DBPath    = "/srv/apps/karaoke/shared/data/db.bolt"
const StaticDir = "static/"

var (
	JobsDir   = "/srv/apps/karaoke/shared/jobs"
	YtDlpCmd  = "/srv/apps/karaoke/shared/demucs-env/bin/yt-dlp"
	PythonCmd = "/srv/apps/karaoke/shared/demucs-env/bin/python"
	NodeCmd        = ""
	CookiesBrowser = ""
	CookiesFile    = ""
)
