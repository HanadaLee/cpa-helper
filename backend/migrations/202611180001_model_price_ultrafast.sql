-- +goose Up
ALTER TABLE model_prices ADD COLUMN ultrafast_enabled INTEGER NOT NULL DEFAULT 0 CHECK (ultrafast_enabled IN (0, 1));
ALTER TABLE model_prices ADD COLUMN ultrafast_multiplier REAL NOT NULL DEFAULT 1 CHECK (ultrafast_multiplier > 0);
ALTER TABLE model_prices ADD COLUMN long_context_ultrafast_unsupported INTEGER NOT NULL DEFAULT 0 CHECK (long_context_ultrafast_unsupported IN (0, 1));

-- +goose Down
ALTER TABLE model_prices DROP COLUMN long_context_ultrafast_unsupported;
ALTER TABLE model_prices DROP COLUMN ultrafast_multiplier;
ALTER TABLE model_prices DROP COLUMN ultrafast_enabled;
