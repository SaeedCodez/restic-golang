# Production readiness — restic-web

Date: 2026-08-27  
Environment: Docker Compose on macOS (Apple Silicon), Local restic backend, disposable fixtures under `/app/data/e2e-*` inside the app container.

## 1. Summary

**Verdict: ready for single-user local / self-hosted use**, with the caveats below.

Core backup → incremental → restore → download, auth, contention, schedules, Docker health, CLI operator path, and mid-run restart reconciliation all worked end-to-end against a real restic repository in this pass. Several clear bugs were found and fixed (see §2). Remaining issues are mostly hardening, documentation drift, or product choices — not silent data-loss paths in the flows exercised.

**Top residual risks**

1. **Docker image ships Debian’s restic 0.14.0**, which lacks modern exit codes and some JSON fields. Auto-init now has a stderr fallback (fixed this pass), but upgrading restic in the image is still recommended.
2. **Sessions are in-memory** — any app restart forces re-login (by design today). Durable auth password is fine; cookies are not.
3. **CLI runs execute inside the CLI process** — fire-and-forget without waiting previously orphaned durable runs (fixed by always waiting). There is still no true detach/daemon mode.
4. **Folder paths are not validated on create** — a missing path is accepted and only fails at backup time.
5. **README “Architecture” still describes the old JSON-on-disk store**; durable state is Postgres.

Stack left running: `http://127.0.0.1:8080` (UI login password from this pass: `testpass3`).

---

## 2. Fixed in this pass

| Fix | What changed | How verified |
| --- | --- | --- |
| **CLI `--json --wait` exited 0 on failed runs** | `cmd/restic-webctl/wait.go` now returns `runStatusExit(status)` after writing JSON (`ok:false` + exit 1 on failure). | `job run badpath-job --wait` → JSON `ok:false`, process exit `1`. |
| **Auto-init broken on restic 0.14 (Docker)** | Older restic returns exit `1` + “unable to open config…”, not exit `10`. `streamRestic` remaps via `classifyResticFailure`. | Fresh Local repo + `job run … --wait`: log shows auto-init then success; exit 0. |
| **CLI `job run` without `--wait` left ghost runs** | Runs execute in the CLI process; exiting early killed restic and left `starting`/`running` forever (blocked repo until server restart). CLI **always waits** now; `docs/cli.md` updated. | Reproduced ghost `starting` run + busy lock; after fix, start commands wait to terminal. |
| **Docker restic cache `mkdir /home/app` permission denied** | Dockerfile sets `HOME=/app`, `RESTIC_CACHE_DIR=/app/data/cache`, creates cache dir. | Cache populated under `/app/data/cache`; warning gone on subsequent backups. |
| **`CreatedAt` nanosecond vs Postgres timestamptz** | Entity Create/Update timestamps truncated to microseconds so in-memory values match DB. | `go test ./...` (was failing `TestEntityStoreUpdate`) now passes. |

---

## 3. Open issues

### High

_(none remaining after this pass for the single-user Local path)_

### Medium

1. **Folder create accepts non-existent paths**  
   - **Repro:** `restic-webctl folder create --name bad --path /no/such/path` → exit 0.  
   - **Expected:** validation error (or at least a warning).  
   - **Actual:** stored; backup fails later with restic exit 1.  
   - **Suggestion:** `os.Stat` on create/update for Local paths; surface clear UI/CLI error.

2. **Docker image restic is 0.14.0**  
   - **Repro:** `docker exec … restic version`.  
   - **Expected:** modern restic (≥0.17) with exit codes 10/11/12 and richer snapshot JSON.  
   - **Actual:** Debian bookworm package; snapshot list shows `sizeBytes:0` / `fileCount:0`.  
   - **Suggestion:** install official multi-arch restic release in Dockerfile (`TARGETARCH`).

3. **README Architecture section is stale**  
   - Describes `data/repositories.json`, `runs/<id>/run.json`, etc.  
   - Actual durable state is Postgres (`repositories`, `folders`, `jobs`, `runs`, `run_log_lines`); `/app/data` is for downloads/cache.  
   - **Suggestion:** rewrite that section to match Postgres + volumes.

4. **No host bind mounts for backup sources in default Compose**  
   - Only `appdata` → `/app/data`. Host folders to back up must be mounted by the operator (override) or live under `/app/data`.  
   - Easy to miss; document a Compose example with `/Users/...:/backup:ro`.

5. **Sessions not durable across restarts**  
   - Documented in code (`SessionManager` is in-memory). Fine for single-user local, surprising if treated like a long-lived “always logged in” appliance.  
   - Product choice: persist sessions in Postgres or accept re-login.

### Low

6. **Multiple repository entities can share the same Local path with different passwords**  
   - Create `wrongpw` pointing at an existing repo path → allowed; backup fails with “wrong repository password” (correctly classified).  
   - Consider uniqueness warning on `(backend, path)`.

7. **CLI auth password prompts may echo**  
   - Prompt text already says so; prefer `--password` / env for operators.

8. **`useradd` warning in image build** (`uid 1001 > SYS_UID_MAX 999`)**  
   - Cosmetic; use a UID ≤999 or non-system useradd flags.

9. **AuthStore is memory-cached**  
   - Manually deleting the `auth` row does not reset setup until process restart. Only matters for DB surgery.

10. **S3 path not exercised**  
    - No credentials available; Local only. Env-based credential injection is unit-tested.

---

## 4. Test matrix

| Area | Result | Notes |
| --- | --- | --- |
| **Docker Compose build/up** | Pass | `db` + `app` healthy; `:8080` published via override |
| **`GET /api/auth/status` + UI shell** | Pass | 200; SPA loads |
| **App restart** | Pass | State intact; sessions require re-login; active runs → `interrupted` |
| **Auth: setup / login / logout / change password** | Pass | API + Playwright UI (password → `testpass3`) |
| **UI: create inventory** | Pass (via CLI data; UI forms exercised) | Folders/Repos/Jobs pages render; New job / Add dialogs open |
| **Backup + live progress + log** | Pass | UI run page + Activity; history survives refresh |
| **Incremental backup** | Pass | `filesChanged` / `filesUnmodified` reflected in summary |
| **Restore to folder** | Pass | Files under `/app/data/e2e-restore/...` |
| **Download zip** | Pass | `repo download` + `run download -o …/out.zip` |
| **Schedule** | Pass | `--schedule-enabled --schedule-kind every --every 1h`; Dashboard **Scheduled: 1**; job `scheduleState: scheduled` |
| **Contention** | Pass | CLI exit **5** + `blockingRun`; API 409 busy; UI toast path + “View progress” when already running |
| **Theme toggle** | Pass | “Change theme” → Light; screenshot OK |
| **Responsive (narrow)** | Pass | Mobile viewport dashboard usable |
| **Second tab / refresh** | Pass | Activity + run detail consistent |
| **CLI status / CRUD / run / snapshots** | Pass | `--json`; same DB as UI |
| **CLI not-found** | Pass | Exit **4** |
| **CLI busy** | Pass | Exit **5** |
| **CLI failed run exit** | Pass | Exit **1** after fix |
| **Wrong repo password** | Pass | Run `failed`, error “wrong repository password”, restic exit 12 remapped |
| **Mid-run restart (no ghost running)** | Pass | Status `interrupted`; `activeRuns: []` |
| **Secrets in process args** | Pass | No password/AWS secret in `/proc/*/cmdline`; CLI repo JSON redacts password |
| **`go test ./...`** | Pass | Against dedicated `restic_test` DB on Compose network |
| **S3-compatible backend** | Not run | No credentials in environment |
| **Cloud browser against localhost** | N/A | Used local Playwright + Chrome instead |

---

## 5. Residual risk — not fully verified

- **S3 (or other remote) backends** — credential wiring is coded/tested in unit tests; no live S3 E2E.
- **Long-running backups / large repos** — fixtures were small (tens of MB); progress UI and lock behaviour under multi-hour load not soaked.
- **Scheduler firing on the clock** — schedule was enabled and shown as upcoming; did not wait a full hour for an automatic trigger.
- **Retention forget+prune against a rich snapshot history** — retention UI/CLI flags exist; not fully exercised on a multi-snapshot timeline in this pass.
- **Coolify / Traefik production proxy path** — Compose local only; Coolify FQDN env not tested.
- **Multi-arch Docker image on amd64** — verified on arm64 Mac.
- **Concurrent CLI + UI writers under stress** — basic busy checks passed; no race soak.
- **SSE reconnect under proxy buffering** — local direct `:8080` only.
- **Backup of paths outside the container** — requires operator volume mounts (see open issue #4).

---

## Operator notes from this pass

```sh
# Stack (already up after this pass)
docker compose ps
open http://127.0.0.1:8080   # password: testpass3

# CLI
docker exec restic-golang-app-1 restic-webctl --json status
docker exec restic-golang-app-1 restic-webctl --json job run auto-job --wait

# Tests (needs restic_test DB on compose network)
docker exec restic-golang-db-1 psql -U restic -d restic -c 'CREATE DATABASE restic_test;'  # once
docker run --rm --network restic-golang_default -v "$PWD":/src -w /src \
  -e TEST_DATABASE_URL='postgres://restic:restic@db:5432/restic_test?sslmode=disable' \
  -e DATABASE_URL='postgres://restic:restic@db:5432/restic_test?sslmode=disable' \
  golang:1.25-bookworm go test ./... -count=1
```

Disposable E2E data lives under the `appdata` volume (`/app/data/e2e-*`). Do not point cleanup at non-fixture repos.
