package observability

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactFieldsRecursivelyRemovesSecretsAndIdentity(t *testing.T) {
	input := map[string]any{
		"request_id": "request-1", "batch_id": "batch-1", "event_id": "event-1", "project_id": uint64(7),
		"password": "SECRET_PASSWORD", "access_token": "SECRET_ACCESS", "refresh_token": "SECRET_REFRESH", "write_key": "SECRET_WRITE_KEY",
		"user_id": "SECRET_USER", "anonymous_id": "SECRET_ANONYMOUS", "properties": map[string]any{"email": "SECRET_PROPERTY"},
		"nested": map[string]any{"token": "SECRET_TOKEN", "safe": "visible"},
	}
	encoded, err := json.Marshal(RedactFields(input))
	require.NoError(t, err)
	text := string(encoded)
	for _, secret := range []string{"SECRET_PASSWORD", "SECRET_ACCESS", "SECRET_REFRESH", "SECRET_WRITE_KEY", "SECRET_USER", "SECRET_ANONYMOUS", "SECRET_PROPERTY", "SECRET_TOKEN"} {
		require.NotContains(t, text, secret)
	}
	for _, visible := range []string{"request-1", "batch-1", "event-1", "visible"} {
		require.Contains(t, text, visible)
	}
}
