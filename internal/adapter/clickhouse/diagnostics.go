package clickhouse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
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
	"userid": {}, "anonymousid": {}, "visitorid": {}, "visitorhash": {}, "sessionid": {}, "sessionhash": {},
	"password": {}, "passwd": {}, "secret": {}, "token": {}, "authorization": {}, "cookie": {}, "writekey": {}, "appsecret": {}, "accesstoken": {}, "refreshtoken": {},
	"apikey": {}, "accesskey": {}, "credential": {}, "credentials": {}, "clientsecret": {}, "privatekey": {}, "signingkey": {},
}

func sanitizeProperties(value map[string]any) map[string]any {
	cleaned, _ := sanitizeDiagnosticValue(reflect.ValueOf(value), 0).(map[string]any)
	if cleaned == nil {
		return map[string]any{}
	}
	return cleaned
}

func sanitizeDiagnosticValue(value reflect.Value, depth int) any {
	if !value.IsValid() || depth > 32 {
		return nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Map:
		out := map[string]any{}
		iter := value.MapRange()
		for iter.Next() {
			keyValue := iter.Key()
			if keyValue.Kind() != reflect.String {
				continue
			}
			key := keyValue.String()
			if sensitiveDiagnosticKey(key) {
				continue
			}
			out[key] = sanitizeDiagnosticValue(iter.Value(), depth+1)
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, value.Len())
		for i := 0; i < value.Len(); i++ {
			out[i] = sanitizeDiagnosticValue(value.Index(i), depth+1)
		}
		return out
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return nil
	}
}
func sensitiveDiagnosticKey(key string) bool {
	var canonical strings.Builder
	for _, r := range strings.ToLower(key) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			canonical.WriteRune(r)
		}
	}
	value := canonical.String()
	if _, ok := sensitivePropertyKeys[value]; ok {
		return true
	}
	for _, fragment := range []string{"password", "passwd", "secret", "token", "authorization", "cookie", "visitor", "anonymousid", "userid", "sessionid", "sessionhash", "apikey", "accesskey", "credential", "privatekey", "signingkey"} {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
