-- +goose Up
-- +goose StatementBegin
CREATE TABLE runs (
    id TEXT PRIMARY KEY CHECK (id <> ''),
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'canceled')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK (updated_at >= created_at)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE events (
    sequence BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    id TEXT NOT NULL UNIQUE CHECK (id <> ''),
    run_id TEXT NOT NULL REFERENCES runs (id) ON DELETE RESTRICT,
    step_key TEXT NOT NULL CHECK (step_key <> ''),
    type TEXT NOT NULL CHECK (type <> ''),
    occurred_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    CHECK (octet_length(payload::text) <= 8192)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX events_run_sequence_idx ON events (run_id, sequence);
-- +goose StatementEnd


-- +goose StatementBegin
CREATE FUNCTION reject_event_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'events are append-only';
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER events_reject_mutation
BEFORE UPDATE OR DELETE ON events
FOR EACH ROW
EXECUTE FUNCTION reject_event_mutation();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE events;
DROP TABLE runs;
DROP FUNCTION reject_event_mutation();
-- +goose StatementEnd