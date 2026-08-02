# restic backup manager

A **local web app** for managing [restic](https://restic.net)-based **incremental
backups**. The Go server wraps the `restic` command-line tool — restic does all
the real work (deduplication, chunking, encryption) — and serves a single-page UI
built around three things you create and manage:

- **Storage repositories** — several named places to store backups, added and
  edited in the app.
- **Backup folders** — named, reusable source folders.
- **Backup jobs** — the core concept: a saved, named pairing of one folder and
  one repository. A job is the thing you run, look at, and come back to.

Everything long-running — backups, restores, repository setup, and old-snapshot
restores/downloads — is a **run**: a durable record with its own live progress
and a permanent log. History survives restarts; a job in progress still shows as
running (with live progress and log) after a refresh, a second tab, or an hour
away; and if the app is killed mid-backup it never comes back claiming something
is still running.

This is a single-user tool meant to run on your own machine, so there is no
authentication.

---

## Prerequisites

1. **Go** 1.23 or newer — <https://go.dev/dl/>
2. **restic** on your `PATH`. The app detects when it is missing and shows a clear
   message. Install it with `brew install restic`, `apt install restic`,
   `dnf install restic`, `scoop install restic`, or a binary from
   <https://github.com/restic/restic/releases>. Verify with `restic version`.

## How to run

```sh
go run .
```

Then open the printed URL (default `http://127.0.0.1:8080`). Flags:

```sh
go run . -addr 127.0.0.1:9000 -data ./data
```

- `-addr` — address to listen on.
- `-data` — directory for persisted state (default `./data`).
- `-config` — a legacy `config.json` to import once into a "Default" repository.

Build a standalone binary (the web UI is embedded):

```sh
go build -o restic-web . && ./restic-web
```

Node is **not** needed to build or run the app — only to change the interface.
See [Working on the UI](#working-on-the-ui).

## Quick start (Local backend — no credentials)

1. **Folders** → add a folder with the absolute path you want to back up.
2. **Repositories** → add a repository, backend **Local directory**, pick a
   directory (e.g. `./restic-repo`) and set a password.
3. **Jobs** → create a job pairing that folder and that repository.
4. **Run backup**. The repository is created and initialized on the first backup,
   so there is no separate setup step. Watch the live run; its history and
   snapshots appear on the job page.
5. Change a file and run again to see an incremental backup (mostly *unchanged*,
   only a little *data added*).

For **S3**, add a repository with backend **S3-compatible** and fill in the
endpoint, bucket, optional region, access key, secret key, and password.
Credentials are always passed to restic through the process **environment**
(`RESTIC_PASSWORD`, `AWS_*`), never on the command line.

---

## Architecture

Dependency-free Go (standard library only); state is plain files under the data
directory. The front-end has its own toolchain, but it is a build-time concern —
the shipped binary is still a single self-contained executable.

```
data/
├── repositories.json        named storage locations   (atomic temp+rename, fsync)
├── folders.json             named source folders
├── jobs.json                folder + repository pairings
└── runs/<runId>/
    ├── run.json             the authoritative run record (status, progress, summary)
    └── log.jsonl            the append-only, permanent log
```

- **The durable run record is the single source of truth** for whether an
  operation is running. An in-memory handle exists only to stream output and to
  stop a run; it is never consulted to answer "is it running", so any browser,
  tab, or later visit reconstructs exact state from disk.
- **Per-repository serialization.** Two jobs targeting different repositories run
  in parallel; a second operation on a busy repository is refused with a clear
  message naming the blocker. (This is the app's policy — restic's backup/restore
  locks are non-exclusive — chosen for clarity and future prune/forget headroom.)
- **Crash honesty.** On startup, before serving, any run still marked running is
  reconciled to *interrupted*, and an orphaned restic child (identified via
  `/proc` on Linux) is reaped; affected repositories get a stale-only
  `restic unlock`.
- **Stop** sends restic `SIGINT` (clean: no partial snapshot, lock released) with
  a hard-kill fallback.
- **Live sync** is Server-Sent Events. A per-run stream replays the durable log
  backlog then tails live, resuming exactly via `Last-Event-ID`; a global stream
  drives list badges. Live delivery is a pure accelerator — the durable record is
  always the catch-up source.
- **Job ↔ snapshot** association is a stable per-job restic tag
  (`resticweb-job:<id>`), so a job's snapshots are discoverable from the
  repository itself.

### Code layout

```
main.go            entry point: flags, embedded UI, startup reconcile, HTTP server
model.go           domain types: Repository, Folder, Job, Run, LogLine
store.go           generic entity file store (atomic, fsync'd) + typed errors
app.go             dependency container + legacy config import
runstore.go        durable run records + append-only logs + reconcile + retention
coordinator.go     per-repository locking; drives every run kind
runner.go          Runner interface + real restic runner (streaming) + exit codes
restic.go          stateless restic helpers (message/snapshot parsing, classification)
broadcaster.go     history-free SSE fan-out (the event bus)
reconcile.go       startup crash recovery + orphan reaping
server.go          routing + shared HTTP helpers
handlers_*.go      HTTP handlers: entities, runs, repo ops, SSE, status
sysproc_*.go       process-group setup / signalling (unix, windows)
reap_*.go          orphan identification (linux via /proc; no-op elsewhere)
ui/                UI source (React + Tailwind + shadcn/ui) — see below
web/               built UI, committed and embedded into the binary
```

## Working on the UI

The interface is a React single-page app built with [Vite](https://vite.dev),
[Tailwind CSS v4](https://tailwindcss.com) and
[shadcn/ui](https://ui.shadcn.com) components (Radix primitives, lucide icons).
Those components are *copied into* `ui/src/components/ui/` rather than installed
as a dependency, so they can be edited like any other file in the repo.

The source lives in `ui/`; its build output is committed to `web/`, which
`main.go` embeds. That keeps `go build` a single step for anyone who only wants
to run the app.

```sh
cd ui
npm install
npm run build     # writes ../web — commit the result alongside your source change
npm run dev       # hot-reloading dev server, proxying /api to localhost:8080
```

For `npm run dev`, run the Go server separately (`go run .`) so the proxy has an
API to talk to.

```
ui/src/
├── main.jsx             routes (hash-based, so old #/jobs/… links still work)
├── index.css            theme tokens for dark + light
├── routes/              one file per screen
├── components/          app components (shell, run history, log panel, …)
├── components/ui/       shadcn/ui primitives
└── lib/                 API client, SSE hooks, formatting, run vocabulary
```

Dark is the default theme; light and "follow system" are available from the top
bar and applied before first paint, so the page never flashes the wrong theme.

## How it maps to restic

| Action          | Command run                                              |
| --------------- | ------------------------------------------------------- |
| Test connection | `restic -r <repo> cat config`                           |
| Initialize      | `restic -r <repo> init` (also run automatically by a backup against an uninitialized repository) |
| Backup          | `restic -r <repo> backup <src> --tag resticweb-job:<id> --json` |
| List snapshots  | `restic -r <repo> snapshots [--tag …] --json`           |
| Restore         | `restic -r <repo> restore <id> --target <dir> --json`   |
| Download (zip)  | restore into a temp workspace, then stream a zip        |
| Unlock          | `restic -r <repo> unlock` (removes only stale locks)    |

## Notes & limitations

- Secrets (repository passwords, S3 secret keys) are stored **in plaintext** in
  the data directory (files are `0600`). Fine for a local single-user tool; not
  for shared or production use. They are read through a single choke point, so
  encrypting at rest later is a localized change. They are never sent to the
  browser: the API reports only whether a secret is set, and an edit that leaves
  a secret field blank keeps the stored value.
- Retention keeps the newest 100 runs per job on startup; download workspaces are
  ephemeral and wiped on startup.
- A browser cannot hand the server a real local path, so source and target
  folders are entered as absolute-path text fields.
- restic must be the sole writer per repository for the stale-lock handling to be
  safe.
