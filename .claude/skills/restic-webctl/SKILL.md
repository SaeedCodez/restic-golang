---
name: restic-webctl
description: Operate restic-web backups from a shell with the restic-webctl CLI — check backup health, verify repository integrity, run backups, list snapshots, restore or download snapshots, inspect run logs, and apply retention. Use when asked to check, verify, run, restore, or troubleshoot backups managed by restic-web, locally or inside its Docker/Coolify container.
---

# Operating restic-web with restic-webctl

`restic-webctl` drives restic-web directly through its Postgres database and
shared core logic. No HTTP, no web login. Every long operation (backup, restore,
check, forget…) is a **run** with a durable record and log.

## 1. Find how to invoke it

Try in this order and use the first that works for every later command:

```sh
restic-webctl --json status                                  # inside the container / PATH
docker exec <container> restic-webctl --json status          # from the Docker host
go run ./cmd/restic-webctl --json status                     # from a source checkout
```

To find the container: `docker ps --format '{{.Names}} {{.Image}}'` and pick the
restic-web app container (not its Postgres). It needs `DATABASE_URL`; the
container already has it. `status` must report restic as available.

## 2. Rules

- Always pass `--json`. Success requires exit code `0` **and** `"ok": true`.
- Exit codes: `0` ok · `1` failed (restic/validation) · `2` usage or ambiguous id ·
  `4` not found · `5` busy/conflict/stopped.
- Entities can be referenced by name, full id, or unique id prefix.
- Start commands (`job run`, `repo restore`, `repo check`…) always wait for the
  run to finish. Do not background or kill them: that orphans the run and blocks
  the repository.
- **Destructive — ask the user first, naming the exact target:** `repo forget`,
  `repo reset`, `job forget`, `job delete`, `repo delete`, `folder delete`, and
  any restore whose `--target` already holds data.
- Never echo repository passwords or S3 keys back to the user.

## 3. Workflows

### Health overview

```sh
restic-webctl --json status            # restic version, counts, active runs
restic-webctl --json job list          # each job: lastRun, schedule health
restic-webctl --json run list --status failed --limit 20
```

Report per job: last run status and time, and whether the schedule is overdue.
For any failed run, read its log (below) and summarize the cause.

### Verify backups are restorable

```sh
restic-webctl --json repo list
restic-webctl --json repo check <repo>                            # structure only, fast
restic-webctl --json repo check <repo> --read-data-subset 10%     # also reads 10% of data
```

`--read-data` reads everything: slow, and on S3 it downloads the whole
repository. Only use it when the user asks. A failed check means damage or an
unreachable repository; read the log. The log may suggest `restic repair …`
commands. Do not run them; show them to the user.

### Run a backup

```sh
restic-webctl --json job run <job>
```

`success_warnings` means a snapshot was made but some files were unreadable;
the log lists them. If retention is enabled on the job, a retention run starts
automatically afterwards.

### List snapshots

```sh
restic-webctl --json job snapshots <job>     # this job's snapshots
restic-webctl --json repo snapshots <repo>   # everything in the repository
```

Each snapshot has `id`, `shortId`, `time`, `paths`, `hostname`, `tags`, and
usually `sizeBytes` / `fileCount`.

### Restore

Restore **replaces** the destination: files not in the snapshot are deleted.
When `--target` equals the path stored in the snapshot, it restores in place
over the live folder.

Safe default: restore into a new empty directory, then let the user compare or
copy what they need.

```sh
restic-webctl --json repo restore <repo> --snapshot <id> --target /app/data/restore-<date>
```

Only restore over a live path after the user explicitly confirms that exact
path and snapshot. To give the user a zip instead:

```sh
restic-webctl --json repo download <repo> --snapshot <id>
restic-webctl run download <runId> -o snapshot.zip
```

### Read a run's log

```sh
restic-webctl --json run get <runId>      # status, exitCode, error, summary
restic-webctl --json run log <runId>      # lines: [{seq, ts, level, stream, message}]
```

Focus on `level` `error` and `warn` lines and the last few lines.

### Retention and forgetting

```sh
restic-webctl --json job retention <job>                 # apply the job's keep policy now
restic-webctl --json repo forget <repo> --snapshot <id>  # destructive: confirm first
```

## 4. Troubleshooting

| Symptom | Action |
| --- | --- |
| exit `5`, `code: "busy"` | Another run holds the repo. Read `blockingRun`; wait with `run watch <runId>`, or ask before `run stop <runId>`. |
| "repository is locked" | A crashed restic left a lock. `repo unlock <repo>` removes only stale locks, so it is safe. |
| exit `1`, `code: "timeout"` | restic did not answer within `--timeout` (default `10m`) on `repo test`/`unlock`/`snapshots` or `job snapshots`. S3 can be that slow: retry with `--timeout 30m`. The message keeps restic's last output. |
| A run's log is quiet, or says "No output from restic for …" / "Still waiting for restic …" | restic is alive and waiting on the storage backend. Wait, or ask before `run stop <runId>`. |
| "wrong repository password" | The stored password is wrong. Ask the user; do not guess. |
| "repository is not initialized" | `repo test <repo>` to confirm, then ask before `repo init <repo>`. |
| exit `2`, ambiguous id | Use the full id from `repo list` / `job list`. |
| `run interrupted` | The process owning the run restarted mid-run. Re-run the operation. |

Full command reference: `restic-webctl --help`, `restic-webctl <command> --help`,
and `docs/cli.md` in the restic-web repository.
