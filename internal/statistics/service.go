package statistics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

var (
	ErrInvalidQuery  = errors.New("invalid statistics query")
	ErrInputTooLarge = errors.New("input too large")
	ErrRateLimited   = errors.New("rate limited")
)

type Authorizer interface {
	RequireRole(context.Context, uint64, uint64, ...project.Role) error
}
type ProjectTimezoneSource interface {
	ProjectTimezone(context.Context, uint64, uint64) (string, error)
}
type RealtimeStore interface {
	QueryRealtime(context.Context, ports.StatisticsFilter) (ports.Summary, error)
}
type HistoryStore interface {
	QuerySummary(context.Context, ports.StatisticsFilter) (ports.Summary, error)
	QueryTrend(context.Context, ports.StatisticsFilter) ([]ports.TrendPoint, error)
}
type PointSource interface {
	Get(context.Context, uint64, string) (tracking.Snapshot, error)
}
type DiagnosticsStore interface {
	ListRecent(context.Context, DiagnosticFilter) (EventPage, error)
	ListInvalid(context.Context, DiagnosticFilter) (InvalidEventPage, error)
	ListDeadLetters(context.Context, DeadLetterFilter) (DeadLetterPage, error)
}
type EventDiagnostics interface {
	ListRecent(context.Context, DiagnosticFilter) (EventPage, error)
	ListInvalid(context.Context, DiagnosticFilter) (InvalidEventPage, error)
}
type DeadLetterDiagnostics interface {
	ListDeadLetters(context.Context, DeadLetterFilter) (DeadLetterPage, error)
}
type CompositeDiagnostics struct {
	Events      EventDiagnostics
	DeadLetters DeadLetterDiagnostics
}

func (c CompositeDiagnostics) ListRecent(ctx context.Context, f DiagnosticFilter) (EventPage, error) {
	return c.Events.ListRecent(ctx, f)
}
func (c CompositeDiagnostics) ListInvalid(ctx context.Context, f DiagnosticFilter) (InvalidEventPage, error) {
	return c.Events.ListInvalid(ctx, f)
}
func (c CompositeDiagnostics) ListDeadLetters(ctx context.Context, f DeadLetterFilter) (DeadLetterPage, error) {
	return c.DeadLetters.ListDeadLetters(ctx, f)
}

type Query struct {
	ProjectID                                                 uint64
	Start, End                                                time.Time
	Granularity                                               string
	Timezone                                                  string
	DomainCategoryID, ModuleCategoryID, SceneCategoryID       uint64
	DomainCategoryCode, ModuleCategoryCode, SceneCategoryCode string
	PointCodes                                                []string
	Platform, Channel                                         string
}
type DiagnosticQuery struct {
	ProjectID                            uint64
	Start, End                           time.Time
	PointCodes                           []string
	Platform, Channel, ErrorCode, Cursor string
	Limit                                int
}
type DiagnosticFilter struct {
	ProjectID                            uint64
	Start, End                           time.Time
	PointCodes                           []string
	Platform, Channel, ErrorCode, Cursor string
	Limit                                int
}
type DeadLetterFilter struct {
	ProjectID uint64
	Cursor    string
	Limit     int
}
type Event struct {
	EventID       string         `json:"event_id"`
	PointCode     string         `json:"point_code"`
	Platform      string         `json:"platform"`
	Channel       string         `json:"channel,omitempty"`
	AppVersion    string         `json:"app_version,omitempty"`
	ClientEventAt time.Time      `json:"client_event_at"`
	ReceivedAt    time.Time      `json:"received_at"`
	StatisticAt   time.Time      `json:"statistic_at"`
	Properties    map[string]any `json:"properties,omitempty"`
}
type InvalidEvent struct {
	EventID      string    `json:"event_id,omitempty"`
	PointCode    string    `json:"point_code,omitempty"`
	Platform     string    `json:"platform,omitempty"`
	Channel      string    `json:"channel,omitempty"`
	ErrorCode    string    `json:"error_code"`
	ErrorMessage string    `json:"error_message"`
	ReceivedAt   time.Time `json:"received_at"`
}
type DeadLetter struct {
	ID         string    `json:"id"`
	BatchID    string    `json:"batch_id,omitempty"`
	Deliveries int       `json:"deliveries"`
	FirstSeen  time.Time `json:"first_seen"`
	LastError  string    `json:"last_error,omitempty"`
}
type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
}
type InvalidEventPage struct {
	Items      []InvalidEvent `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}
type DeadLetterPage struct {
	Items      []DeadLetter `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type Service struct {
	auth        Authorizer
	realtime    RealtimeStore
	history     HistoryStore
	points      PointSource
	diagnostics DiagnosticsStore
	now         func() time.Time
}

func NewService(auth Authorizer, realtime RealtimeStore, history HistoryStore, points PointSource, diagnostics DiagnosticsStore, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{auth: auth, realtime: realtime, history: history, points: points, diagnostics: diagnostics, now: now}
}

func (s *Service) Realtime(ctx context.Context, actor uint64, q Query) (ports.Summary, error) {
	if len(uniqueNonempty(q.PointCodes)) == 0 {
		return ports.Summary{}, fmt.Errorf("%w: realtime requires point_code to determine UV mode", ErrInvalidQuery)
	}
	f, err := s.filter(ctx, actor, q, 72*time.Hour)
	if err != nil {
		return ports.Summary{}, err
	}
	if s.realtime == nil {
		return ports.Summary{}, errors.New("realtime statistics unavailable")
	}
	if hasCategoryFilter(q) && len(f.PointCodes) == 0 {
		return ports.Summary{}, fmt.Errorf("%w: realtime category filters require point_code", ErrInvalidQuery)
	}
	if len(f.PointCodes) > 0 && len(f.PointIDs) == 0 {
		return ports.Summary{Approximate: !f.ExactUV}, nil
	}
	return s.realtime.QueryRealtime(ctx, f)
}
func (s *Service) Summary(ctx context.Context, actor uint64, q Query) (ports.Summary, error) {
	f, err := s.filter(ctx, actor, q, 90*24*time.Hour)
	if err != nil {
		return ports.Summary{}, err
	}
	if s.history == nil {
		return ports.Summary{}, errors.New("historical statistics unavailable")
	}
	return s.history.QuerySummary(ctx, f)
}
func (s *Service) Trend(ctx context.Context, actor uint64, q Query) ([]ports.TrendPoint, error) {
	f, err := s.filter(ctx, actor, q, 90*24*time.Hour)
	if err != nil {
		return nil, err
	}
	if s.history == nil {
		return nil, errors.New("historical statistics unavailable")
	}
	return s.history.QueryTrend(ctx, f)
}

func (s *Service) filter(ctx context.Context, actor uint64, q Query, maxRange time.Duration) (ports.StatisticsFilter, error) {
	if q.ProjectID == 0 || q.Start.IsZero() || !q.End.After(q.Start) || q.End.Sub(q.Start) > maxRange {
		return ports.StatisticsFilter{}, fmt.Errorf("%w: project and a range no longer than %s are required", ErrInvalidQuery, maxRange)
	}
	if q.End.After(s.now().UTC().Add(10 * time.Minute)) {
		return ports.StatisticsFilter{}, fmt.Errorf("%w: end is in the future", ErrInvalidQuery)
	}
	if q.Granularity == "" {
		q.Granularity = "hour"
	}
	if q.Granularity != "" && q.Granularity != "hour" && q.Granularity != "day" {
		return ports.StatisticsFilter{}, fmt.Errorf("%w: granularity", ErrInvalidQuery)
	}
	if q.Timezone == "" {
		q.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(q.Timezone); err != nil {
		return ports.StatisticsFilter{}, fmt.Errorf("%w: timezone", ErrInvalidQuery)
	}
	if err := s.auth.RequireRole(ctx, actor, q.ProjectID, project.Admin, project.Viewer); err != nil {
		return ports.StatisticsFilter{}, err
	}
	timezoneSource, ok := s.auth.(ProjectTimezoneSource)
	if !ok {
		return ports.StatisticsFilter{}, errors.New("trusted project timezone source unavailable")
	}
	storageTimezone, err := timezoneSource.ProjectTimezone(ctx, actor, q.ProjectID)
	if err != nil {
		return ports.StatisticsFilter{}, err
	}
	storageLocation, err := time.LoadLocation(storageTimezone)
	if err != nil {
		return ports.StatisticsFilter{}, errors.New("project timezone is invalid")
	}
	if !alignedBoundary(q.Start, q.Granularity, storageLocation) || !alignedBoundary(q.End, q.Granularity, storageLocation) {
		return ports.StatisticsFilter{}, fmt.Errorf("%w: range must align to %s boundaries in project timezone", ErrInvalidQuery, q.Granularity)
	}
	f := ports.StatisticsFilter{ProjectID: q.ProjectID, Start: q.Start.UTC(), End: q.End.UTC(), Granularity: q.Granularity, Timezone: q.Timezone,
		StorageTimezone:  storageTimezone,
		DomainCategoryID: q.DomainCategoryID, ModuleCategoryID: q.ModuleCategoryID, SceneCategoryID: q.SceneCategoryID,
		DomainCategoryCode: q.DomainCategoryCode, ModuleCategoryCode: q.ModuleCategoryCode, SceneCategoryCode: q.SceneCategoryCode,
		PointCodes: uniqueNonempty(q.PointCodes), Platform: q.Platform, Channel: q.Channel}
	if len(f.PointCodes) > 0 && s.points != nil {
		var exact *bool
		for _, code := range f.PointCodes {
			p, err := s.points.Get(ctx, q.ProjectID, code)
			if err != nil {
				return ports.StatisticsFilter{}, err
			}
			value := p.UVMode == tracking.Exact
			if snapshotMatchesCategories(p, q) {
				f.PointIDs = append(f.PointIDs, p.PointID)
			}
			if exact != nil && *exact != value {
				return ports.StatisticsFilter{}, fmt.Errorf("%w: exact and approximate points cannot be merged", ErrInvalidQuery)
			}
			exact = &value
			if len(f.PointCodes) == 1 {
				f.PointID, f.PointCode = p.PointID, p.PointCode
			}
		}
		f.ExactUV = exact != nil && *exact
	}
	if len(f.PointIDs) == 1 {
		f.PointID = f.PointIDs[0]
	}
	return f, nil
}

func alignedBoundary(value time.Time, granularity string, location *time.Location) bool {
	local := value.In(location)
	if local.Minute() != 0 || local.Second() != 0 || local.Nanosecond() != 0 {
		return false
	}
	return granularity != "day" || local.Hour() == 0
}

func (s *Service) RecentEvents(ctx context.Context, actor uint64, q DiagnosticQuery) (EventPage, error) {
	f, err := s.diagnosticFilter(ctx, actor, q)
	if err != nil {
		return EventPage{}, err
	}
	if s.diagnostics == nil {
		return EventPage{}, errors.New("diagnostics unavailable")
	}
	return s.diagnostics.ListRecent(ctx, f)
}
func (s *Service) InvalidEvents(ctx context.Context, actor uint64, q DiagnosticQuery) (InvalidEventPage, error) {
	f, err := s.diagnosticFilter(ctx, actor, q)
	if err != nil {
		return InvalidEventPage{}, err
	}
	if s.diagnostics == nil {
		return InvalidEventPage{}, errors.New("diagnostics unavailable")
	}
	return s.diagnostics.ListInvalid(ctx, f)
}
func (s *Service) DeadLetters(ctx context.Context, actor, projectID uint64, cursor string, limit int) (DeadLetterPage, error) {
	if projectID == 0 || limit < 1 || limit > 100 {
		return DeadLetterPage{}, fmt.Errorf("%w: project and limit", ErrInvalidQuery)
	}
	if err := s.auth.RequireRole(ctx, actor, projectID, project.Admin, project.Viewer); err != nil {
		return DeadLetterPage{}, err
	}
	if s.diagnostics == nil {
		return DeadLetterPage{}, errors.New("diagnostics unavailable")
	}
	return s.diagnostics.ListDeadLetters(ctx, DeadLetterFilter{ProjectID: projectID, Cursor: cursor, Limit: limit})
}
func (s *Service) diagnosticFilter(ctx context.Context, actor uint64, q DiagnosticQuery) (DiagnosticFilter, error) {
	if q.ProjectID == 0 || q.Start.IsZero() || !q.End.After(q.Start) || q.End.Sub(q.Start) > 30*24*time.Hour || q.Start.Before(s.now().UTC().Add(-30*24*time.Hour)) || q.Limit < 1 || q.Limit > 100 {
		return DiagnosticFilter{}, fmt.Errorf("%w: diagnostics are limited to the latest 30 days and 100 rows", ErrInvalidQuery)
	}
	if q.End.After(s.now().UTC().Add(10 * time.Minute)) {
		return DiagnosticFilter{}, fmt.Errorf("%w: end is in the future", ErrInvalidQuery)
	}
	if err := s.auth.RequireRole(ctx, actor, q.ProjectID, project.Admin, project.Viewer); err != nil {
		return DiagnosticFilter{}, err
	}
	return DiagnosticFilter{ProjectID: q.ProjectID, Start: q.Start.UTC(), End: q.End.UTC(), PointCodes: uniqueNonempty(q.PointCodes), Platform: q.Platform, Channel: q.Channel, ErrorCode: q.ErrorCode, Cursor: q.Cursor, Limit: q.Limit}, nil
}
func uniqueNonempty(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func hasCategoryFilter(q Query) bool {
	return q.DomainCategoryID != 0 || q.ModuleCategoryID != 0 || q.SceneCategoryID != 0 || q.DomainCategoryCode != "" || q.ModuleCategoryCode != "" || q.SceneCategoryCode != ""
}
func snapshotMatchesCategories(snapshot tracking.Snapshot, q Query) bool {
	for _, category := range snapshot.CategoryPath {
		switch category.Level {
		case tracking.Domain:
			if q.DomainCategoryID != 0 && category.ID != q.DomainCategoryID || q.DomainCategoryCode != "" && category.Code != q.DomainCategoryCode {
				return false
			}
		case tracking.Module:
			if q.ModuleCategoryID != 0 && category.ID != q.ModuleCategoryID || q.ModuleCategoryCode != "" && category.Code != q.ModuleCategoryCode {
				return false
			}
		case tracking.Scene:
			if q.SceneCategoryID != 0 && category.ID != q.SceneCategoryID || q.SceneCategoryCode != "" && category.Code != q.SceneCategoryCode {
				return false
			}
		}
	}
	return true
}
