package redisqueue

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEveryMultiKeyScriptUsesOneRedisClusterSlot(t *testing.T) {
	stream := "tracking:{queue}:events"
	dead := "tracking:{queue}:dead"
	sets := map[string][]string{
		"publish":        {stream, publishDedupKey(stream, "batch")},
		"schedule retry": {stream, retryScheduleKey(stream), retryDataKey(stream)},
		"promote retry":  {stream, retryScheduleKey(stream), retryDataKey(stream)},
		"move DLQ":       {dead, stream},
	}
	for name, keys := range sets {
		t.Run(name, func(t *testing.T) {
			want := redisSlot(keys[0])
			for _, key := range keys[1:] {
				require.Equal(t, want, redisSlot(key), "%q and %q must share a Redis Cluster slot", keys[0], key)
			}
		})
	}
}

func redisSlot(key string) uint16 {
	tag, ok := redisHashTag(key)
	if ok {
		key = tag
	}
	return crc16([]byte(key)) % 16384
}

func crc16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
