package rediscounter

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
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
