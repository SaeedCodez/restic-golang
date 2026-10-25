// Command restic-web is a small local web app that demonstrates restic-based
// incremental backups. It wraps the external `restic` CLI (which provides all
// the deduplication, chunking and encryption) and serves a single-page UI for
// configuring a repository, running backups, browsing snapshots, restoring and
// downloading snapshots as zip files — all with live progress.
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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
	resticTimeout := core.DefaultResticTimeout
	if v := os.Getenv("RESTIC_WEB_RESTIC_TIMEOUT"); v != "" {
		d, err := core.ParseTimeout(v)
		if err != nil {
			log.Fatalf("RESTIC_WEB_RESTIC_TIMEOUT: %v", err)
		}
		resticTimeout = d
	}
	flag.Func("restic-timeout", "limit for the API's test/unlock/list-snapshots restic calls, e.g. 4m or 0 for none (default 4m; env RESTIC_WEB_RESTIC_TIMEOUT)", func(v string) error {
		d, err := core.ParseTimeout(v)
		resticTimeout = d
		return err
	})
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
	app.ResticTimeout = resticTimeout
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

	// On SIGTERM (a redeploy) or Ctrl-C, stop the runs cleanly before exiting.
	// Docker gives the container stop_grace_period (60s in docker-compose.yaml)
	// before it kills it.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		sig := <-sigs
		log.Printf("%v: stopping runs before shutting down…", sig)
		app.Sched.Stop()
		if !app.Coord.Shutdown(50 * time.Second) {
			log.Printf("some runs did not finish in time; they are marked interrupted on the next start")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
	<-shutdownDone
}
