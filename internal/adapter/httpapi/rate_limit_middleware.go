package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type CollectionRateLimiter interface {
	Allow(context.Context, string) (bool, time.Duration, error)
}

type RateLimitMiddleware struct{ limiter CollectionRateLimiter }

func NewRateLimitMiddleware(limiter CollectionRateLimiter) RateLimitMiddleware {
	return RateLimitMiddleware{limiter: limiter}
}

func (m RateLimitMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := CollectionProjectFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid write key")
			return
		}
		if m.limiter == nil {
			writeError(w, http.StatusServiceUnavailable, "collection service unavailable")
			return
		}
		key := collectionRateKey(p.ID, requestIP(r), r.Header.Get("X-Device-ID"))
		allowed, retry, err := m.limiter.Allow(r.Context(), key)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "collection service unavailable")
			return
		}
		if !allowed {
			seconds := int(math.Ceil(retry.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func collectionRateKey(projectID uint64, ip, device string) string {
	if len(device) > 256 {
		device = device[:256]
	}
	sumIP := sha256.Sum256([]byte(strings.TrimSpace(ip)))
	sumDevice := sha256.Sum256([]byte(strings.TrimSpace(device)))
	return fmt.Sprintf("collect:rate:%d:%s:%s", projectID, hex.EncodeToString(sumIP[:8]), hex.EncodeToString(sumDevice[:8]))
}

var tokenBucketScript = redis.NewScript(`
local current = redis.call('TIME')
local now = tonumber(current[1]) + tonumber(current[2]) / 1000000
local values = redis.call('HMGET', KEYS[1], 'tokens', 'updated')
local tokens = tonumber(values[1]) or tonumber(ARGV[1])
local updated = tonumber(values[2]) or now
tokens = math.min(tonumber(ARGV[1]), tokens + math.max(0, now - updated) * tonumber(ARGV[2]))
local allowed = 0
local retry = 0
if tokens >= 1 then
  allowed = 1
  tokens = tokens - 1
else
  retry = (1 - tokens) / tonumber(ARGV[2])
end
redis.call('HSET', KEYS[1], 'tokens', tokens, 'updated', now)
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[3]))
return {allowed, tostring(retry)}
`)

type RedisTokenBucket struct {
	client          redis.Scripter
	capacity        int
	refillPerSecond float64
	ttl             time.Duration
}

func NewRedisTokenBucket(client redis.Scripter, capacity int, refillPerSecond float64) *RedisTokenBucket {
	if capacity <= 0 {
		capacity = 200
	}
	if refillPerSecond <= 0 {
		refillPerSecond = 100
	}
	return &RedisTokenBucket{client: client, capacity: capacity, refillPerSecond: refillPerSecond, ttl: 10 * time.Minute}
}

func (b *RedisTokenBucket) Allow(ctx context.Context, key string) (bool, time.Duration, error) {
	if b == nil || b.client == nil {
		return false, 0, errors.New("redis rate limiter unavailable")
	}
	result, err := tokenBucketScript.Run(ctx, b.client, []string{key}, b.capacity, b.refillPerSecond, int(math.Ceil(b.ttl.Seconds()))).Slice()
	if err != nil {
		return false, 0, fmt.Errorf("run rate limit script: %w", err)
	}
	if len(result) != 2 {
		return false, 0, errors.New("invalid rate limit response")
	}
	allowed, err := redisInt64(result[0])
	if err != nil {
		return false, 0, errors.New("invalid rate limit decision")
	}
	retrySeconds, err := strconv.ParseFloat(fmt.Sprint(result[1]), 64)
	if err != nil {
		return false, 0, errors.New("invalid rate limit retry")
	}
	return allowed == 1, time.Duration(retrySeconds * float64(time.Second)), nil
}
func redisInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported Redis integer %T", value)
	}
}
