package funnel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/project"
	"github.com/stretchr/testify/require"
)

type authStub struct{ err error }

func (a authStub) RequireRole(context.Context, uint64, uint64, ...project.Role) error { return a.err }

type storeStub struct {
	seen   Query
	values []uint64
}

func (s *storeStub) QueryFunnel(_ context.Context, q Query) ([]uint64, error) {
	s.seen = q
	return s.values, nil
}

// TestRunFunnel 验证口径校验、权限、窗口未结束标识和转化率。
func TestRunFunnel(t *testing.T) {
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	q := Query{ProjectID: 1, Start: now.Add(-48 * time.Hour), End: now.Add(-time.Hour), WindowHours: 24, PointCodes: []string{"view_page", "submit_order", "pay_done"}}
	store := &storeStub{values: []uint64{10, 5, 0}}
	svc := NewService(authStub{}, store, func() time.Time { return now })
	out, err := svc.Run(context.Background(), 9, q)
	require.NoError(t, err)
	require.True(t, out.Incomplete)
	require.Equal(t, now, store.seen.EvaluatedUntil)
	require.Equal(t, 0.5, *out.Steps[1].Conversion)
	require.Equal(t, 0.0, *out.Steps[2].Conversion)
	q.PointCodes[1] = q.PointCodes[0]
	_, err = svc.Run(context.Background(), 9, q)
	require.ErrorIs(t, err, ErrInvalid)
	q.PointCodes[1] = "submit_order"
	q.Start = now.Add(-22 * 24 * time.Hour)
	_, err = svc.Run(context.Background(), 9, q)
	require.ErrorIs(t, err, ErrInvalid)
	denied := NewService(authStub{err: project.ErrForbidden}, store, func() time.Time { return now })
	_, err = denied.Run(context.Background(), 9, q)
	require.True(t, errors.Is(err, project.ErrForbidden))
}
