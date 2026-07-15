package statistics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashVisitorPrefersUserAndSeparatesProjects(t *testing.T) {
	a := HashVisitor([]byte("master"), []byte("project-a"), "42", "device")
	b := HashVisitor([]byte("master"), []byte("project-b"), "42", "device")

	require.NotEqual(t, a, b)
	require.Equal(t, a, HashVisitor([]byte("master"), []byte("project-a"), "42", "other-device"))
	require.NotEqual(t, a, HashVisitor([]byte("master"), []byte("project-a"), "", "42"))
}

func TestHashVisitorMatchesStableHMACVector(t *testing.T) {
	require.Equal(t, uint64(0), HashVisitor(nil, nil, "", ""))
	require.Equal(t, uint64(0xe18022ad18a74025), HashVisitor([]byte("master"), []byte("project-a"), "42", "device"))
}
