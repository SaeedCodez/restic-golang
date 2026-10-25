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
| `--timeout D` | | `10m`; limit for commands that wait on restic directly |
| `--quiet` / `-q` | | less human chatter |
| `--help` / `-h` | | help |

`--json`, `--database`, `--data`, `--timeout`, and `--quiet` may appear before
or after the subcommand.

`--timeout` bounds `repo test`, `repo unlock`, `repo snapshots`, and
`job snapshots`, which run restic and wait for its answer. It takes a duration
(`90s`, `30m`) or whole seconds; `0` means no limit. On a slow S3 backend these
can take minutes, so raise it rather than retrying. A timed-out call exits `1`
with `code: "timeout"`. Runs (backup, restore, check, forget, …) are not
bounded by it; stop one with `run stop`. Ctrl-C interrupts restic cleanly so it
removes its repository lock.

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

| `code` | Meaning |
| --- | --- |
| `busy` | another run holds the repository (`blockingRun` says which) |
| `timeout` | restic did not answer within `--timeout`; the message keeps restic's last output |
| `not_initialized` | no restic repository at that location yet |
| `restic_error` | restic failed; the message is restic's error |

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

### `database`

A database source is one database inside a PostgreSQL, MySQL or MariaDB
container. It is dumped and restored inside that container (`docker exec`)
with the credentials from the container's own environment, so the app stores
no database password. Needs the Docker socket.

```sh
restic-webctl database list
restic-webctl database get <id|name>
restic-webctl database create --name NAME --container CONTAINER --db DB [--engine postgres|mysql|mariadb]
restic-webctl database update <id|name> [--name N] [--db DB]
restic-webctl database delete <id|name>
```

Without `--engine`, `create` inspects the container: the engine comes from its
image (or Coolify's label) and the owning Coolify resource is recorded, so the
source still finds the container after Coolify recreates it. The global
`--database` flag is the Postgres URL of this app, so the database name is
`--db` and a job's database source is `--db-source`.

### `docker` (alias `coolify`)

```sh
restic-webctl docker status                 # socket reachable? Coolify host?
restic-webctl docker resources              # sites, volumes, databases; * = backed up
restic-webctl docker databases <container>  # databases inside a database container
```

`--json docker resources` returns, per resource: `key` (`service:<uuid>`,
`application:<uuid>`, `database:<uuid>`), `name`, `project`, `domains`,
`volumes[]` (`path` is where this app reaches it, `jobs` what backs it up),
`databases[]` (database containers) and `linkedDatabases[]` (databases the
site's environment points at, e.g. WordPress on a shared MariaDB).

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
restic-webctl repo check <id|name> [--read-data | --read-data-subset S] [--wait]
restic-webctl repo snapshots <id|name>
restic-webctl repo restore <id|name> --snapshot ID --target PATH [--wait]
# Restore replaces the destination: snapshot files overwrite, extras are deleted.
# When PATH equals a path stored in the snapshot (usual job-folder restore),
# the app uses restic --target / --include PATH so files land in place and
# extras under PATH are removed (restic forbids --delete with --target /
# unless a filter is set). The run first reads the snapshot to decide this
# ("Reading snapshot … to plan the restore" in its log); if that read fails,
# the restore fails rather than guessing.
restic-webctl repo restore <id|name> --snapshot ID --target PATH --include P [--include P2 …] [--wait]
# --include restores only those snapshot paths (files or folders). In place,
# only they are replaced; files inside a selected folder that are not in the
# snapshot are deleted, everything else is left alone.
restic-webctl repo restore <id|name> --snapshot ID --target PATH --stop-containers [--wait]
# Stops the running containers that use PATH (a site's volume) and starts them
# again afterwards, whatever the outcome.
restic-webctl repo files <id|name> --snapshot ID [--path DIR]   # list one folder of a snapshot
restic-webctl repo download <id|name> --snapshot ID [--include P …] [--wait]
restic-webctl repo restore-db <id|name> --snapshot ID --db-source ID|NAME [--into NAME] [--no-safety-backup] [--wait]
# Replaces the database with the dump in the snapshot (kind restore_db): it is
# dropped and recreated (owner and character set kept), then imported. First a
# safety snapshot of the current database is taken (tag resticweb-prerestore);
# if the import fails it is put back automatically. --into NAME restores into
# another database on the same server instead, leaving the live one alone.
restic-webctl repo forget <id|name> --snapshot ID [--wait]
restic-webctl repo reset <id|name> [--wait]
```

`repo check` runs `restic check` as a tracked run (kind `check`). Without flags
it verifies repository structure, indexes, snapshots and trees. `--read-data`
also reads and verifies every data pack (slow, downloads everything from S3);
`--read-data-subset` reads part of them: `10%`, `1/5`, or a size like `500M`.
If restic finds damage, the run fails with `error` "the repository check found
errors" and the run log (`run log <runId>`) holds restic's details and
suggested `restic repair …` commands. The app never runs repairs itself.

A run's log shows restic's stderr (backend retry warnings, errors). restic
prints nothing while it opens the repository and loads the index, so when a run
has been silent for a minute, and again each time the silence doubles, the log
notes "No output from restic for …; it is still running (pid N)". On S3 that
usually means restic is waiting on the storage backend.

Local create: `--path DIR`  
S3 create: `--endpoint URL --bucket NAME [--region R] --access-key K --secret-key S`

### `job`

```sh
restic-webctl job list
restic-webctl job get <id|name>
restic-webctl job create --name NAME --folder ID|NAME --repo ID|NAME […]
restic-webctl job create --name NAME --db-source ID|NAME --repo ID|NAME […]
restic-webctl job update <id|name> […]
restic-webctl job delete <id|name> [--forget]
restic-webctl job run <id|name> [--wait] [--follow]
restic-webctl job retention <id|name> [--wait] [--follow]
restic-webctl job forget <id|name> [--delete-job] [--wait] [--follow]
restic-webctl job runs <id|name> [--limit N]
restic-webctl job snapshots <id|name>
```

A database job's backup dumps the database inside its container and stores it
as one file, `<database>.sql`, tagged `resticweb-db:<engine>`. If the dump
fails, no snapshot is saved and the run fails with the dump tool's message.

`job delete` removes the job from the app and leaves restic snapshots in place.
`job delete --forget` (or `job forget --delete-job`) forgets that job's tagged
snapshots, prunes unused data, then deletes the job. `job forget` without
`--delete-job` only removes the snapshots. `repo forget --snapshot` deletes one
snapshot; `repo reset` forgets every snapshot in the repository.

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

Agents that open this repository pick up [`AGENTS.md`](../AGENTS.md) (and
`CLAUDE.md` for Claude Code). The step-by-step operating guide is the skill at
[`.claude/skills/restic-webctl/SKILL.md`](../.claude/skills/restic-webctl/SKILL.md).
For an agent that manages a server without this checkout, copy that folder to
`~/.claude/skills/restic-webctl/` on the agent's machine.

1. Ensure `DATABASE_URL` is set (inherited in the Coolify container).
2. Prefer `--json` and check exit codes `0–5`.
3. Resolve entities by **name** when possible (`job run nightly`).
4. Use `job run … --wait --json` for synchronous backups.
5. On exit `5` with `code=busy`, read `blockingRun` and wait or `run stop`.
6. To verify backups, run `repo check <repo> --read-data-subset 10% --json`;
   a failed run means damage (or an unreachable repo) — read `run log <runId>`.
7. Do not rely on web login cookies or `RESTIC_WEB_PASSWORD` for CLI ops.
8. `repo restore-db` without `--into` drops the live database: get the user's
   explicit confirmation naming the database first. To inspect a backup, use
   `--into <name>_restored`.
