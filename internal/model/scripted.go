package model

import (
	"context"
	"errors"
)

var ErrNoResponses = errors.New("scripted client has no remaining responses")

type ScriptedClient struct {
	responses []Response
	index     int
}

var _ Client = (*ScriptedClient)(nil)

func NewScriptedClient(responses ...Response) *ScriptedClient {
	return &ScriptedClient{
		responses: responses,
	}
}

func (sc *ScriptedClient) Next(ctx context.Context, _ Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}

	if sc.index >= len(sc.responses) {
		return Response{}, ErrNoResponses
	}

	response := sc.responses[sc.index]
	sc.index++

	return response, nil
}
