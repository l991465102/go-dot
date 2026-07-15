package rediscounter

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/tracking"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestApplyCountsEventOnceAndUsesProjectTimezone(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	counter := New(client)
	event := countEvent("evt-1")

	first, err := counter.Apply(context.Background(), event)
	require.NoError(t, err)
	second, err := counter.Apply(context.Background(), event)
	require.NoError(t, err)
	require.True(t, first)
	require.False(t, second)

	keys := keysFor(event)
	require.Equal(t, "1", mustGet(t, server, keys.DayPV))
	require.Equal(t, "1", mustGet(t, server, keys.HourPV))
	dayUV, err := server.PfCount(keys.DayUV)
	require.NoError(t, err)
	hourUV, err := server.PfCount(keys.HourUV)
	require.NoError(t, err)
	require.Equal(t, 1, dayUV)
	require.Equal(t, 1, hourUV)
	require.Equal(t, "2026071512", keys.HourBucket)
	require.Equal(t, "20260715", keys.DayBucket)
	for _, key := range keys.all() {
		require.Equal(t, 72*time.Hour, server.TTL(key), key)
	}
}

func TestApplyExactUVUsesSets(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	counter := New(client)
	event := countEvent("evt-exact")
	event.UVMode = tracking.Exact

	applied, err := counter.Apply(context.Background(), event)
	require.NoError(t, err)
	require.True(t, applied)
	keys := keysFor(event)
	members, err := server.SMembers(keys.DayUV)
	require.NoError(t, err)
	require.Equal(t, []string{"000000000000002a"}, members)
}

func TestApplyDoesNotMarkCompleteWhenCounterKeyHasWrongType(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	counter := New(client)
	event := countEvent("evt-bad")
	keys := keysFor(event)
	server.SAdd(keys.DayPV, "wrong-type")

	applied, err := counter.Apply(context.Background(), event)
	require.Error(t, err)
	require.False(t, applied)
	require.False(t, server.Exists(keys.Completion))
	require.False(t, server.Exists(keys.HourPV))
}

func TestApplyPreflightsUVKeysBeforeIncrementingPV(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	counter := New(client)
	event := countEvent("evt-bad-uv")
	keys := keysFor(event)
	server.SAdd(keys.DayUV, "wrong-type-for-hll")

	applied, err := counter.Apply(context.Background(), event)
	require.Error(t, err)
	require.False(t, applied)
	require.False(t, server.Exists(keys.Completion))
	require.False(t, server.Exists(keys.HourPV))
	require.False(t, server.Exists(keys.DayPV))
}

func TestApplyPreflightsEveryPVValueBeforeMutationAndRetriesCleanly(t *testing.T) {
	tests := []struct {
		name  string
		value string
		key   func(counterKeys) string
	}{
		{name: "nonnumeric hour", value: "not-an-integer", key: func(keys counterKeys) string { return keys.HourPV }},
		{name: "nonnumeric day", value: "not-an-integer", key: func(keys counterKeys) string { return keys.DayPV }},
		{name: "overflow hour", value: "9223372036854775807", key: func(keys counterKeys) string { return keys.HourPV }},
		{name: "overflow day", value: "9223372036854775807", key: func(keys counterKeys) string { return keys.DayPV }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			event := countEvent("evt-corrupt-pv")
			keys := keysFor(event)
			badKey := tt.key(keys)
			server.Set(badKey, tt.value)

			applied, err := New(client).Apply(context.Background(), event)
			require.Error(t, err)
			require.False(t, applied)
			require.Equal(t, tt.value, mustGet(t, server, badKey))
			require.False(t, server.Exists(keys.Completion))
			for _, key := range []string{keys.HourPV, keys.DayPV, keys.HourUV, keys.DayUV} {
				if key != badKey {
					require.False(t, server.Exists(key), key)
				}
			}

			server.Del(badKey)
			first, err := New(client).Apply(context.Background(), event)
			require.NoError(t, err)
			second, err := New(client).Apply(context.Background(), event)
			require.NoError(t, err)
			require.True(t, first)
			require.False(t, second)
			require.Equal(t, "1", mustGet(t, server, keys.HourPV))
			require.Equal(t, "1", mustGet(t, server, keys.DayPV))
		})
	}
}

func TestApplyPreflightsEveryApproximateUVValueBeforeMutationAndRetriesCleanly(t *testing.T) {
	tests := []struct {
		name string
		key  func(counterKeys) string
	}{
		{name: "hour HLL", key: func(keys counterKeys) string { return keys.HourUV }},
		{name: "day HLL", key: func(keys counterKeys) string { return keys.DayUV }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			event := countEvent("evt-corrupt-hll")
			keys := keysFor(event)
			badKey := tt.key(keys)
			server.Set(badKey, "ordinary-string")

			applied, err := New(client).Apply(context.Background(), event)
			require.Error(t, err)
			require.False(t, applied)
			require.Equal(t, "ordinary-string", mustGet(t, server, badKey))
			require.False(t, server.Exists(keys.Completion))
			for _, key := range []string{keys.HourPV, keys.DayPV, keys.HourUV, keys.DayUV} {
				if key != badKey {
					require.False(t, server.Exists(key), key)
				}
			}

			server.Del(badKey)
			first, err := New(client).Apply(context.Background(), event)
			require.NoError(t, err)
			second, err := New(client).Apply(context.Background(), event)
			require.NoError(t, err)
			require.True(t, first)
			require.False(t, second)
			hourUV, err := server.PfCount(keys.HourUV)
			require.NoError(t, err)
			dayUV, err := server.PfCount(keys.DayUV)
			require.NoError(t, err)
			require.Equal(t, 1, hourUV)
			require.Equal(t, 1, dayUV)
		})
	}
}

func TestApplyRejectsInvalidTimezoneWithoutWriting(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	event := countEvent("evt-timezone")
	event.ProjectTimezone = "Mars/Olympus"

	applied, err := New(client).Apply(context.Background(), event)
	require.ErrorContains(t, err, "project timezone")
	require.False(t, applied)
	require.Empty(t, server.Keys())
}

func TestCounterKeysShareAClusterHashTagAndIsolateDimensions(t *testing.T) {
	event := countEvent("evt-1")
	keys := keysFor(event)
	for _, key := range keys.all() {
		require.Contains(t, key, "{project:7}")
	}
	other := event
	other.Platform = "web:mobile"
	require.NotEqual(t, keys.DayPV, keysFor(other).DayPV)
	other = event
	other.ProjectID = 8
	require.NotEqual(t, keys.DayPV, keysFor(other).DayPV)
}

func TestQueryRealtimeMergesPVAndExactUVAcrossHourlyBuckets(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	counter := New(client)
	start := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	base := ports.CountEvent{ProjectID: 7, PointID: 9, Platform: "web", Channel: "organic", ProjectTimezone: "UTC", CountPV: true, CountUV: true, UVMode: tracking.Exact}
	for _, event := range []ports.CountEvent{
		withCountEvent(base, "a", 1, start.Add(time.Minute)),
		withCountEvent(base, "b", 1, start.Add(time.Hour+time.Minute)),
		withCountEvent(base, "c", 2, start.Add(time.Hour+time.Minute)),
	} {
		_, err := counter.Apply(context.Background(), event)
		require.NoError(t, err)
	}
	got, err := counter.QueryRealtime(context.Background(), ports.StatisticsFilter{ProjectID: 7, PointID: 9, Platform: "web", Channel: "organic", Start: start, End: start.Add(2 * time.Hour), Timezone: "UTC", StorageTimezone: "UTC", ExactUV: true})
	require.NoError(t, err)
	require.EqualValues(t, 3, got.PV)
	require.EqualValues(t, 2, got.UV)
	require.False(t, got.Approximate)
	require.NotEmpty(t, s.Keys())
}

func TestQueryRealtimeUsesStorageTimezoneAndFindsUVWithoutPVKey(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	counter := New(client)
	at := time.Date(2026, 7, 15, 16, 30, 0, 0, time.FixedZone("CST", 8*3600))
	event := ports.CountEvent{EventID: "uv-only", ProjectID: 7, PointID: 9, Platform: "web", Channel: "organic", ProjectTimezone: "Asia/Shanghai", StatTime: at, VisitorHash: 1, CountUV: true, UVMode: tracking.Exact}
	_, err := counter.Apply(context.Background(), event)
	require.NoError(t, err)
	got, err := counter.QueryRealtime(context.Background(), ports.StatisticsFilter{ProjectID: 7, PointID: 9, Start: at.Truncate(time.Hour), End: at.Truncate(time.Hour).Add(time.Hour), StorageTimezone: "Asia/Shanghai", Timezone: "America/New_York", ExactUV: true})
	require.NoError(t, err)
	require.Zero(t, got.PV)
	require.EqualValues(t, 1, got.UV)
}

func withCountEvent(base ports.CountEvent, id string, visitor uint64, at time.Time) ports.CountEvent {
	base.EventID, base.VisitorHash, base.StatTime = id, visitor, at
	return base
}

func countEvent(eventID string) ports.CountEvent {
	return ports.CountEvent{
		EventID: eventID, ProjectID: 7, PointID: 9, Platform: "web", Channel: "organic",
		StatTime: time.Date(2026, 7, 15, 4, 30, 0, 0, time.UTC), ProjectTimezone: "Asia/Shanghai",
		VisitorHash: 42, CountPV: true, CountUV: true, UVMode: tracking.Approximate,
	}
}

func mustGet(t *testing.T, server *miniredis.Miniredis, key string) string {
	t.Helper()
	value, err := server.Get(key)
	require.NoError(t, err)
	return value
}
