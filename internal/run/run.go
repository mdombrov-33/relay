package run

import "errors"

var (
	ErrCannotStart   = errors.New("only a pending run can start")
	ErrCannotWait    = errors.New("only a running run can wait")
	ErrCannotResume  = errors.New("only a waiting run can resume")
	ErrCannotSucceed = errors.New("only a running run can succeed")
	ErrCannotFail    = errors.New("only a running run can fail")
	ErrCannotCancel  = errors.New("only a pending, running, or waiting run can cancel")
)

type ID string

// StepKey identifies one logical workflow operation within a run.
type StepKey string

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

func (r *Run) Wait() error {
	if r.Status != StatusRunning {
		return ErrCannotWait
	}

	r.Status = StatusWaiting
	return nil
}

func (r *Run) Resume() error {
	if r.Status != StatusWaiting {
		return ErrCannotResume
	}

	r.Status = StatusRunning
	return nil
}

func (r *Run) Cancel() error {
	switch r.Status {
	case StatusPending, StatusRunning, StatusWaiting:
		r.Status = StatusCanceled
		return nil
	default:
		return ErrCannotCancel
	}
}
