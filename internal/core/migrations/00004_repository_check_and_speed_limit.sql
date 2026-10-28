-- +goose Up

ALTER TABLE repositories ADD COLUMN check_schedule JSONB;
ALTER TABLE repositories ADD COLUMN upload_limit_kib INTEGER NOT NULL DEFAULT 0;
ALTER TABLE repositories ADD COLUMN download_limit_kib INTEGER NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE repositories DROP COLUMN IF EXISTS download_limit_kib;
ALTER TABLE repositories DROP COLUMN IF EXISTS upload_limit_kib;
ALTER TABLE repositories DROP COLUMN IF EXISTS check_schedule;
