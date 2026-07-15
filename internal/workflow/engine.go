package workflow

import (
	"context"
	"fmt"

	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/run"
)

type Engine struct {
	Client model.Client
}

func (e Engine) Execute(ctx context.Context, r *run.Run, request model.Request) (model.Response, error) {
	if err := r.Start(); err != nil {
		return model.Response{}, fmt.Errorf("start run: %w", err)
	}

	response, modelErr := e.Client.Next(ctx, request)

	if modelErr != nil {
		if err := r.Fail(); err != nil {
			return model.Response{}, fmt.Errorf("fail run after model error: %w", err)
		}

		return model.Response{}, fmt.Errorf("get next model response: %w", modelErr)

	}

	if err := r.Succeed(); err != nil {
		return model.Response{}, fmt.Errorf("succeed run: %w", err)
	}

	return response, nil
}
