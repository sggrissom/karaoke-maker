package karaoke

import (
	"karaoke/backend"
	"karaoke/cfg"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"go.hasen.dev/vbeam"
	"go.hasen.dev/vbolt"
)

func OpenDB(dbpath string) *vbolt.DB {
	return vbolt.Open(dbpath)
}

func MakeApplication() *vbeam.Application {
	_ = godotenv.Load()

	if os.Getenv("PROD") == "true" || os.Getenv("ENVIRONMENT") == "production" {
		_ = godotenv.Load("/srv/apps/karaoke/shared/.env")
	}

	db := OpenDB(cfg.DBPath)
	app := vbeam.NewApplication("KaraokeMaker", db)

	app.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	backend.RegisterMethods(app)

	return app
}
