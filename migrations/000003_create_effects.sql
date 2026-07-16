-- +goose Up
-- +goose StatementBegin
CREATE TABLE effects (
    idempotency_key TEXT PRIMARY KEY CHECK (idempotency_key <> ''),
    run_id TEXT NOT NULL REFERENCES runs (id) ON DELETE RESTRICT,
    step_key TEXT NOT NULL CHECK (step_key <> ''),
    effect_type TEXT NOT NULL CHECK (effect_type <> ''),
    result JSONB NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX effects_run_step_idx ON effects (run_id, step_key);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE effects;
-- +goose StatementEnd
