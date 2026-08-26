// Command restic-web is a small local web app that demonstrates restic-based
// incremental backups. It wraps the external `restic` CLI (which provides all
// the deduplication, chunking and encryption) and serves a single-page UI for
// configuring a repository, running backups, browsing snapshots, restoring and
// downloading snapshots as zip files — all with live progress.
package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"restic-web/internal/core"
)

// The entire web UI is embedded into the binary, so the app is a single
// executable. Durable state lives in Postgres (see .env / DATABASE_URL).
//
//go:embed web
var webFiles embed.FS

func main() {
	_ = core.LoadDotEnv(".env")

	addr := flag.String("addr", core.DefaultListenAddr(), "address to listen on")
	configPath := flag.String("config", "config.json", "path to the legacy JSON config file (imported once)")
	dataDir := flag.String("data", "data", "directory for ephemeral files (download workspaces)")
	dsnFlag := flag.String("database", "", "Postgres URL (default: DATABASE_URL from the environment or .env)")
	retain := flag.Int("retain-runs-per-job", 0, "keep only the newest N runs per job (0 = keep all)")
	flag.Parse()

	dsn := core.ResolveDSN(*dsnFlag)
	if dsn == "" {
		log.Fatal(core.MissingDSNMessage())
	}
	pool, err := core.OpenDB(context.Background(), dsn)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	app, err := core.NewApp(*dataDir, pool)
	if err != nil {
		log.Fatalf("could not open data directory: %v", err)
	}
	app.RetainRunsPerJob = *retain
	if err := app.ImportLegacyConfig(*configPath); err != nil {
		log.Printf("note: could not import legacy config: %v", err)
	}

	// Serve the embedded web/ directory at the site root.
	uiFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("could not open embedded web assets: %v", err)
	}
	static := http.FileServer(http.FS(uiFS))

	// Make the durable run records honest before accepting any traffic: any run
	// still marked running when the previous process died is marked interrupted,
	// and orphaned restic children are reaped.
	app.Reconcile()

	// Start the automatic-backup scheduler after reconcile so interrupted runs
	// are settled before any scheduled backup is considered.
	app.Sched.Start()
	defer app.Sched.Stop()

	srv := core.NewServer(app)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(static),
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: SSE connections and large downloads are long-lived.
	}

	log.Printf("restic-web is running.")
	if !core.ResticInstalled() {
		log.Printf("WARNING: the `restic` binary was not found on PATH. Install it before backing up (e.g. `brew install restic`).")
	}
	log.Printf("Open http://%s in your browser.", *addr)

	if err := httpServer.ListenAndServe(); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
