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
	completed, err := m.client.Exists(ctx, doneKey).Result()
	if err != nil {
		return fmt.Errorf("check Redis lease completion %q: %w", key, err)
	}
	if completed > 0 {
		return nil
	}
	token, err := leaseToken()
	if err != nil {
		return err
	}
	acquired, err := m.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return fmt.Errorf("acquire Redis lease %q: %w", key, err)
	}
	if !acquired {
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
		if err := m.client.Set(releaseCtx, doneKey, token, ttl).Err(); err != nil {
			jobErr = fmt.Errorf("mark Redis lease %q complete: %w", key, err)
		}
	}
	if _, e := releaseLeaseScript.Run(releaseCtx, m.client, []string{key}, token).Result(); e != nil && jobErr == nil {
		jobErr = fmt.Errorf("release Redis lease %q: %w", key, e)
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
