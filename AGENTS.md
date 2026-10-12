# AGENTS.md

restic-web is a self-hosted web UI and CLI around [restic](https://restic.net).
Go backend (`internal/core`), React UI (`ui/`), Postgres for all durable state.

There are two ways an agent works with this project. Read the section that fits.

## Operating backups (check, back up, restore)

Use the control CLI **`restic-webctl`**. It talks to the same Postgres and core
logic as the web app directly: no HTTP, no login.

- Step-by-step operating guide: [`.claude/skills/restic-webctl/SKILL.md`](.claude/skills/restic-webctl/SKILL.md)
- Full command reference, exit codes, JSON shapes: [`docs/cli.md`](docs/cli.md)
- In production it runs inside the app container:
  `docker exec <container> restic-webctl --json status`

Rules that apply every time:

1. Always pass `--json` and check **both** the exit code and the `ok` field.
2. **Restore replaces the destination.** Files not in the snapshot are deleted
   from `--target`. Restore into a fresh scratch directory unless the user
   explicitly asked to overwrite a specific path.
3. `repo forget`, `repo reset`, `job forget`, `job delete --forget`,
   `repo delete`, `job delete`, `folder delete` are destructive. Get the user's
   explicit confirmation, naming the exact repo/job/snapshot, before running them.
4. Exit `5` with `code: "busy"` means another run holds the repository. Read
   `blockingRun`, then wait or ask before `run stop`.
5. Never print or log repository passwords or S3 secret keys.

## Developing the code

```sh
cp .env.example .env              # set DATABASE_URL (Postgres is required)
go build ./...
go test ./...                     # needs Postgres: DATABASE_URL or TEST_DATABASE_URL
go run ./cmd/restic-web           # server + embedded UI on 127.0.0.1:8080
go build -o restic-webctl ./cmd/restic-webctl
```

UI (only when changing the interface): `cd ui && npm install && npm run build`.
The build output goes to `cmd/restic-web/web/`, which is committed and embedded
with `go:embed`. Commit the rebuilt output together with UI source changes.

Layout:

```
cmd/restic-web/      server binary (embeds the built UI)
cmd/restic-webctl/   control CLI; one file per command group
internal/core/       app, stores, coordinator, runner, HTTP handlers
  coordinator.go     per-repository locking; Start* for every run kind
  runner.go          Runner interface + real restic runner
  migrations/        goose SQL migrations (run on startup)
ui/                  React + Tailwind + shadcn/ui source
docs/cli.md          CLI reference
```

Conventions:

- Every long operation is a **run** (`RunKind` in `model.go`) started through
  `Coordinator.Start*`, which serializes work per repository and persists logs.
  A new operation gets a run kind, a `Runner` method, a fake in
  `testsupport_test.go`, an API route in `server.go`, a CLI subcommand, a label
  in `ui/src/lib/runs.js`, and a line in `docs/cli.md` and `README.md`.
- All restic execution goes through the `Runner` interface, so tests use
  `fakeRunner` and need no restic binary.
- Run `gofmt` and `go test ./...` before calling a change done.
