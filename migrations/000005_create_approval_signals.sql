-- +goose Up
-- +goose StatementBegin
ALTER TABLE approval_requests
ADD CONSTRAINT approval_requests_id_run_id_unique UNIQUE (id, run_id);

CREATE TABLE approval_signals (
    id TEXT PRIMARY KEY CHECK (id <> ''),
    request_id TEXT NOT NULL UNIQUE,
    run_id TEXT NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),
    received_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (request_id, run_id)
        REFERENCES approval_requests (id, run_id)
        ON DELETE RESTRICT
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE approval_signals;

ALTER TABLE approval_requests
DROP CONSTRAINT approval_requests_id_run_id_unique;
-- +goose StatementEnd
