package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/run"
	"github.com/mdombrov-33/relay/internal/tool"
)

var (
	ErrInvalidMaxSteps     = errors.New("max steps must be positive")
	ErrStepLimitExceeded   = errors.New("workflow step limit exceeded")
	ErrToolsNotConfigured  = errors.New("tools not configured")
	ErrInvalidModelTimeout = errors.New("model timeout must be positive")
	ErrInvalidToolTimeout  = errors.New("tool timeout must be positive")
)

type Engine struct {
	Client       model.Client
	Tools        *tool.Registry
	MaxSteps     int
	ModelTimeout time.Duration
	ToolTimeout  time.Duration
}

func (e Engine) Execute(ctx context.Context, r *run.Run, request model.Request) (model.Response, error) {
	if e.MaxSteps <= 0 {
		return model.Response{}, fmt.Errorf("execute workflow: %w", ErrInvalidMaxSteps)
	}

	if e.ModelTimeout <= 0 {
		return model.Response{}, fmt.Errorf("execute workflow: %w", ErrInvalidModelTimeout)
	}

	if e.ToolTimeout <= 0 {
		return model.Response{}, fmt.Errorf("execute workflow: %w", ErrInvalidToolTimeout)
	}

	if err := r.Start(); err != nil {
		return model.Response{}, fmt.Errorf("start run: %w", err)
	}

	request.Messages = append([]model.Message(nil), request.Messages...)

	for step := 0; step < e.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			if cancelErr := r.Cancel(); cancelErr != nil {
				return model.Response{}, fmt.Errorf("cancel run before model call: %w", cancelErr)
			}

			return model.Response{}, fmt.Errorf("execute workflow: %w", err)
		}

		modelCtx, cancel := context.WithTimeout(ctx, e.ModelTimeout)
		response, err := e.Client.Next(modelCtx, request)
		cancel()

		if err != nil {
			if errors.Is(err, context.Canceled) {
				if cancelErr := r.Cancel(); cancelErr != nil {
					return model.Response{}, fmt.Errorf("cancel run after model cancellation: %w", cancelErr)
				}

				return model.Response{}, fmt.Errorf("get next model response: %w", err)
			}

			if failErr := r.Fail(); failErr != nil {
				return model.Response{}, fmt.Errorf("fail run after model error: %w", failErr)
			}

			return model.Response{}, fmt.Errorf("get next model response: %w", err)
		}

		request.Messages = append(request.Messages, model.NewAssistantMessage(response))

		if len(response.ToolCalls) == 0 {
			if err := r.Succeed(); err != nil {
				return model.Response{}, fmt.Errorf("succeed run: %w", err)
			}

			return response, nil
		}

		if e.Tools == nil {
			if err := r.Fail(); err != nil {
				return model.Response{}, fmt.Errorf("fail run without tools: %w", err)
			}

			return model.Response{}, fmt.Errorf("lookup tool: %w", ErrToolsNotConfigured)
		}

		for _, call := range response.ToolCalls {
			executable, err := e.Tools.Lookup(call.Name)
			if err != nil {
				if failErr := r.Fail(); failErr != nil {
					return model.Response{}, fmt.Errorf("fail run after tool lookup error: %w", failErr)
				}

				return model.Response{}, fmt.Errorf("lookup tool %q: %w", call.Name, err)
			}

			toolCtx, cancel := context.WithTimeout(ctx, e.ToolTimeout)
			output, err := executable.Execute(toolCtx, call)
			cancel()

			if err != nil {
				if errors.Is(err, context.Canceled) {
					if cancelErr := r.Cancel(); cancelErr != nil {
						return model.Response{}, fmt.Errorf("cancel run after tool cancellation: %w", cancelErr)
					}

					return model.Response{}, fmt.Errorf("execute tool %q: %w", call.Name, err)
				}

				if failErr := r.Fail(); failErr != nil {
					return model.Response{}, fmt.Errorf("fail run after tool error: %w", failErr)
				}

				return model.Response{}, fmt.Errorf("execute tool %q: %w", call.Name, err)
			}

			request.Messages = append(request.Messages, model.NewToolMessage(tool.Result{
				CallID:   call.ID,
				ToolName: call.Name,
				Content:  output.Content,
			}))
		}
	}

	if err := r.Fail(); err != nil {
		return model.Response{}, fmt.Errorf("fail run after step limit: %w", err)
	}

	return model.Response{}, fmt.Errorf("execute workflow: %w", ErrStepLimitExceeded)
}
