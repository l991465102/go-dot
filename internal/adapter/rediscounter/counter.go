package rediscounter

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/tracking"
	"github.com/redis/go-redis/v9"
)

const retention = 72 * time.Hour

//go:embed update.lua
var updateLua string

var updateScript = redis.NewScript(updateLua)

type Counter struct {
	client redis.UniversalClient
}

func New(client redis.UniversalClient) *Counter { return &Counter{client: client} }

type counterKeys struct {
	Completion string
	HourPV     string
	DayPV      string
	HourUV     string
	DayUV      string
	HourBucket string
	DayBucket  string
}

func (k counterKeys) all() []string {
	return []string{k.Completion, k.HourPV, k.DayPV, k.HourUV, k.DayUV}
}

func (c *Counter) Apply(ctx context.Context, event ports.CountEvent) (bool, error) {
	if c == nil || c.client == nil {
		return false, errors.New("Redis counter client is required")
	}
	if event.EventID == "" || event.ProjectID == 0 || event.PointID == 0 {
		return false, errors.New("event_id, project_id and point_id are required")
	}
	if event.CountUV && event.VisitorHash == 0 {
		return false, errors.New("visitor hash is required for UV")
	}
	if event.CountUV && event.UVMode != tracking.Exact && event.UVMode != tracking.Approximate {
		return false, fmt.Errorf("unsupported UV mode %q", event.UVMode)
	}
	keys, err := buildKeys(event)
	if err != nil {
		return false, err
	}
	result, err := updateScript.Run(ctx, c.client, keys.all(),
		int64(retention/time.Second), fmt.Sprintf("%016x", event.VisitorHash), boolArg(event.CountPV), boolArg(event.CountUV), string(event.UVMode),
	).Int()
	if err != nil {
		return false, fmt.Errorf("apply realtime counters: %w", err)
	}
	return result == 1, nil
}

// QueryRealtime reads only Redis counters. Hour keys are merged so UVs are not
// added across buckets: exact sets are unioned and approximate HLLs are merged
// by PFCOUNT's multi-key form.
func (c *Counter) QueryRealtime(ctx context.Context, f ports.StatisticsFilter) (ports.Summary, error) {
	if c == nil || c.client == nil {
		return ports.Summary{}, errors.New("Redis counter client is required")
	}
	if f.ProjectID == 0 || f.Start.IsZero() || !f.End.After(f.Start) {
		return ports.Summary{}, errors.New("project and valid time range are required")
	}
	tag := "{project:" + strconv.FormatUint(f.ProjectID, 10) + "}"
	point := "*"
	if f.PointID != 0 {
		point = strconv.FormatUint(f.PointID, 10)
	}
	platform, channel := "*", "*"
	if f.Platform != "" {
		platform = encodePart(f.Platform)
	}
	if f.Channel != "" {
		channel = encodePart(f.Channel)
	}
	pvPattern := "rt:pv:hour:" + tag + ":*:" + point + ":" + platform + ":" + channel
	pvKeys, err := c.scanRange(ctx, pvPattern, f)
	if err != nil {
		return ports.Summary{}, err
	}
	var pv uint64
	mode := string(tracking.Approximate)
	if f.ExactUV {
		mode = string(tracking.Exact)
	}
	for _, key := range pvKeys {
		value, err := c.client.Get(ctx, key).Uint64()
		if err != nil && err != redis.Nil {
			return ports.Summary{}, fmt.Errorf("read realtime PV: %w", err)
		}
		pv += value
	}
	uvPattern := "rt:uv:hour:" + tag + ":*:" + point + ":" + platform + ":" + channel + ":" + mode
	uvKeys, err := c.scanRange(ctx, uvPattern, f)
	if err != nil {
		return ports.Summary{}, err
	}
	var uv uint64
	if f.ExactUV {
		members := make(map[string]struct{})
		for _, key := range uvKeys {
			values, err := c.client.SMembers(ctx, key).Result()
			if err != nil && err != redis.Nil {
				return ports.Summary{}, fmt.Errorf("read realtime exact UV: %w", err)
			}
			for _, value := range values {
				members[value] = struct{}{}
			}
		}
		uv = uint64(len(members))
	} else if len(uvKeys) > 0 {
		value, err := c.client.PFCount(ctx, uvKeys...).Result()
		if err != nil && err != redis.Nil {
			return ports.Summary{}, fmt.Errorf("read realtime approximate UV: %w", err)
		}
		uv = uint64(value)
	}
	return ports.Summary{PV: pv, UV: uv, Approximate: !f.ExactUV}, nil
}

func (c *Counter) scanRange(ctx context.Context, pattern string, f ports.StatisticsFilter) ([]string, error) {
	var cursor uint64
	var keys []string
	timezone := f.StorageTimezone
	if timezone == "" {
		return nil, errors.New("trusted storage timezone is required")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}
	for {
		batch, next, err := c.client.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return nil, fmt.Errorf("scan realtime counters: %w", err)
		}
		for _, key := range batch {
			parts := strings.Split(key, ":")
			if len(parts) < 9 {
				continue
			}
			if len(f.PointIDs) > 0 {
				pointID, err := strconv.ParseUint(parts[6], 10, 64)
				if err != nil || !containsPointID(f.PointIDs, pointID) {
					continue
				}
			}
			bucket := parts[5]
			at, err := time.ParseInLocation("2006010215", bucket, location)
			if err == nil && at.Before(f.End.In(location)) && at.Add(time.Hour).After(f.Start.In(location)) {
				// Defensively re-check Scan results, as some Redis-compatible stores
				// may return a superset for MATCH.
				if ok, _ := path.Match(pattern, key); ok {
					keys = append(keys, key)
				}
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return keys, nil
}

func containsPointID(values []uint64, target uint64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func keysFor(event ports.CountEvent) counterKeys {
	keys, _ := buildKeys(event)
	return keys
}

func buildKeys(event ports.CountEvent) (counterKeys, error) {
	timezone := event.ProjectTimezone
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return counterKeys{}, fmt.Errorf("load project timezone %q: %w", timezone, err)
	}
	local := event.StatTime.In(location)
	hour := local.Format("2006010215")
	day := local.Format("20060102")
	tag := "{project:" + strconv.FormatUint(event.ProjectID, 10) + "}"
	dimension := strconv.FormatUint(event.PointID, 10) + ":" + encodePart(event.Platform) + ":" + encodePart(event.Channel)
	eventSum := sha256.Sum256([]byte(event.EventID))
	return counterKeys{
		Completion: "rt:event:" + tag + ":" + hex.EncodeToString(eventSum[:]),
		HourPV:     "rt:pv:hour:" + tag + ":" + hour + ":" + dimension,
		DayPV:      "rt:pv:day:" + tag + ":" + day + ":" + dimension,
		HourUV:     "rt:uv:hour:" + tag + ":" + hour + ":" + dimension + ":" + string(event.UVMode),
		DayUV:      "rt:uv:day:" + tag + ":" + day + ":" + dimension + ":" + string(event.UVMode),
		HourBucket: hour,
		DayBucket:  day,
	}, nil
}

func encodePart(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }
func boolArg(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

var _ ports.RealtimeCounter = (*Counter)(nil)
