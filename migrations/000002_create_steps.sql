-- +goose Up
-- +goose StatementBegin
CREATE TABLE steps (
    run_id TEXT NOT NULL REFERENCES runs (id) ON DELETE RESTRICT,
    step_key TEXT NOT NULL CHECK (step_key <> ''),
    input_hash BYTEA NOT NULL CHECK (octet_length(input_hash) = 32),
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    status TEXT NOT NULL CHECK (status IN ('running', 'completed')),
    result JSONB,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (run_id, step_key),
    CHECK (
        (status = 'running' AND result IS NULL AND completed_at IS NULL)
        OR (
            status = 'completed'
            AND result IS NOT NULL
            AND completed_at IS NOT NULL
            AND completed_at >= started_at
        )
    )
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE steps;
-- +goose StatementEnd
