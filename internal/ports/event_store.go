package ports

import (
	"context"
	"encoding/json"
	"time"
)

// EventStore persists immutable, normalized ingestion results. Implementations
// must use token as the storage engine's retry deduplication token.
type EventStore interface {
	SaveValid(ctx context.Context, token string, events []StoredEvent) error
	SaveInvalid(ctx context.Context, token string, events []InvalidEvent) error
}

type StoredEvent struct {
	EventID            string
	ProjectID          uint64
	PointID            uint64
	PointCode          string
	PointVersion       uint32
	DomainCategoryID   uint64
	DomainCategoryCode string
	ModuleCategoryID   uint64
	ModuleCategoryCode string
	SceneCategoryID    uint64
	SceneCategoryCode  string
	VisitorHash        uint64
	Platform           string
	Channel            string
	AppVersion         string
	SessionHash        string
	ClientEventAt      time.Time
	ReceivedAt         time.Time
	StatisticAt        time.Time
	ProjectTimezone    string
	TimeCorrected      bool
	CountPV            bool
	CountUV            bool
	UVMode             string
	Properties         map[string]any
	Version            uint64
}

type InvalidEvent struct {
	ProjectID    uint64
	EventID      string
	PointCode    string
	Platform     string
	Channel      string
	ErrorCode    string
	ErrorMessage string
	Original     json.RawMessage
	ReceivedAt   time.Time
	Version      uint64
}
