// Command restic-web is a small local web app that demonstrates restic-based
// incremental backups. It wraps the external `restic` CLI (which provides all
// the deduplication, chunking and encryption) and serves a single-page UI for
// configuring a repository, running backups, browsing snapshots, restoring and
// downloading snapshots as zip files — all with live progress.
package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"
)

// The entire web UI is embedded into the binary, so the app is a single,
// dependency-free executable.
//
//go:embed web
var webFiles embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "address to listen on")
	configPath := flag.String("config", "config.json", "path to the JSON config file")
	flag.Parse()

	store, err := loadConfigStore(*configPath)
	if err != nil {
		log.Fatalf("could not load config %q: %v", *configPath, err)
	}

	// Serve the embedded web/ directory at the site root.
	uiFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("could not open embedded web assets: %v", err)
	}
	static := http.FileServer(http.FS(uiFS))

	hub := newHub()
	srv := newServer(store, hub)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.routes(static),
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: SSE connections and large downloads are long-lived.
	}

	log.Printf("restic-web is running.")
	if !resticInstalled() {
		log.Printf("WARNING: the `restic` binary was not found on PATH. Install it before backing up (e.g. `brew install restic`).")
	}
	log.Printf("Open http://%s in your browser.", *addr)

	if err := httpServer.ListenAndServe(); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
