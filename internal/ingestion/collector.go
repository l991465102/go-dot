package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

var (
	ErrInvalidBatch = errors.New("invalid collection batch")
	ulidPattern     = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)
	uuidV7Pattern   = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

type SnapshotProvider interface {
	Get(context.Context, uint64, string) (tracking.Snapshot, error)
}

type Config struct {
	MaxEvents     int
	PastWindow    time.Duration
	FutureWindow  time.Duration
	Now           func() time.Time
	EventStream   string
	InvalidStream string
}

type Event struct {
	EventID     string         `json:"event_id"`
	PointCode   string         `json:"point_code"`
	EventTime   int64          `json:"event_time"`
	UserID      string         `json:"user_id,omitempty"`
	AnonymousID string         `json:"anonymous_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	Platform    string         `json:"platform"`
	AppVersion  string         `json:"app_version,omitempty"`
	Channel     string         `json:"channel,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

type CollectRequest struct {
	// ProjectID is accepted only to make accidental client fields explicit; it is never trusted.
	ProjectID uint64  `json:"project_id,omitempty"`
	Events    []Event `json:"events"`
}

type EventResult struct {
	Index   int    `json:"index"`
	EventID string `json:"event_id,omitempty"`
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type CollectResult struct {
	BatchID  string        `json:"batch_id"`
	Accepted int           `json:"accepted"`
	Rejected int           `json:"rejected"`
	Results  []EventResult `json:"results"`
}

type NormalizedEvent struct {
	Event
	ProjectID           uint64                 `json:"project_id"`
	SnapshotID          uint64                 `json:"snapshot_id"`
	PointID             uint64                 `json:"point_id"`
	PointVersion        uint                   `json:"point_version"`
	CategoryID          uint64                 `json:"category_id"`
	CategoryCode        string                 `json:"category_code"`
	CategoryPath        []tracking.CategoryRef `json:"category_path"`
	UVMode              tracking.UVMode        `json:"uv_mode"`
	CountPV             bool                   `json:"count_pv"`
	CountUV             bool                   `json:"count_uv"`
	ProjectTimezone     string                 `json:"project_timezone"`
	ReceivedAt          time.Time              `json:"received_at"`
	ClientTime          time.Time              `json:"client_time"`
	StatTime            time.Time              `json:"stat_time"`
	TimeCorrected       bool                   `json:"time_corrected"`
	AnalyticsProperties []tracking.Property    `json:"analytics_properties,omitempty"`
}

type invalidEvent struct {
	ProjectID    uint64          `json:"project_id"`
	BatchID      string          `json:"batch_id"`
	ReceivedAt   time.Time       `json:"received_at"`
	ErrorCode    string          `json:"error_code"`
	ErrorMessage string          `json:"error_message"`
	Original     json.RawMessage `json:"original"`
}

type batchPayload[T any] struct {
	BatchID    string    `json:"batch_id"`
	ProjectID  uint64    `json:"project_id"`
	ReceivedAt time.Time `json:"received_at"`
	Events     []T       `json:"events"`
}

type Collector struct {
	snapshots SnapshotProvider
	queue     ports.MessageQueue
	cfg       Config
}

func NewCollector(snapshots SnapshotProvider, queue ports.MessageQueue, cfg Config) *Collector {
	if cfg.MaxEvents == 0 {
		cfg.MaxEvents = 100
	}
	if cfg.PastWindow == 0 {
		cfg.PastWindow = 7 * 24 * time.Hour
	}
	if cfg.FutureWindow == 0 {
		cfg.FutureWindow = 10 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.EventStream == "" {
		cfg.EventStream = EventStream
	}
	if cfg.InvalidStream == "" {
		cfg.InvalidStream = InvalidEventStream
	}
	return &Collector{snapshots: snapshots, queue: queue, cfg: cfg}
}

func (c *Collector) Collect(ctx context.Context, p project.Project, req CollectRequest) (CollectResult, error) {
	if p.ID == 0 || !p.Active {
		return CollectResult{}, fmt.Errorf("project unavailable")
	}
	if len(req.Events) < 1 || len(req.Events) > c.cfg.MaxEvents {
		return CollectResult{}, ErrInvalidBatch
	}
	now := c.cfg.Now().UTC()
	batchID, err := stableBatchID(p.ID, req.Events)
	if err != nil {
		return CollectResult{}, fmt.Errorf("build batch id: %w", err)
	}
	result := CollectResult{BatchID: batchID, Results: make([]EventResult, 0, len(req.Events))}
	accepted := make([]NormalizedEvent, 0, len(req.Events))
	invalid := make([]invalidEvent, 0)
	for i, event := range req.Events {
		normalized, validationErr := c.normalize(ctx, p, now, event)
		if validationErr == nil {
			accepted = append(accepted, normalized)
			result.Accepted++
			result.Results = append(result.Results, EventResult{Index: i, EventID: event.EventID, Status: "accepted"})
			continue
		}
		if _, ok := tracking.ErrorCode(validationErr); !ok {
			return CollectResult{}, validationErr
		}
		code, message := eventError(validationErr)
		raw, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return CollectResult{}, fmt.Errorf("encode invalid event: %w", marshalErr)
		}
		invalid = append(invalid, invalidEvent{ProjectID: p.ID, BatchID: batchID, ReceivedAt: now, ErrorCode: code, ErrorMessage: message, Original: raw})
		result.Rejected++
		result.Results = append(result.Results, EventResult{Index: i, EventID: event.EventID, Status: "rejected", Code: code, Message: message})
	}
	if len(accepted) > 0 {
		payload, err := json.Marshal(batchPayload[NormalizedEvent]{BatchID: batchID, ProjectID: p.ID, ReceivedAt: now, Events: accepted})
		if err != nil {
			return CollectResult{}, fmt.Errorf("encode accepted batch: %w", err)
		}
		if _, err := c.queue.PublishBatch(ctx, c.cfg.EventStream, ports.BatchEnvelope{BatchID: batchID, Payload: payload, CreatedAt: now}); err != nil {
			return CollectResult{}, fmt.Errorf("publish accepted batch: %w", err)
		}
	}
	if len(invalid) > 0 {
		payload, err := json.Marshal(batchPayload[invalidEvent]{BatchID: batchID, ProjectID: p.ID, ReceivedAt: now, Events: invalid})
		if err != nil {
			return CollectResult{}, fmt.Errorf("encode invalid batch: %w", err)
		}
		if _, err := c.queue.PublishBatch(ctx, c.cfg.InvalidStream, ports.BatchEnvelope{BatchID: batchID, Payload: payload, CreatedAt: now}); err != nil {
			return CollectResult{}, fmt.Errorf("publish invalid batch: %w", err)
		}
	}
	return result, nil
}

func (c *Collector) normalize(ctx context.Context, p project.Project, now time.Time, event Event) (NormalizedEvent, error) {
	event.EventID = strings.TrimSpace(event.EventID)
	event.PointCode = strings.TrimSpace(event.PointCode)
	event.Platform = strings.ToLower(strings.TrimSpace(event.Platform))
	if !validEventID(event.EventID) {
		return NormalizedEvent{}, tracking.NewCodedError("EVENT_ID_INVALID", "event_id must be UUIDv7 or ULID")
	}
	if event.PointCode == "" {
		return NormalizedEvent{}, tracking.NewCodedError("POINT_CODE_REQUIRED", "point_code is required")
	}
	if strings.TrimSpace(event.UserID) == "" && strings.TrimSpace(event.AnonymousID) == "" {
		return NormalizedEvent{}, tracking.NewCodedError("VISITOR_REQUIRED", "user_id or anonymous_id is required")
	}
	if event.EventTime <= 0 {
		return NormalizedEvent{}, tracking.NewCodedError("EVENT_TIME_INVALID", "event_time is required")
	}
	snapshot, err := c.snapshots.Get(ctx, p.ID, event.PointCode)
	if err != nil {
		if errors.Is(err, tracking.ErrPointNotPublished) || errors.Is(err, tracking.ErrPointNotFound) {
			return NormalizedEvent{}, tracking.NewCodedError("POINT_NOT_PUBLISHED", "point is not published")
		}
		return NormalizedEvent{}, fmt.Errorf("load point snapshot: %w", err)
	}
	if snapshot.ID == 0 || snapshot.ProjectID != p.ID {
		return NormalizedEvent{}, fmt.Errorf("invalid snapshot metadata")
	}
	if !contains(snapshot.SupportedPlatforms, event.Platform) {
		return NormalizedEvent{}, tracking.NewCodedError("PLATFORM_NOT_SUPPORTED", "platform is not supported")
	}
	if err := snapshot.ValidateProperties(event.Properties); err != nil {
		return NormalizedEvent{}, err
	}
	clientTime := time.UnixMilli(event.EventTime).UTC()
	statTime := clientTime
	corrected := clientTime.Before(now.Add(-c.cfg.PastWindow)) || clientTime.After(now.Add(c.cfg.FutureWindow))
	if corrected || snapshot.TimeBasis == tracking.Server {
		statTime = now
	}
	timezone := p.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return NormalizedEvent{}, fmt.Errorf("invalid project timezone")
	}
	analyticsProperties := make([]tracking.Property, 0, len(snapshot.Properties))
	for _, property := range snapshot.Properties {
		if property.Analytics.Mode != "" && property.Analytics.Mode != tracking.AnalyticsNone {
			analyticsProperties = append(analyticsProperties, property)
		}
	}
	return NormalizedEvent{Event: event, ProjectID: p.ID, SnapshotID: snapshot.ID, PointID: snapshot.PointID, PointVersion: snapshot.Version, CategoryID: snapshot.CategoryID, CategoryCode: snapshot.CategoryCode, CategoryPath: snapshot.CategoryPath, UVMode: snapshot.UVMode, CountPV: snapshot.CountPV, CountUV: snapshot.CountUV, ProjectTimezone: timezone, ReceivedAt: now, ClientTime: clientTime, StatTime: statTime, TimeCorrected: corrected, AnalyticsProperties: analyticsProperties}, nil
}

func stableBatchID(projectID uint64, events []Event) (string, error) {
	raw, err := json.Marshal(struct {
		ProjectID uint64  `json:"project_id"`
		Events    []Event `json:"events"`
	}{projectID, events})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "b_" + hex.EncodeToString(sum[:16]), nil
}
func validEventID(id string) bool {
	return uuidV7Pattern.MatchString(id) || ulidPattern.MatchString(strings.ToUpper(id))
}
func contains(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}
func eventError(err error) (string, string) {
	if code, ok := tracking.ErrorCode(err); ok {
		return code, err.Error()
	}
	return "VALIDATION_FAILED", "event validation failed"
}
