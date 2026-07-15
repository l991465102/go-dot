package cmd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunDispatchesMigrateCommand(t *testing.T) {
	original := migrateCommand
	t.Cleanup(func() { migrateCommand = original })
	called := false
	migrateCommand = func(context.Context) error {
		called = true
		return nil
	}

	require.NoError(t, Run(context.Background(), []string{"migrate"}))
	require.True(t, called)
}
