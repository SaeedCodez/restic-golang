# Production readiness — restic-web

Date: 2026-08-27  
Environment: Docker Compose on macOS (Apple Silicon), Local restic backend plus MinIO S3-compatible backend, disposable fixtures under `/app/data/e2e-*` inside the app container.

## 1. Summary

**Verdict: ready for single-user local / self-hosted use**, with the caveats below.

Core backup → incremental → restore → download, auth, contention, schedules, Docker health, CLI operator path, and mid-run restart reconciliation all worked end-to-end against a real restic repository in this pass. **S3-compatible (MinIO on Docker) was also verified**: auto-init, backup, incremental, restore, and `repo test` succeeded. Several clear bugs were found and fixed (see §2). Remaining issues are mostly hardening, documentation drift, or product choices — not silent data-loss paths in the flows exercised.

**Top residual risks**

1. **Docker image ships Debian’s restic 0.14.0**, which lacks modern exit codes and some JSON fields. Auto-init now has a stderr fallback (fixed this pass), but upgrading restic in the image is still recommended.
2. **Sessions are in-memory** — any app restart forces re-login (by design today). Durable auth password is fine; cookies are not.
3. **CLI runs execute inside the CLI process** — fire-and-forget without waiting previously orphaned durable runs (fixed by always waiting). There is still no true detach/daemon mode.
4. **Folder paths are not validated on create** — a missing path is accepted and only fails at backup time.
5. **README “Architecture” still describes the old JSON-on-disk store**; durable state is Postgres.
6. **Bad S3 credentials can be misclassified as “not initialized”** (Access Denied text still triggers auto-init attempt; init then fails). Backup correctly fails, but the error messaging is confusing.

Stack left running: `http://127.0.0.1:8080` (UI login password from this pass: `testpass3`). MinIO (optional, for S3 E2E): container `restic-minio` on the compose network, API `localhost:9000`, console `localhost:9001` (`minioadmin` / `minioadmin`).

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

6. **Wrong S3/MinIO secret key reported as “not initialized”**  
   - **Repro:** create an S3 repo with a bad `--secret-key`, run backup.  
   - **Expected:** clear “access denied” / bad credentials failure without auto-init.  
   - **Actual:** stderr `Access Denied` is remapped like a missing repo; auto-init is attempted, then fails with `BucketExists: Access Denied`; run ends as `repository is not initialized`.  
   - **Suggestion:** treat `access denied` / `InvalidAccessKeyId` / `SignatureDoesNotMatch` as a distinct auth failure before auto-init.

### Low

7. **Multiple repository entities can share the same Local path with different passwords**  
   - Create `wrongpw` pointing at an existing repo path → allowed; backup fails with “wrong repository password” (correctly classified).  
   - Consider uniqueness warning on `(backend, path)`.

8. **CLI auth password prompts may echo**  
   - Prompt text already says so; prefer `--password` / env for operators.

9. **`useradd` warning in image build** (`uid 1001 > SYS_UID_MAX 999`)**  
   - Cosmetic; use a UID ≤999 or non-system useradd flags.

10. **AuthStore is memory-cached**  
    - Manually deleting the `auth` row does not reset setup until process restart. Only matters for DB surgery.

11. **S3 access key appears in API/CLI JSON** (secret key is redacted via `hasSecretKey`)  
    - Usually acceptable; document if you treat access keys as sensitive.

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
| **S3-compatible (MinIO Docker)** | Pass | Auto-init, backup, incremental, restore, `repo test`; objects visible in bucket; API start-run OK |
| **S3 bad credentials** | Partial | Fails safely, but mislabeled as not-initialized (see open issue #6) |
| **Cloud browser against localhost** | N/A | Used local Playwright + Chrome instead |

---

## 5. Residual risk — not fully verified

- **Real AWS/GCS/R2 endpoints** — MinIO path-style local S3 was verified; vendor-specific auth/IAM quirks were not.
- **Long-running backups / large repos** — fixtures were small (tens of MB); progress UI and lock behaviour under multi-hour load not soaked.
- **Scheduler firing on the clock** — schedule was enabled and shown as upcoming; did not wait a full hour for an automatic trigger. (Deferred follow-up.)
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

### MinIO S3 E2E (how this was run)

```sh
# MinIO on the compose network (left running after this pass)
docker run -d --name restic-minio --network restic-golang_default \
  -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
  minio/minio server /data --console-address ":9001"

docker run --rm --network restic-golang_default --entrypoint /bin/sh minio/mc -c '
  mc alias set local http://restic-minio:9000 minioadmin minioadmin &&
  mc mb -p local/restic-web-e2e
'

docker exec restic-golang-app-1 restic-webctl --json repo create \
  --name minio-e2e --backend S3 \
  --endpoint http://restic-minio:9000 --bucket restic-web-e2e --region us-east-1 \
  --access-key minioadmin --secret-key minioadmin --password 's3-repo-secret'

docker exec restic-golang-app-1 restic-webctl --json job create \
  --name s3-job --folder e2e-folder --repo minio-e2e
docker exec restic-golang-app-1 restic-webctl --json job run s3-job --wait
```
