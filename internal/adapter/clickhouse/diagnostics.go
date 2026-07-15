package clickhouse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/statistics"
)

type diagnosticCursor struct {
	Kind       string    `json:"k"`
	ReceivedAt time.Time `json:"t"`
	EventID    string    `json:"e"`
	ErrorCode  string    `json:"c,omitempty"`
}

func (s *StatisticsStore) ListRecent(ctx context.Context, f statistics.DiagnosticFilter) (statistics.EventPage, error) {
	if s == nil || s.db == nil || !identifierPattern.MatchString(s.rawTable) {
		return statistics.EventPage{}, errors.New("invalid ClickHouse diagnostics store")
	}
	cursor, err := parseDiagnosticCursor(f.Cursor, "recent")
	if err != nil {
		return statistics.EventPage{}, err
	}
	query := `SELECT event_id,point_code,platform,channel,app_version,client_event_at,received_at,statistic_at,properties_json FROM ` + s.rawTable + ` FINAL WHERE project_id=? AND received_at>=? AND received_at<?`
	args := []any{f.ProjectID, f.Start.UTC(), f.End.UTC()}
	query, args = appendDiagnosticFilters(query, args, f, false)
	if f.Cursor != "" {
		query += ` AND (received_at<? OR (received_at=? AND event_id<?))`
		args = append(args, cursor.ReceivedAt, cursor.ReceivedAt, cursor.EventID)
	}
	query += ` ORDER BY received_at DESC,event_id DESC LIMIT ?`
	args = append(args, f.Limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return statistics.EventPage{}, fmt.Errorf("query recent events: %w", err)
	}
	defer rows.Close()
	page := statistics.EventPage{Items: []statistics.Event{}}
	for rows.Next() {
		var event statistics.Event
		var raw string
		if err := rows.Scan(&event.EventID, &event.PointCode, &event.Platform, &event.Channel, &event.AppVersion, &event.ClientEventAt, &event.ReceivedAt, &event.StatisticAt, &raw); err != nil {
			return page, fmt.Errorf("scan recent event: %w", err)
		}
		var properties map[string]any
		if raw != "" && json.Unmarshal([]byte(raw), &properties) == nil {
			event.Properties = sanitizeProperties(properties)
		}
		page.Items = append(page.Items, event)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > f.Limit {
		page.Items = page.Items[:f.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeDiagnosticCursor(diagnosticCursor{Kind: "recent", ReceivedAt: last.ReceivedAt, EventID: last.EventID})
	}
	return page, nil
}

func (s *StatisticsStore) ListInvalid(ctx context.Context, f statistics.DiagnosticFilter) (statistics.InvalidEventPage, error) {
	if s == nil || s.db == nil || !identifierPattern.MatchString(s.invalidEventsTable) {
		return statistics.InvalidEventPage{}, errors.New("invalid ClickHouse diagnostics store")
	}
	cursor, err := parseDiagnosticCursor(f.Cursor, "invalid")
	if err != nil {
		return statistics.InvalidEventPage{}, err
	}
	query := `SELECT event_id,point_code,platform,channel,error_code,error_message,received_at FROM ` + s.invalidEventsTable + ` FINAL WHERE project_id=? AND received_at>=? AND received_at<?`
	args := []any{f.ProjectID, f.Start.UTC(), f.End.UTC()}
	query, args = appendDiagnosticFilters(query, args, f, true)
	if f.Cursor != "" {
		query += ` AND (received_at<? OR (received_at=? AND (event_id<? OR (event_id=? AND error_code<?))))`
		args = append(args, cursor.ReceivedAt, cursor.ReceivedAt, cursor.EventID, cursor.EventID, cursor.ErrorCode)
	}
	query += ` ORDER BY received_at DESC,event_id DESC,error_code DESC LIMIT ?`
	args = append(args, f.Limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return statistics.InvalidEventPage{}, fmt.Errorf("query invalid events: %w", err)
	}
	defer rows.Close()
	page := statistics.InvalidEventPage{Items: []statistics.InvalidEvent{}}
	for rows.Next() {
		var event statistics.InvalidEvent
		if err := rows.Scan(&event.EventID, &event.PointCode, &event.Platform, &event.Channel, &event.ErrorCode, &event.ErrorMessage, &event.ReceivedAt); err != nil {
			return page, fmt.Errorf("scan invalid event: %w", err)
		}
		if len(event.ErrorMessage) > 500 {
			event.ErrorMessage = event.ErrorMessage[:500]
		}
		page.Items = append(page.Items, event)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > f.Limit {
		page.Items = page.Items[:f.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeDiagnosticCursor(diagnosticCursor{Kind: "invalid", ReceivedAt: last.ReceivedAt, EventID: last.EventID, ErrorCode: last.ErrorCode})
	}
	return page, nil
}

func appendDiagnosticFilters(query string, args []any, f statistics.DiagnosticFilter, invalid bool) (string, []any) {
	if len(f.PointCodes) > 0 {
		query += " AND point_code IN " + placeholders(len(f.PointCodes))
		for _, code := range f.PointCodes {
			args = append(args, code)
		}
	}
	if f.Platform != "" {
		query += " AND platform=?"
		args = append(args, f.Platform)
	}
	if f.Channel != "" {
		query += " AND channel=?"
		args = append(args, f.Channel)
	}
	if invalid && f.ErrorCode != "" {
		query += " AND error_code=?"
		args = append(args, f.ErrorCode)
	}
	return query, args
}

func parseDiagnosticCursor(raw, kind string) (diagnosticCursor, error) {
	if raw == "" {
		return diagnosticCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return diagnosticCursor{}, fmt.Errorf("%w: cursor", statistics.ErrInvalidQuery)
	}
	var cursor diagnosticCursor
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cursor); err != nil || cursor.Kind != kind || cursor.ReceivedAt.IsZero() || cursor.EventID == "" || (kind == "invalid" && cursor.ErrorCode == "") {
		return diagnosticCursor{}, fmt.Errorf("%w: cursor", statistics.ErrInvalidQuery)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return diagnosticCursor{}, fmt.Errorf("%w: cursor", statistics.ErrInvalidQuery)
	}
	return cursor, nil
}
func encodeDiagnosticCursor(cursor diagnosticCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

var sensitivePropertyKeys = map[string]struct{}{
	"user_id": {}, "anonymous_id": {}, "visitor_id": {}, "visitor_hash": {}, "session_id": {}, "session_hash": {},
	"password": {}, "passwd": {}, "secret": {}, "token": {}, "authorization": {}, "cookie": {}, "write_key": {}, "app_secret": {},
}

func sanitizeProperties(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, item := range value {
		normalized := strings.ToLower(key)
		_, exactSensitive := sensitivePropertyKeys[normalized]
		sensitive := exactSensitive || strings.Contains(normalized, "password") || strings.Contains(normalized, "passwd") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "token") || strings.Contains(normalized, "authorization") || strings.Contains(normalized, "cookie") || strings.Contains(normalized, "visitor") || strings.Contains(normalized, "anonymous_id") || strings.Contains(normalized, "user_id") || strings.Contains(normalized, "session")
		if sensitive {
			continue
		}
		switch nested := item.(type) {
		case map[string]any:
			out[key] = sanitizeProperties(nested)
		case []any:
			clean := make([]any, 0, len(nested))
			for _, child := range nested {
				if object, ok := child.(map[string]any); ok {
					clean = append(clean, sanitizeProperties(object))
				} else {
					clean = append(clean, child)
				}
			}
			out[key] = clean
		default:
			out[key] = item
		}
	}
	return out
}
