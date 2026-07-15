package ports

import (
	"context"
	"time"

	"github.com/lzw7758/go-dot/internal/tracking"
)

type RealtimeCounter interface {
	Apply(ctx context.Context, event CountEvent) (applied bool, err error)
}

type CountEvent struct {
	EventID         string
	ProjectID       uint64
	PointID         uint64
	Platform        string
	Channel         string
	StatTime        time.Time
	ProjectTimezone string
	VisitorHash     uint64
	CountPV         bool
	CountUV         bool
	UVMode          tracking.UVMode
}
