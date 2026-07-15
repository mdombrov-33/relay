package postgres

import (
	"context"
	"testing"
)

func TestOpen(t *testing.T) {
	t.Run("rejects an invalid database URL before creating a pool", func(t *testing.T) {
		pool, err := Open(context.Background(), "://not-a-postgres-url")
		if err == nil {
			pool.Close()
			t.Fatal("Open() error = nil, want an invalid URL error")
		}
	})
}
