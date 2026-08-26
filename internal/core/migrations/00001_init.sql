-- +goose Up

CREATE TABLE auth (
    id         SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    salt       BYTEA NOT NULL,
    hash       BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE repositories (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL,
    backend_type TEXT NOT NULL,
    local_path   TEXT,
    endpoint     TEXT,
    bucket       TEXT,
    region       TEXT,
    access_key   TEXT,
    secret_key   TEXT,
    password     TEXT NOT NULL
);

CREATE UNIQUE INDEX repositories_name_lower ON repositories (LOWER(name));

CREATE TABLE folders (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    path       TEXT NOT NULL
);

CREATE UNIQUE INDEX folders_name_lower ON folders (LOWER(name));

CREATE TABLE jobs (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    folder_id      TEXT NOT NULL REFERENCES folders (id) ON DELETE RESTRICT,
    repository_id  TEXT NOT NULL REFERENCES repositories (id) ON DELETE RESTRICT,
    schedule       JSONB
);

CREATE UNIQUE INDEX jobs_name_lower ON jobs (LOWER(name));

CREATE TABLE runs (
    id             TEXT PRIMARY KEY,
    kind           TEXT NOT NULL,
    status         TEXT NOT NULL,
    job_id         TEXT,
    repository_id  TEXT NOT NULL,
    job_name       TEXT,
    folder_path    TEXT,
    repo_name      TEXT NOT NULL,
    params         JSONB,
    started_at     TIMESTAMPTZ NOT NULL,
    finished_at    TIMESTAMPTZ,
    pid            INTEGER,
    pid_start      TEXT,
    progress       JSONB NOT NULL DEFAULT '{}',
    summary        JSONB,
    exit_code      INTEGER,
    error          TEXT
);

CREATE INDEX runs_job_started ON runs (job_id, started_at DESC, id DESC);
CREATE INDEX runs_started ON runs (started_at DESC, id DESC);
CREATE INDEX runs_status_kind_started ON runs (status, kind, started_at DESC);
CREATE INDEX runs_active ON runs (status) WHERE status IN ('starting', 'running');

CREATE TABLE run_log_lines (
    run_id  TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    seq     BIGINT NOT NULL,
    ts      TIMESTAMPTZ NOT NULL,
    stream  TEXT,
    level   TEXT NOT NULL,
    message TEXT NOT NULL,
    PRIMARY KEY (run_id, seq)
);

-- +goose Down

DROP TABLE IF EXISTS run_log_lines;
DROP TABLE IF EXISTS runs;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS folders;
DROP TABLE IF EXISTS repositories;
DROP TABLE IF EXISTS auth;
