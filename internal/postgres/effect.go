package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mdombrov-33/relay/internal/run"
)

var (
	ErrInvalidEffect          = errors.New("effect identity must be complete")
	ErrInvalidEffectResult    = errors.New("effect result must be valid JSON")
	ErrEffectIdentityMismatch = errors.New("idempotency key belongs to a different effect")
)

type EffectType string

type Effect struct {
	IdempotencyKey string
	RunID          run.ID
	StepKey        run.StepKey
	Type           EffectType
	Result         json.RawMessage
	RecordedAt     time.Time
}

func (s *Store) RecordEffect(ctx context.Context, effect Effect) (Effect, bool, error) {
	if effect.IdempotencyKey == "" || effect.RunID == "" || effect.StepKey == "" || effect.Type == "" || effect.RecordedAt.IsZero() {
		return Effect{}, false, ErrInvalidEffect
	}
	if !json.Valid(effect.Result) {
		return Effect{}, false, ErrInvalidEffectResult
	}

	recorded, err := scanEffect(s.pool.QueryRow(
		ctx,
		`INSERT INTO effects (idempotency_key, run_id, step_key, effect_type, result, recorded_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		 ON CONFLICT (idempotency_key) DO NOTHING
		 RETURNING idempotency_key, run_id, step_key, effect_type, result, recorded_at`,
		effect.IdempotencyKey,
		effect.RunID,
		effect.StepKey,
		effect.Type,
		string(effect.Result),
		effect.RecordedAt,
	))
	if err == nil {
		return recorded, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Effect{}, false, fmt.Errorf("record effect: %w", err)
	}

	recorded, err = s.findEffect(ctx, effect.IdempotencyKey)
	if err != nil {
		return Effect{}, false, err
	}
	if recorded.RunID != effect.RunID || recorded.StepKey != effect.StepKey || recorded.Type != effect.Type {
		return Effect{}, false, ErrEffectIdentityMismatch
	}

	return recorded, false, nil
}

func (s *Store) findEffect(ctx context.Context, idempotencyKey string) (Effect, error) {
	effect, err := scanEffect(s.pool.QueryRow(
		ctx,
		`SELECT idempotency_key, run_id, step_key, effect_type, result, recorded_at
		 FROM effects
		 WHERE idempotency_key = $1`,
		idempotencyKey,
	))
	if err != nil {
		return Effect{}, fmt.Errorf("find effect: %w", err)
	}

	return effect, nil
}

func scanEffect(row pgx.Row) (Effect, error) {
	var effect Effect
	if err := row.Scan(
		&effect.IdempotencyKey,
		&effect.RunID,
		&effect.StepKey,
		&effect.Type,
		&effect.Result,
		&effect.RecordedAt,
	); err != nil {
		return Effect{}, fmt.Errorf("scan effect: %w", err)
	}
	if !json.Valid(effect.Result) {
		return Effect{}, ErrInvalidEffectResult
	}

	effect.Result = bytes.Clone(effect.Result)
	return effect, nil
}
