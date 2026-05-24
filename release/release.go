//go:build release

package main

import (
	"embed"
	"fmt"
	"io/fs"
	"karaoke"
	"karaoke/cfg"
	"log"
	"net/http"
	"os"
)

//go:embed dist
var embedded embed.FS

const Port = 8668

func main() {
	distFS, err := fs.Sub(embedded, "dist")
	if err != nil {
		log.Fatalf("failed to sub-fs: %v", err)
	}

	app := karaoke.MakeApplication()
	app.Frontend = distFS
	app.StaticData = os.DirFS(cfg.StaticDir)

	addr := fmt.Sprintf(":%d", Port)
	log.Printf("listening on %s\n", addr)
	appServer := &http.Server{Addr: addr, Handler: app}
	appServer.ListenAndServe()
}
