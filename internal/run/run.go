package run

import "errors"

var ErrCannotStart = errors.New("only a pending run can start")

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

func (r *Run) Start() error {
	if r.Status != StatusPending {
		return ErrCannotStart
	}

	r.Status = StatusRunning
	return nil
}
