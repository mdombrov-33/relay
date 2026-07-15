package run

import "errors"

var (
	ErrCannotStart   = errors.New("only a pending run can start")
	ErrCannotSucceed = errors.New("only a running run can succeed")
	ErrCannotFail    = errors.New("only a running run can fail")
)

type ID string

type Run struct {
	ID     ID
	Status Status
}

func New(id ID) Run {
	return Run{
		ID:     id,
		Status: StatusPending,
	}
}

func (r *Run) Succeed() error {
	if r.Status != StatusRunning {
		return ErrCannotSucceed
	}

	r.Status = StatusSucceeded
	return nil
}

func (r *Run) Fail() error {
	if r.Status != StatusRunning {
		return ErrCannotFail
	}

	r.Status = StatusFailed
	return nil
}

func (r *Run) Start() error {

	if r.Status != StatusPending {
		return ErrCannotStart
	}

	r.Status = StatusRunning
	return nil
}
