# restic-webctl — CLI usage guide

`restic-webctl` is the control CLI for a running **restic-web** server. It talks
to the same JSON HTTP API as the web UI, so every screen capability is available
from a shell — including on a Coolify/Docker host where AI agents or operators
run commands via `docker exec`.

Default output is human-readable tables. Pass **`--json`** for stable,
machine-readable responses (recommended for agents and scripts).

---

## Install

### Local build

```sh
go build -o restic-web .
go build -o restic-webctl ./cmd/restic-webctl
```

The CLI expects the web server to already be running (default
`http://127.0.0.1:8080`).

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
| `RESTIC_WEB_URL` | `http://127.0.0.1:8080` |
| `RESTIC_WEB_SESSION_FILE` | `/app/data/.restic-webctl-session` |

Set `RESTIC_WEB_PASSWORD` in the container environment (same password as the UI
login) so non-interactive agents can authenticate without prompts.

---

## Global flags and environment

```
restic-webctl [global flags] <command> [args]
```

| Flag | Environment | Default |
| --- | --- | --- |
| `--url URL` | `RESTIC_WEB_URL` | `http://127.0.0.1:$PORT` or `:8080` |
| `--password PASS` | `RESTIC_WEB_PASSWORD` | (prompt if needed) |
| `--session-file PATH` | `RESTIC_WEB_SESSION_FILE` | see below |
| `--json` | | JSON on stdout |
| `--quiet` / `-q` | | less human chatter |
| `--timeout DURATION` | | `30s` (use `0` only via code; downloads disable the limit) |
| `--help` / `-h` | | help |

`--json`, `--url`, `--timeout`, `--session-file`, and `--quiet` may appear
**before or after** the subcommand. `--password` (login password) should stay
**before** the command so it does not clash with `repo create --password`.

**Session file defaults**

1. `/app/data/.restic-webctl-session` if `/app/data` exists (Docker volume)
2. `$XDG_CONFIG_HOME/restic-web/session`
3. `~/.config/restic-web/session`

On first protected API call, if the session is missing/expired and a password is
available, the CLI logs in, stores the `restic_session` cookie, and retries.

---

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | success |
| `1` | general / server / restic failure |
| `2` | usage / bad flags / ambiguous id |
| `3` | auth failure or setup required |
| `4` | not found |
| `5` | conflict / busy / canceled |

With `--json`, failures still print one JSON object on **stdout**:

```json
{"ok":false,"code":"busy","error":"…","blockingRun":{…}}
```

and the process exits non-zero. Agents should check **both** the exit code and
`ok` / `code`.

---

## Authentication

```sh
restic-webctl auth status
restic-webctl auth setup --password 'choose-a-password'   # first install only
restic-webctl auth login --password '…'
restic-webctl auth logout
restic-webctl auth passwd --current '…' --new '…'
```

Prefer `RESTIC_WEB_PASSWORD` over putting secrets on the command line (shell
history).

---

## Quick start (same flow as the UI)

```sh
export RESTIC_WEB_PASSWORD='…'

restic-webctl folder create --name home --path /data/home
restic-webctl repo create --name local --backend Local \
  --path /data/restic-repo --password 'repo-secret'
restic-webctl job create --name nightly --folder home --repo local

# start a backup and wait for completion
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
restic-webctl repo create --name NAME --backend Local --path DIR --password PASS
restic-webctl repo create --name NAME --backend S3 \
  --endpoint URL --bucket B --access-key K --secret-key S --password PASS \
  [--region R]
restic-webctl repo update <id|name> [fields…]   # omitted secrets are kept
restic-webctl repo delete <id|name>
restic-webctl repo test <id|name>
restic-webctl repo init <id|name> [--wait] [--follow]
restic-webctl repo unlock <id|name>
restic-webctl repo snapshots <id|name>
restic-webctl repo restore <id|name> --snapshot ID --target DIR [--wait]
restic-webctl repo download <id|name> --snapshot ID [--wait]
```

List/get never print repository passwords or S3 secret keys (`hasPassword` /
`hasSecretKey` only).

### `job`

```sh
restic-webctl job list
restic-webctl job get <id|name>
restic-webctl job create --name NAME --folder ID|NAME --repo ID|NAME [schedule] [retention]
restic-webctl job update <id|name> [fields…]
restic-webctl job delete <id|name>
restic-webctl job run <id|name> [--wait] [--follow]
restic-webctl job retention <id|name> [--wait] [--follow]
restic-webctl job runs <id|name> [--limit N]
restic-webctl job snapshots <id|name>
```

**Schedule flags:** `--schedule-enabled` / `--schedule-disabled`,  
`--schedule-kind hourly|every|daily|weekly`, `--every 6h`,  
`--schedule-at HH:MM`, `--weekdays 1,2,3,4,5` (0 = Sunday).

**Retention flags:** `--retention-enabled` / `--retention-disabled`,  
`--retention-preset light|balanced|long|custom`,  
`--keep-last` `--keep-hourly` `--keep-daily` `--keep-weekly` `--keep-monthly`  
`--keep-within-days`.

### `run`

```sh
restic-webctl run list [--status active|finished|STATUS] [--kind KIND] [--job ID|NAME] [--limit N]
restic-webctl run get <runId>
restic-webctl run log <runId> [--after N] [--follow]
restic-webctl run watch <runId>
restic-webctl run stop <runId>
restic-webctl run download <runId> -o file.zip
```

`job run` / `repo init` / restore / download / retention print a `runId`. Use
`--wait` to block until the run finishes, or `--follow` to stream the log while
waiting. `run watch` always follows the log until completion.

---

## For AI agents

1. Prefer **`--json`** on every command.
2. Set `RESTIC_WEB_URL` and `RESTIC_WEB_PASSWORD` in the environment (do not
   echo passwords into logs).
3. Treat exit codes as authoritative; also read `code` on failure
   (`busy`, `not_found`, `setup_required`, `no_restic`, `not_initialized`, …).
4. On `busy` (exit `5`), read `blockingRun` and either wait/stop that run or
   retry later.
5. After start operations (`job run`, `repo init`, restore, download, retention),
   capture `runId` from the JSON body and poll with
   `run get` / `run watch --json` / `job run … --wait --json`.
6. Resolve entities by **name** when unique (`--folder home`); use ids when
   names collide.
7. Do not assume theme/settings UI options exist in the CLI — they are
   browser-only.

### Minimal agent recipe

```sh
export RESTIC_WEB_URL=http://127.0.0.1:8080
export RESTIC_WEB_PASSWORD=…

restic-webctl --json auth status
restic-webctl --json status
restic-webctl --json job list
restic-webctl --json job run <id> --wait
```

### Coolify

```sh
# replace <container> with the Coolify service container name/id
docker exec <container> restic-webctl --json status
docker exec -e RESTIC_WEB_PASSWORD="$RESTIC_WEB_PASSWORD" <container> \
  restic-webctl --json job run nightly --wait
```

---

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| `setup_required` | `auth setup --password …` once |
| `unauthorized` / exit 3 | `auth login` or set `RESTIC_WEB_PASSWORD` |
| `could not reach …` | Is `restic-web` up? Check `--url` / `PORT` |
| `no_restic` | Install `restic` in the image/host and restart the server |
| `not_initialized` | `repo init <id> --wait` or run a backup (auto-init on first backup) |
| `busy` | Another run holds the repository; `run get` / `run stop` the blocker |
| Ambiguous name (exit 2) | Pass a longer id prefix or the full id |

Live log following uses **polling** (not SSE), which is reliable behind Docker
and reverse proxies.
