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
