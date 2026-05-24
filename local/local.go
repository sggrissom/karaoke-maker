package main

import (
	"fmt"
	"karaoke"
	"karaoke/cfg"
	"net/http"
	"os"

	"go.hasen.dev/vbeam"
	"go.hasen.dev/vbeam/esbuilder"
	"go.hasen.dev/vbeam/local_ui"
)

const Port   = 8668
const Domain = "karaoke.localhost"
const FEDist = ".serve/frontend"

func StartLocalServer() {
	defer vbeam.NiceStackTraceOnPanic()
	vbeam.RunBackServer(cfg.Backport)
	app := karaoke.MakeApplication()
	app.Frontend = os.DirFS(FEDist)
	app.StaticData = os.DirFS(cfg.StaticDir)
	vbeam.GenerateTSBindings(app, "frontend/server.ts")

	addr := fmt.Sprintf(":%d", Port)
	appServer := &http.Server{Addr: addr, Handler: app}
	appServer.ListenAndServe()
}

var FEOpts = esbuilder.FEBuildOptions{
	FERoot:    "frontend",
	EntryTS:   []string{"main.tsx"},
	EntryHTML: []string{"index.html"},
	CopyItems: []string{},
	Outdir:    FEDist,
	Define: map[string]string{
		"BROWSER": "true",
		"DEBUG":   "true",
		"VERBOSE": "false",
	},
}

var FEWatchDirs = []string{"frontend"}

func main() {
	os.MkdirAll(".serve", 0755)
	os.MkdirAll(".serve/static", 0755)
	os.MkdirAll(".serve/frontend", 0755)

	args := local_ui.LocalServerArgs{
		Domain:      Domain,
		Port:        Port,
		FEOpts:      FEOpts,
		FEWatchDirs: FEWatchDirs,
		StartServer: StartLocalServer,
	}
	local_ui.LaunchUI(args)
}
