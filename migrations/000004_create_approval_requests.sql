-- +goose Up
-- +goose StatementBegin
ALTER TABLE runs
DROP CONSTRAINT runs_status_check;

ALTER TABLE runs
ADD CONSTRAINT runs_status_check
CHECK (status IN ('pending', 'running', 'waiting', 'succeeded', 'failed', 'canceled'));
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE approval_requests (
    id TEXT PRIMARY KEY CHECK (id <> ''),
    run_id TEXT NOT NULL REFERENCES runs (id) ON DELETE RESTRICT,
    step_key TEXT NOT NULL CHECK (step_key <> ''),
    call_id TEXT NOT NULL CHECK (call_id <> ''),
    tool_name TEXT NOT NULL CHECK (tool_name <> ''),
    status TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
    requested_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    UNIQUE (run_id, call_id),
    CHECK (
        (status = 'pending' AND resolved_at IS NULL)
        OR (
            status IN ('approved', 'rejected')
            AND resolved_at IS NOT NULL
            AND resolved_at >= requested_at
        )
    )
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX approval_requests_one_pending_per_run_idx
ON approval_requests (run_id)
WHERE status = 'pending';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE approval_requests;

ALTER TABLE runs
DROP CONSTRAINT runs_status_check;

ALTER TABLE runs
ADD CONSTRAINT runs_status_check
CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'canceled'));
-- +goose StatementEnd
