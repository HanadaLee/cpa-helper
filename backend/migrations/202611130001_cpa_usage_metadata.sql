-- +goose Up
ALTER TABLE usage_records ADD COLUMN cpa_metadata_json TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE usage_records DROP COLUMN cpa_metadata_json;
