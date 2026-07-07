# restic backup demo

A small **local web app** that demonstrates [restic](https://restic.net)-based
**incremental backups**. The Go server wraps the `restic` command-line tool —
restic does all the real work (deduplication, chunking, encryption) — and serves
a clean single-page UI for configuring a repository, running backups, browsing
snapshots, restoring, and downloading any snapshot as a zip, all with **live
progress**.

This is a single-user tool meant to run on your own machine, so there is no
authentication.

---

## What it can do

- **Pick a folder** and back it up. The first run stores everything; every later
  run is automatically **incremental** — restic only stores what changed.
- **Live progress** while a backup or restore runs: a progress bar, the current
  phase, files/bytes processed, a streaming log, and a final summary
  (files new / changed / unchanged, data added, snapshot ID, duration).
- **List snapshots** with their time, source paths, size and file count.
- **Restore** any snapshot to a folder you choose, with progress.
- **Download** any snapshot as a `.zip` (restored to a temp folder, zipped,
  streamed to your browser, then cleaned up).
- Two storage backends: an **S3-compatible** object store, or a **Local
  directory** so you can try the demo immediately with no credentials.

### Seeing the incremental behavior

1. Back up a folder once — the summary shows every file as **new**.
2. Change one file in that folder.
3. Back it up again — the summary now shows most files as **unchanged** and only
   a small amount of **data added**. The app highlights this explicitly.

---

## Prerequisites

1. **Go** 1.23 or newer — <https://go.dev/dl/>
2. **restic** must be installed and on your `PATH`. The app detects when it is
   missing and shows a clear message. Install it with one of:

   | Platform        | Command                                  |
   | --------------- | ---------------------------------------- |
   | macOS (Homebrew) | `brew install restic`                   |
   | Debian / Ubuntu | `sudo apt install restic`                |
   | Fedora          | `sudo dnf install restic`                |
   | Windows (Scoop) | `scoop install restic`                   |
   | Any             | Download a binary from <https://github.com/restic/restic/releases> |

   Verify it works:

   ```sh
   restic version
   ```

---

## How to run

From this directory:

```sh
go run .
```

Then open the URL it prints (default):

```
http://127.0.0.1:8080
```

To use a different address or config file:

```sh
go run . -addr 127.0.0.1:9000 -config ./myconfig.json
```

You can also build a standalone binary (the web UI is embedded into it):

```sh
go build -o restic-web .
./restic-web
```

---

## Quick start (Local backend — no credentials needed)

1. Open the app and go to **Settings**.
2. Leave the backend as **Local directory** and pick a repository folder, e.g.
   `./restic-repo` (the default).
3. Enter a **repository password** (used to encrypt the backups — don't lose it)
   and click **Save settings**.
4. Click **Test connection**. If the repository doesn't exist yet, an
   **Initialize repository** button appears — click it.
5. Go to **Backup**, enter the absolute path of a folder to back up, and click
   **Start backup**. Watch the live progress and summary.
6. Go to **Snapshots** to see your snapshot. Use **Restore** or **Download**.
7. Change a file in the source folder and back up again to see an incremental run.

### Using S3 instead

In **Settings**, switch the backend to **S3-compatible** and fill in the
endpoint URL (e.g. `https://s3.amazonaws.com` or a MinIO endpoint), bucket,
optional region, access key, and secret key, plus the repository password. The
app maps these to the restic repository string `s3:<endpoint>/<bucket>` and
passes credentials via environment variables — never on the command line.

---

## How it maps to restic

| Action          | Command run                                              |
| --------------- | ------------------------------------------------------- |
| Initialize      | `restic -r <repo> init`                                 |
| Backup          | `restic -r <repo> backup <sourceDir> --json`            |
| List snapshots  | `restic -r <repo> snapshots --json`                     |
| Restore         | `restic -r <repo> restore <id> --target <dir> --json`   |
| Download (zip)  | `restic -r <repo> restore <id> --target <tempDir>` then zip |
| Test connection | `restic -r <repo> cat config`                           |

Credentials are always supplied through the command's **environment**
(`RESTIC_PASSWORD`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`,
`AWS_DEFAULT_REGION`), so secrets never appear in a process listing or shell
history.

---

## Project layout

```
.
├── main.go      — entry point: flags, embedded UI, HTTP server
├── config.go    — settings model + JSON persistence + restic repo/env mapping
├── restic.go    — restic CLI wrapper: init, test, list, streaming backup/restore
├── hub.go       — SSE hub + single-operation "busy" lock with history replay
├── server.go    — HTTP handlers, SSE endpoint, zip download
└── web/         — single-page UI (HTML + CSS + vanilla JS), embedded into the binary
```

## Notes & limitations

- Only **one** backup/restore/download runs at a time; the UI shows a clear busy
  state and the server rejects concurrent operations.
- Settings (including the repository password and S3 secret) are stored in
  `config.json` in the working directory **in plaintext**. That is fine for a
  local single-user demo but not for shared or production use.
- A browser cannot give the server a real local path from a file picker, so
  source and target folders are entered as text fields (absolute paths).
