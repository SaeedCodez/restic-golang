-- +goose Up

ALTER TABLE jobs ADD COLUMN retention JSONB;

-- +goose Down

ALTER TABLE jobs DROP COLUMN IF EXISTS retention;
