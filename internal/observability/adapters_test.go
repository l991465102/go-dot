package observability

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/lzw7758/go-dot/internal/ingestion"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/stretchr/testify/require"
)

func TestObservedCollectLogCorrelatesRequestAndBatch(t *testing.T) {
	var output bytes.Buffer
	service := ObservedCollectService{
		Next: collectServiceFunc(func(context.Context, project.Project, ingestion.CollectRequest) (ingestion.CollectResult, error) {
			return ingestion.CollectResult{BatchID: "batch-42", Accepted: 1, Results: []ingestion.EventResult{{EventID: "event-42"}}}, nil
		}),
		Logger: NewJSONLogger(&output, "api"),
	}
	ctx := WithRequestID(context.Background(), "request-42")
	_, err := service.Collect(ctx, project.Project{ID: 7}, ingestion.CollectRequest{})
	require.NoError(t, err)
	require.Contains(t, output.String(), `"request_id":"request-42"`)
	require.Contains(t, output.String(), `"batch_id":"batch-42"`)
	require.Contains(t, output.String(), `"event-42"`)
}

func TestObservedStatisticsRefresherMarksOnlySuccessfulRefresh(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	metrics := newMetrics(func() time.Time { return now })
	refreshes := 0
	refresher := ObservedStatisticsRefresher{
		Next: statisticsRefresherFunc(func(context.Context) error {
			refreshes++
			if refreshes == 1 {
				return context.DeadlineExceeded
			}
			return nil
		}),
		Metrics: metrics,
		Now:     func() time.Time { return now },
	}
	require.Error(t, refresher.RefreshCurrentAndPrevious(context.Background()))
	require.Zero(t, metrics.aggregationLastSuccess.Load())
	require.NoError(t, refresher.RefreshCurrentAndPrevious(context.Background()))
	require.Equal(t, now.UnixNano(), metrics.aggregationLastSuccess.Load())
}

type collectServiceFunc func(context.Context, project.Project, ingestion.CollectRequest) (ingestion.CollectResult, error)

func (f collectServiceFunc) Collect(ctx context.Context, p project.Project, req ingestion.CollectRequest) (ingestion.CollectResult, error) {
	return f(ctx, p, req)
}

type statisticsRefresherFunc func(context.Context) error

func (f statisticsRefresherFunc) RefreshCurrentAndPrevious(ctx context.Context) error { return f(ctx) }
