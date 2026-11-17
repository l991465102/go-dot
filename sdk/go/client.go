// Package godot 提供 go-dot 埋点采集客户端。
package godot

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Event 是一次埋点事件；同一业务事件重试时必须保留 EventID。
type Event struct {
	EventID     string         `json:"event_id"`
	PointCode   string         `json:"point_code"`
	EventTime   int64          `json:"event_time"`
	UserID      string         `json:"user_id,omitempty"`
	AnonymousID string         `json:"anonymous_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	Platform    string         `json:"platform"`
	AppVersion  string         `json:"app_version,omitempty"`
	Channel     string         `json:"channel,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

// EventResult 是服务端对单条事件的校验结果，Index 对应原始事件切片。
type EventResult struct {
	Index   int    `json:"index"`
	EventID string `json:"event_id"`
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
type response struct {
	BatchID  string        `json:"batch_id"`
	Accepted int           `json:"accepted"`
	Rejected int           `json:"rejected"`
	Results  []EventResult `json:"results"`
}

// HTTPError 表示采集服务返回非成功 HTTP 状态。
type HTTPError struct{ StatusCode int }

// Error 返回 HTTP 状态错误说明。
func (e *HTTPError) Error() string { return fmt.Sprintf("go-dot: HTTP %d", e.StatusCode) }

// Client 是可复用的上报客户端。
type Client struct {
	endpoint string
	key      string
	http     *http.Client
	retries  int
	backoff  time.Duration
}

// Options 控制服务地址、环境、重试和请求超时。
type Options struct {
	BaseURL      string
	WriteKey     string
	Test         bool
	HTTPClient   *http.Client
	MaxRetries   int
	RetryBackoff time.Duration
}

// NewClient 构造客户端；MaxRetries 为零时不自动重试。
func NewClient(o Options) (*Client, error) {
	u, err := url.Parse(o.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || o.WriteKey == "" || o.MaxRetries < 0 || o.MaxRetries > 10 {
		return nil, errors.New("go-dot: invalid options")
	}
	path := "/api/v1/collect/events"
	if o.Test {
		path = "/api/v1/collect/test-events"
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	// 禁止重定向，避免将项目写入密钥转发到其他地址。
	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &clientCopy
	backoff := o.RetryBackoff
	if backoff <= 0 {
		backoff = 200 * time.Millisecond
	}
	return &Client{endpoint: u.String(), key: o.WriteKey, http: client, retries: o.MaxRetries, backoff: backoff}, nil
}

// Prepare 为事件补充 UUIDv7 和发生时间；可在保存本地队列之前调用。
func Prepare(e *Event) error {
	if e == nil {
		return errors.New("go-dot: nil event")
	}
	if e.EventTime == 0 {
		e.EventTime = time.Now().UnixMilli()
	}
	if e.EventID == "" {
		id, err := newUUIDv7(time.Now())
		if err != nil {
			return err
		}
		e.EventID = id
	}
	return nil
}
func newUUIDv7(now time.Time) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	ms := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	var dst [36]byte
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst[:]), nil
}

// Send 按最多 100 条分批发送，原地补充 ID；错误时返回已完成批次结果，调用方可用相同 ID 重试未完成的事件。
func (c *Client) Send(ctx context.Context, events []Event) ([]EventResult, error) {
	if len(events) == 0 {
		return nil, errors.New("go-dot: empty events")
	}
	for i := range events {
		if err := Prepare(&events[i]); err != nil {
			return nil, err
		}
	}
	results := make([]EventResult, 0, len(events))
	for start := 0; start < len(events); start += 100 {
		end := start + 100
		if end > len(events) {
			end = len(events)
		}
		body, err := json.Marshal(struct {
			Events []Event `json:"events"`
		}{events[start:end]})
		if err != nil {
			return results, err
		}
		res, err := c.sendBatch(ctx, body)
		if err != nil {
			return results, err
		}
		if err = validateResults(res, events[start:end]); err != nil {
			return results, err
		}
		for _, item := range res.Results {
			item.Index += start
			results = append(results, item)
		}
	}
	return results, nil
}
func validateResults(res response, events []Event) error {
	if len(res.Results) != len(events) || res.Accepted+res.Rejected != len(events) {
		return errors.New("go-dot: malformed response")
	}
	seen := make([]bool, len(events))
	a, r := 0, 0
	for _, item := range res.Results {
		if item.Index < 0 || item.Index >= len(events) || seen[item.Index] || item.EventID != events[item.Index].EventID {
			return errors.New("go-dot: malformed event result")
		}
		seen[item.Index] = true
		switch item.Status {
		case "accepted":
			a++
		case "rejected":
			r++
		default:
			return errors.New("go-dot: malformed event status")
		}
	}
	if a != res.Accepted || r != res.Rejected {
		return errors.New("go-dot: malformed response counts")
	}
	return nil
}

// sendBatch 发送并按可重试状态重试单个批次。
func (c *Client) sendBatch(ctx context.Context, body []byte) (response, error) {
	var last error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			delay := c.backoff * time.Duration(1<<uint(attempt-1))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return response{}, ctx.Err()
			case <-timer.C:
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if err != nil {
			return response{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Write-Key", c.key)
		resp, err := c.http.Do(req)
		if err != nil {
			last = err
			if ctx.Err() != nil {
				return response{}, ctx.Err()
			}
			continue
		}
		if resp.StatusCode == http.StatusAccepted {
			var out response
			decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
			err = decoder.Decode(&out)
			resp.Body.Close()
			if err != nil {
				return response{}, fmt.Errorf("go-dot: invalid response: %w", err)
			}
			return out, nil
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		last = &HTTPError{StatusCode: resp.StatusCode}
		if resp.StatusCode != 429 && resp.StatusCode < 500 {
			return response{}, last
		}
	}
	return response{}, last
}
