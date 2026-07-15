package config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadRejectsMissingSecrets(t *testing.T) {
	t.Setenv("GO_DOT_JWT_SECRET", "")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "")
	_, err := Load(context.Background())
	require.ErrorContains(t, err, "GO_DOT_JWT_SECRET")
}

func TestLoadProvidesQueueDefaults(t *testing.T) {
	t.Setenv("GO_DOT_JWT_SECRET", "test-secret")
	t.Setenv("GO_DOT_VISITOR_MASTER_KEY", "test-visitor-key")
	t.Setenv("GO_DOT_QUEUE_EVENT_STREAM", "")
	t.Setenv("GO_DOT_QUEUE_INVALID_STREAM", "")
	t.Setenv("GO_DOT_QUEUE_DEAD_STREAM", "")
	t.Setenv("GO_DOT_QUEUE_CONSUMER_GROUP", "")

	cfg, err := Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tracking:events", cfg.Queue.EventStream)
	require.Equal(t, "tracking:invalid", cfg.Queue.InvalidStream)
	require.Equal(t, "tracking:dead", cfg.Queue.DeadStream)
	require.Equal(t, "tracking-workers", cfg.Queue.ConsumerGroup)
}
