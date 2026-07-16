package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
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
	ErrEventsNotConfigured = errors.New("event log not configured")
)

const workflowStepKey run.StepKey = "workflow"

type Engine struct {
	Client       model.Client
	Events       *event.Log
	Tools        *tool.Registry
	MaxSteps     int
	ModelTimeout time.Duration
	ToolTimeout  time.Duration
	Checkpoints  *StepRunner
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
	if e.Events == nil {
		return model.Response{}, fmt.Errorf("execute workflow: %w", ErrEventsNotConfigured)
	}

	if err := r.Start(); err != nil {
		return model.Response{}, fmt.Errorf("start run: %w", err)
	}
	if err := e.record(r, workflowStepKey, event.TypeWorkflowStarted, event.LifecyclePayload{Status: r.Status}); err != nil {
		return model.Response{}, err
	}

	request.Messages = append([]model.Message(nil), request.Messages...)

	for step := 0; step < e.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			if cancelErr := r.Cancel(); cancelErr != nil {
				return model.Response{}, fmt.Errorf("cancel run before model call: %w", cancelErr)
			}
			if recordErr := e.record(r, workflowStepKey, event.TypeWorkflowCancelled, event.LifecyclePayload{Status: r.Status}); recordErr != nil {
				return model.Response{}, recordErr
			}

			return model.Response{}, fmt.Errorf("execute workflow: %w", err)
		}

		modelStepKey := run.StepKey(fmt.Sprintf("model/%d", step+1))
		if err := e.record(r, modelStepKey, event.TypeModelRequested, event.ModelPayload{}); err != nil {
			return model.Response{}, err
		}

		response, err := e.nextModel(ctx, r.ID, modelStepKey, request)
		if err != nil {
			if recordErr := e.record(r, modelStepKey, event.TypeModelFailed, event.ModelPayload{}); recordErr != nil {
				return model.Response{}, recordErr
			}

			if errors.Is(err, context.Canceled) {
				if cancelErr := r.Cancel(); cancelErr != nil {
					return model.Response{}, fmt.Errorf("cancel run after model cancellation: %w", cancelErr)
				}
				if recordErr := e.record(r, workflowStepKey, event.TypeWorkflowCancelled, event.LifecyclePayload{Status: r.Status}); recordErr != nil {
					return model.Response{}, recordErr
				}

				return model.Response{}, fmt.Errorf("get next model response: %w", err)
			}

			if failErr := r.Fail(); failErr != nil {
				return model.Response{}, fmt.Errorf("fail run after model error: %w", failErr)
			}
			if recordErr := e.record(r, workflowStepKey, event.TypeWorkflowFailed, event.LifecyclePayload{Status: r.Status}); recordErr != nil {
				return model.Response{}, recordErr
			}

			return model.Response{}, fmt.Errorf("get next model response: %w", err)
		}
		if err := e.record(r, modelStepKey, event.TypeModelCompleted, event.ModelPayload{}); err != nil {
			return model.Response{}, err
		}

		request.Messages = append(request.Messages, model.NewAssistantMessage(response))

		if len(response.ToolCalls) == 0 {
			if err := r.Succeed(); err != nil {
				return model.Response{}, fmt.Errorf("succeed run: %w", err)
			}
			if err := e.record(r, workflowStepKey, event.TypeWorkflowCompleted, event.LifecyclePayload{Status: r.Status}); err != nil {
				return model.Response{}, err
			}

			return response, nil
		}

		if e.Tools == nil {
			if err := r.Fail(); err != nil {
				return model.Response{}, fmt.Errorf("fail run without tools: %w", err)
			}
			if err := e.record(r, workflowStepKey, event.TypeWorkflowFailed, event.LifecyclePayload{Status: r.Status}); err != nil {
				return model.Response{}, err
			}

			return model.Response{}, fmt.Errorf("lookup tool: %w", ErrToolsNotConfigured)
		}

		for _, call := range response.ToolCalls {
			toolStepKey := run.StepKey(fmt.Sprintf("tool/%d/%s", step+1, call.ID))
			payload := event.ToolPayload{CallID: call.ID, ToolName: call.Name}
			if err := e.record(r, toolStepKey, event.TypeToolRequested, payload); err != nil {
				return model.Response{}, err
			}

			executable, err := e.Tools.Lookup(call.Name)
			if err != nil {
				if recordErr := e.record(r, toolStepKey, event.TypeToolFailed, payload); recordErr != nil {
					return model.Response{}, recordErr
				}
				if failErr := r.Fail(); failErr != nil {
					return model.Response{}, fmt.Errorf("fail run after tool lookup error: %w", failErr)
				}
				if recordErr := e.record(r, workflowStepKey, event.TypeWorkflowFailed, event.LifecyclePayload{Status: r.Status}); recordErr != nil {
					return model.Response{}, recordErr
				}

				return model.Response{}, fmt.Errorf("lookup tool %q: %w", call.Name, err)
			}

			output, err := e.executeTool(ctx, r.ID, toolStepKey, call, executable)
			if err != nil {
				if recordErr := e.record(r, toolStepKey, event.TypeToolFailed, payload); recordErr != nil {
					return model.Response{}, recordErr
				}
				if errors.Is(err, context.Canceled) {
					if cancelErr := r.Cancel(); cancelErr != nil {
						return model.Response{}, fmt.Errorf("cancel run after tool cancellation: %w", cancelErr)
					}
					if recordErr := e.record(r, workflowStepKey, event.TypeWorkflowCancelled, event.LifecyclePayload{Status: r.Status}); recordErr != nil {
						return model.Response{}, recordErr
					}

					return model.Response{}, fmt.Errorf("execute tool %q: %w", call.Name, err)
				}

				if failErr := r.Fail(); failErr != nil {
					return model.Response{}, fmt.Errorf("fail run after tool error: %w", failErr)
				}
				if recordErr := e.record(r, workflowStepKey, event.TypeWorkflowFailed, event.LifecyclePayload{Status: r.Status}); recordErr != nil {
					return model.Response{}, recordErr
				}

				return model.Response{}, fmt.Errorf("execute tool %q: %w", call.Name, err)
			}
			if err := e.record(r, toolStepKey, event.TypeToolCompleted, payload); err != nil {
				return model.Response{}, err
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
	if err := e.record(r, workflowStepKey, event.TypeWorkflowFailed, event.LifecyclePayload{Status: r.Status}); err != nil {
		return model.Response{}, err
	}

	return model.Response{}, fmt.Errorf("execute workflow: %w", ErrStepLimitExceeded)
}

func (e Engine) nextModel(ctx context.Context, runID run.ID, stepKey run.StepKey, request model.Request) (model.Response, error) {
	if e.Checkpoints == nil {
		modelCtx, cancel := context.WithTimeout(ctx, e.ModelTimeout)
		defer cancel()

		return e.Client.Next(modelCtx, request)
	}

	input, err := json.Marshal(request)
	if err != nil {
		return model.Response{}, fmt.Errorf("marshal model step input: %w", err)
	}

	result, err := e.Checkpoints.Run(ctx, runID, stepKey, input, func(stepCtx context.Context) (json.RawMessage, error) {
		modelCtx, cancel := context.WithTimeout(stepCtx, e.ModelTimeout)
		defer cancel()

		response, err := e.Client.Next(modelCtx, request)
		if err != nil {
			return nil, err
		}

		encoded, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("marshal model response: %w", err)
		}

		return encoded, nil
	})
	if err != nil {
		return model.Response{}, err
	}

	var response model.Response
	if err := json.Unmarshal(result, &response); err != nil {
		return model.Response{}, fmt.Errorf("decode checkpointed model response: %w", err)
	}

	return response, nil
}

func (e Engine) executeTool(ctx context.Context, runID run.ID, stepKey run.StepKey, call tool.Call, executable tool.Tool) (tool.Output, error) {
	if e.Checkpoints == nil {
		toolCtx, cancel := context.WithTimeout(ctx, e.ToolTimeout)
		defer cancel()

		return executable.Execute(toolCtx, call)
	}

	input, err := json.Marshal(call)
	if err != nil {
		return tool.Output{}, fmt.Errorf("marshal tool step input: %w", err)
	}

	result, err := e.Checkpoints.Run(ctx, runID, stepKey, input, func(stepCtx context.Context) (json.RawMessage, error) {
		toolCtx, cancel := context.WithTimeout(stepCtx, e.ToolTimeout)
		defer cancel()

		output, err := executable.Execute(toolCtx, call)
		if err != nil {
			return nil, err
		}

		encoded, err := json.Marshal(output)
		if err != nil {
			return nil, fmt.Errorf("marshal tool output: %w", err)
		}

		return encoded, nil
	})
	if err != nil {
		return tool.Output{}, err
	}

	var output tool.Output
	if err := json.Unmarshal(result, &output); err != nil {
		return tool.Output{}, fmt.Errorf("decode checkpointed tool output: %w", err)
	}

	return output, nil
}

func (e Engine) record(r *run.Run, stepKey run.StepKey, typ event.Type, payload event.Payload) error {
	if _, err := e.Events.Record(r.ID, stepKey, typ, payload); err != nil {
		return fmt.Errorf("record event %q: %w", typ, err)
	}

	return nil
}
