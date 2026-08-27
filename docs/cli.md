# restic-webctl — CLI usage guide

`restic-webctl` is the control CLI for **restic-web**. It opens the same
Postgres database and data directory as the web app and calls the shared
`internal/core` logic **directly** — no HTTP requests and no login session.

That makes it ideal for Coolify/Docker (`docker exec … restic-webctl …`) and for
AI agents with shell access to the container.

Default output is human-readable tables. Pass **`--json`** for stable,
machine-readable responses (recommended for agents and scripts).

---

## Install

### Local build

```sh
go build -o restic-web ./cmd/restic-web
go build -o restic-webctl ./cmd/restic-webctl
```

The CLI needs `DATABASE_URL` (same as the server) and a writable `--data` dir.

### Docker / Coolify

The production image ships **both** binaries. Inside the container:

```sh
restic-webctl --help
restic-webctl --json status
```

From the host:

```sh
docker exec <container> restic-webctl --json status
```

Image defaults:

| Variable | Default |
| --- | --- |
| `DATABASE_URL` | (required, set by Coolify) |
| `RESTIC_WEB_DATA` | `/app/data` |

No password is required for CLI commands. Web UI login auth is unchanged.

---

## Global flags and environment

```
restic-webctl [global flags] <command> [args]
```

| Flag | Environment | Default |
| --- | --- | --- |
| `--database URL` | `DATABASE_URL` | required |
| `--data DIR` | `RESTIC_WEB_DATA` | `/app/data` if that directory exists, else `data` |
| `--json` | | JSON on stdout |
| `--quiet` / `-q` | | less human chatter |
| `--help` / `-h` | | help |

`--json`, `--database`, `--data`, and `--quiet` may appear before or after the
subcommand.

---

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | success |
| `1` | general / restic / validation failure |
| `2` | usage / bad flags / ambiguous id |
| `3` | reserved (web auth errors; rarely used by direct CLI) |
| `4` | not found |
| `5` | conflict / busy / not active |

With `--json`, failures still print one JSON object on **stdout**:

```json
{"ok":false,"code":"busy","error":"…","blockingRun":{…}}
```

and the process exits non-zero. Agents should check **both** the exit code and
`ok` / `code`.

---

## Web UI password (optional)

CLI ops do not need auth. These commands only manage the **web UI** password
stored in Postgres:

```sh
restic-webctl auth status
restic-webctl auth setup --password 'choose-a-password'   # first install only
restic-webctl auth passwd --current '…' --new '…'
```

---

## Quick start (same flow as the UI)

```sh
restic-webctl folder create --name home --path /data/home
restic-webctl repo create --name local --backend Local \
  --path /data/restic-repo --password 'repo-secret'
restic-webctl job create --name nightly --folder home --repo local

# start a backup and wait for completion (--wait is implied; CLI always waits
# because runs execute in this process — exiting early would orphan the run)
restic-webctl job run nightly --wait --json
```

Optional schedule + retention when creating/updating a job:

```sh
restic-webctl job update nightly \
  --schedule-enabled --schedule-kind daily --schedule-at 02:00 \
  --retention-enabled --retention-preset balanced
```

---

## Command reference

IDs may be a full id, a unique id prefix, or a unique **name**.

### `status`

Dashboard summary: restic availability/version, entity counts, active runs.

```sh
restic-webctl status
restic-webctl --json status
```

### `activity`

Active runs plus recent finished history (Activity page).

```sh
restic-webctl activity --limit 20
```

### `folder`

```sh
restic-webctl folder list
restic-webctl folder get <id|name>
restic-webctl folder create --name NAME --path PATH
restic-webctl folder update <id|name> [--name N] [--path P]
restic-webctl folder delete <id|name>
```

### `repo`

```sh
restic-webctl repo list
restic-webctl repo get <id|name>
restic-webctl repo create --name NAME --backend Local|S3 --password PASS […]
restic-webctl repo update <id|name> […]
restic-webctl repo delete <id|name>
restic-webctl repo test <id|name>
restic-webctl repo init <id|name> [--wait]
restic-webctl repo unlock <id|name>
restic-webctl repo snapshots <id|name>
restic-webctl repo restore <id|name> --snapshot ID --target PATH [--wait]
restic-webctl repo download <id|name> --snapshot ID [--wait]
```

Local create: `--path DIR`  
S3 create: `--endpoint URL --bucket NAME [--region R] --access-key K --secret-key S`

### `job`

```sh
restic-webctl job list
restic-webctl job get <id|name>
restic-webctl job create --name NAME --folder ID|NAME --repo ID|NAME […]
restic-webctl job update <id|name> […]
restic-webctl job delete <id|name>
restic-webctl job run <id|name> [--wait] [--follow]
restic-webctl job retention <id|name> [--wait] [--follow]
restic-webctl job runs <id|name> [--limit N]
restic-webctl job snapshots <id|name>
```

### `run`

```sh
restic-webctl run list [--status …] [--kind …] [--job …] [--limit N]
restic-webctl run get <runId>
restic-webctl run log <runId> [--after SEQ]
restic-webctl run watch <runId>          # follow until terminal
restic-webctl run stop <runId>
restic-webctl run download <runId> [-o FILE|-]
```

`--wait` on start commands polls the durable run record (and logs with
`--follow`) until the run finishes.

---

## Concurrent with the web UI

Both processes share Postgres:

- Entity CRUD is visible to both immediately.
- A run started by the CLI executes in the **CLI process**; one started by the UI
  executes in the **server** process.
- Starting a second operation on the same repository is blocked via durable
  “busy” checks.
- `run stop` can signal another process’s restic child when a PID is recorded.
- Live SSE in the browser only streams events from the server process; CLI-started
  runs still show up in DB-backed lists and history.

The CLI never runs startup reconcile or the scheduler (the server owns those).

---

## Agent checklist

1. Ensure `DATABASE_URL` is set (inherited in the Coolify container).
2. Prefer `--json` and check exit codes `0–5`.
3. Resolve entities by **name** when possible (`job run nightly`).
4. Use `job run … --wait --json` for synchronous backups.
5. On exit `5` with `code=busy`, read `blockingRun` and wait or `run stop`.
6. Do not rely on web login cookies or `RESTIC_WEB_PASSWORD` for CLI ops.
