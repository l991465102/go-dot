package redisqueue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var releaseLeaseScript = redis.NewScript(`if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) else return 0 end`)
var renewLeaseScript = redis.NewScript(`if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('PEXPIRE', KEYS[1], ARGV[2]) else return 0 end`)

const acquireLeaseLua = `
if redis.call('EXISTS', KEYS[2]) == 1 then return 0 end
if redis.call('SET', KEYS[1], ARGV[1], 'NX', 'PX', ARGV[2]) then return 1 end
return 0`
const completeLeaseLua = `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[2], ARGV[1], 'PX', ARGV[2])
redis.call('PEXPIRE', KEYS[2], ARGV[2])
redis.call('DEL', KEYS[1])
return 1`

var acquireLeaseScript = redis.NewScript(acquireLeaseLua)
var completeLeaseScript = redis.NewScript(completeLeaseLua)

type LeaseManager struct{ client redis.UniversalClient }

func NewLeaseManager(client redis.UniversalClient) *LeaseManager {
	return &LeaseManager{client: client}
}

func (m *LeaseManager) WithLease(ctx context.Context, key string, ttl time.Duration, fn func(context.Context) error) error {
	if m == nil || m.client == nil {
		return errors.New("Redis lease client is required")
	}
	if key == "" || ttl <= 0 || fn == nil {
		return errors.New("lease key, positive ttl, and function are required")
	}
	doneKey := key + ":done"
	token, err := leaseToken()
	if err != nil {
		return err
	}
	acquired, err := acquireLeaseScript.Run(ctx, m.client, []string{key, doneKey}, token, ttl.Milliseconds()).Int()
	if err != nil {
		return fmt.Errorf("acquire Redis lease %q: %w", key, err)
	}
	if acquired == 0 {
		return nil
	}
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	renewErr := make(chan error, 1)
	go func() {
		defer close(done)
		interval := ttl / 3
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				result, e := renewLeaseScript.Run(jobCtx, m.client, []string{key}, token, ttl.Milliseconds()).Int()
				if e != nil || result == 0 {
					if e == nil {
						e = errors.New("lease ownership lost")
					}
					select {
					case renewErr <- e:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()
	jobErr := fn(jobCtx)
	cancel()
	<-done
	releaseCtx := context.WithoutCancel(ctx)
	if jobErr == nil {
		completed, e := completeLeaseScript.Run(releaseCtx, m.client, []string{key, doneKey}, token, ttl.Milliseconds()).Int()
		if e != nil {
			jobErr = fmt.Errorf("complete Redis lease %q: %w", key, e)
		} else if completed == 0 {
			jobErr = fmt.Errorf("complete Redis lease %q: ownership lost", key)
		}
	} else {
		if _, e := releaseLeaseScript.Run(releaseCtx, m.client, []string{key}, token).Result(); e != nil && jobErr == nil {
			jobErr = fmt.Errorf("release Redis lease %q: %w", key, e)
		}
	}
	select {
	case e := <-renewErr:
		if jobErr == nil {
			jobErr = fmt.Errorf("renew Redis lease %q: %w", key, e)
		}
	default:
	}
	return jobErr
}
func leaseToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate lease token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
