-- +goose Up
-- +goose StatementBegin
ALTER TABLE approval_requests
DROP CONSTRAINT approval_requests_status_check;

ALTER TABLE approval_requests
DROP CONSTRAINT approval_requests_check;

ALTER TABLE approval_requests
ADD CONSTRAINT approval_requests_status_check
CHECK (status IN ('pending', 'approved', 'rejected', 'canceled'));

ALTER TABLE approval_requests
ADD CONSTRAINT approval_requests_resolution_check
CHECK (
    (status = 'pending' AND resolved_at IS NULL)
    OR (
        status IN ('approved', 'rejected', 'canceled')
        AND resolved_at IS NOT NULL
        AND resolved_at >= requested_at
    )
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE approval_requests
DROP CONSTRAINT approval_requests_resolution_check;

ALTER TABLE approval_requests
DROP CONSTRAINT approval_requests_status_check;

ALTER TABLE approval_requests
ADD CONSTRAINT approval_requests_status_check
CHECK (status IN ('pending', 'approved', 'rejected'));

ALTER TABLE approval_requests
ADD CONSTRAINT approval_requests_check
CHECK (
    (status = 'pending' AND resolved_at IS NULL)
    OR (
        status IN ('approved', 'rejected')
        AND resolved_at IS NOT NULL
        AND resolved_at >= requested_at
    )
);
-- +goose StatementEnd
