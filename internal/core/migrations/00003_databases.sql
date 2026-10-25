-- +goose Up

CREATE TABLE databases (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    engine        TEXT NOT NULL,
    container     TEXT NOT NULL,
    owner_kind    TEXT,
    owner_uuid    TEXT,
    owner_service TEXT,
    database_name TEXT NOT NULL
);

CREATE UNIQUE INDEX databases_name_lower ON databases (LOWER(name));

-- A job backs up exactly one source: a folder or a database.
ALTER TABLE jobs ALTER COLUMN folder_id DROP NOT NULL;
ALTER TABLE jobs ADD COLUMN database_id TEXT REFERENCES databases (id) ON DELETE RESTRICT;
ALTER TABLE jobs ADD CONSTRAINT jobs_one_source CHECK ((folder_id IS NULL) <> (database_id IS NULL));

-- +goose Down

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_one_source;
DELETE FROM jobs WHERE folder_id IS NULL;
ALTER TABLE jobs DROP COLUMN IF EXISTS database_id;
ALTER TABLE jobs ALTER COLUMN folder_id SET NOT NULL;
DROP TABLE IF EXISTS databases;
