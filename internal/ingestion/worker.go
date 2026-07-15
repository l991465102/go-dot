package ingestion

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/ports"
	"github.com/lzw7758/go-dot/internal/statistics"
	"github.com/lzw7758/go-dot/internal/tracking"
)

const (
	workerReadCount = 16
	workerReadBlock = 2 * time.Second
)

type WorkerConfig struct {
	EventStream      string
	InvalidStream    string
	Group            string
	Consumer         string
	VisitorMasterKey []byte
	Concurrency      int
	MaxDeliveries    int
	ReclaimIdle      time.Duration
	DrainTimeout     time.Duration
}

type Worker struct {
	queue   ports.MessageQueue
	store   ports.EventStore
	counter ports.RealtimeCounter
	cfg     WorkerConfig
}

func NewWorker(queue ports.MessageQueue, store ports.EventStore, counter ports.RealtimeCounter, cfg WorkerConfig) *Worker {
	if cfg.EventStream == "" {
		cfg.EventStream = EventStream
	}
	if cfg.InvalidStream == "" {
		cfg.InvalidStream = InvalidEventStream
	}
	if cfg.Group == "" {
		cfg.Group = WorkerGroup
	}
	if cfg.Consumer == "" {
		cfg.Consumer = "worker"
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 4
	}
	if cfg.MaxDeliveries < 1 {
		cfg.MaxDeliveries = 5
	}
	if cfg.ReclaimIdle <= 0 {
		cfg.ReclaimIdle = 30 * time.Second
	}
	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = 30 * time.Second
	}
	return &Worker{queue: queue, store: store, counter: counter, cfg: cfg}
}

func (w *Worker) Handle(ctx context.Context, msg ports.Message) error {
	if err := w.validate(); err != nil {
		return err
	}
	if msg.ID == "" || msg.BatchID == "" || len(msg.Payload) == 0 {
		return w.finishFailure(ctx, msg, permanent(errors.New("message id, batch id and payload are required")))
	}
	var err error
	switch msg.Stream {
	case w.cfg.EventStream:
		err = w.handleValid(ctx, msg)
	case w.cfg.InvalidStream:
		err = w.handleInvalid(ctx, msg)
	default:
		err = permanent(fmt.Errorf("unsupported source stream %q", msg.Stream))
	}
	if err != nil {
		return w.finishFailure(ctx, msg, err)
	}
	if err := w.queue.Ack(ctx, msg.Stream, w.cfg.Group, msg.ID); err != nil {
		return fmt.Errorf("ack processed message: %w", err)
	}
	return nil
}

func (w *Worker) validate() error {
	if w == nil || w.queue == nil || w.store == nil || w.counter == nil {
		return errors.New("worker queue, event store, and realtime counter are required")
	}
	if len(w.cfg.VisitorMasterKey) == 0 {
		return errors.New("visitor master key is required")
	}
	return nil
}

func (w *Worker) handleValid(ctx context.Context, msg ports.Message) error {
	var payload batchPayload[NormalizedEvent]
	if err := decodePayload(msg, &payload); err != nil {
		return err
	}
	stored := make([]ports.StoredEvent, 0, len(payload.Events))
	counted := make([]ports.CountEvent, 0, len(payload.Events))
	for i, event := range payload.Events {
		if err := validateNormalized(payload.ProjectID, event); err != nil {
			return permanent(fmt.Errorf("event %d: %w", i, err))
		}
		visitorHash := statistics.HashVisitor(w.cfg.VisitorMasterKey, []byte(strconv.FormatUint(event.ProjectID, 10)), event.UserID, event.AnonymousID)
		if visitorHash == 0 {
			return permanent(fmt.Errorf("event %d: visitor identity is required", i))
		}
		domain, module, scene := event.CategoryPath[0], event.CategoryPath[1], event.CategoryPath[2]
		stored = append(stored, ports.StoredEvent{
			EventID: event.EventID, ProjectID: event.ProjectID, PointID: event.PointID, PointCode: event.PointCode,
			PointVersion: uint32(event.PointVersion), DomainCategoryID: domain.ID, DomainCategoryCode: domain.Code,
			ModuleCategoryID: module.ID, ModuleCategoryCode: module.Code, SceneCategoryID: scene.ID, SceneCategoryCode: scene.Code,
			VisitorHash: visitorHash, Platform: event.Platform, Channel: event.Channel, AppVersion: event.AppVersion,
			SessionHash: opaqueSession(w.cfg.VisitorMasterKey, event.ProjectID, event.SessionID), ClientEventAt: event.ClientTime,
			ReceivedAt: event.ReceivedAt, StatisticAt: event.StatTime, ProjectTimezone: event.ProjectTimezone,
			TimeCorrected: event.TimeCorrected, Properties: event.Properties, Version: uint64(event.ReceivedAt.UnixMilli()),
		})
		counted = append(counted, ports.CountEvent{EventID: event.EventID, ProjectID: event.ProjectID, PointID: event.PointID,
			Platform: event.Platform, Channel: event.Channel, StatTime: event.StatTime, ProjectTimezone: event.ProjectTimezone,
			VisitorHash: visitorHash, CountPV: event.CountPV, CountUV: event.CountUV, UVMode: event.UVMode})
	}
	if err := w.store.SaveValid(ctx, msg.BatchID, stored); err != nil {
		return fmt.Errorf("store valid events: %w", err)
	}
	for _, event := range counted {
		// applied=false means the event completion marker already exists. That is
		// the expected no-op during retry after a partially counted batch.
		if _, err := w.counter.Apply(ctx, event); err != nil {
			return fmt.Errorf("count event %q: %w", event.EventID, err)
		}
	}
	return nil
}

func (w *Worker) handleInvalid(ctx context.Context, msg ports.Message) error {
	var payload batchPayload[invalidEvent]
	if err := decodePayload(msg, &payload); err != nil {
		return err
	}
	events := make([]ports.InvalidEvent, 0, len(payload.Events))
	for i, event := range payload.Events {
		if event.ProjectID != payload.ProjectID || event.BatchID != msg.BatchID || event.ErrorCode == "" || len(event.Original) == 0 {
			return permanent(fmt.Errorf("invalid event %d has inconsistent metadata", i))
		}
		var original struct {
			EventID   string `json:"event_id"`
			PointCode string `json:"point_code"`
			Platform  string `json:"platform"`
			Channel   string `json:"channel"`
		}
		if err := json.Unmarshal(event.Original, &original); err != nil {
			return permanent(fmt.Errorf("invalid event %d original JSON: %w", i, err))
		}
		events = append(events, ports.InvalidEvent{ProjectID: event.ProjectID, EventID: original.EventID, PointCode: original.PointCode,
			Platform: original.Platform, Channel: original.Channel, ErrorCode: event.ErrorCode, ErrorMessage: event.ErrorMessage,
			Original: event.Original, ReceivedAt: event.ReceivedAt, Version: uint64(event.ReceivedAt.UnixMilli())})
	}
	if err := w.store.SaveInvalid(ctx, msg.BatchID, events); err != nil {
		return fmt.Errorf("store invalid events: %w", err)
	}
	return nil
}

func decodePayload[T any](msg ports.Message, target *batchPayload[T]) error {
	decoder := json.NewDecoder(strings.NewReader(string(msg.Payload)))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return permanent(fmt.Errorf("decode batch payload: %w", err))
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return permanent(errors.New("decode batch payload: multiple JSON values"))
	} else if !errors.Is(err, io.EOF) {
		return permanent(fmt.Errorf("decode trailing batch payload: %w", err))
	}
	if target.BatchID == "" || target.BatchID != msg.BatchID || target.ProjectID == 0 || target.ReceivedAt.IsZero() || len(target.Events) == 0 {
		return permanent(errors.New("batch payload metadata is incomplete or inconsistent"))
	}
	return nil
}

func validateNormalized(projectID uint64, event NormalizedEvent) error {
	if event.ProjectID != projectID || event.SnapshotID == 0 || event.PointID == 0 || event.PointVersion == 0 || event.PointCode == "" || event.EventID == "" {
		return errors.New("immutable snapshot metadata is incomplete")
	}
	if event.ReceivedAt.IsZero() || event.ClientTime.IsZero() || event.StatTime.IsZero() || event.ProjectTimezone == "" {
		return errors.New("event time metadata is incomplete")
	}
	if len(event.CategoryPath) != 3 || event.CategoryPath[0].Level != tracking.Domain || event.CategoryPath[1].Level != tracking.Module || event.CategoryPath[2].Level != tracking.Scene {
		return errors.New("category snapshot must contain DOMAIN, MODULE, and SCENE")
	}
	for _, category := range event.CategoryPath {
		if category.ID == 0 || category.Code == "" {
			return errors.New("category snapshot is incomplete")
		}
	}
	if event.CategoryID != event.CategoryPath[2].ID || event.CategoryCode != event.CategoryPath[2].Code {
		return errors.New("scene snapshot is inconsistent")
	}
	if event.CountUV && event.UVMode != tracking.Exact && event.UVMode != tracking.Approximate {
		return errors.New("UV mode is invalid")
	}
	return nil
}

func opaqueSession(master []byte, projectID uint64, sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(strconv.FormatUint(projectID, 10)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (w *Worker) Run(ctx context.Context) error {
	if err := w.validate(); err != nil {
		return err
	}
	sem := make(chan struct{}, w.cfg.Concurrency)
	launch := func(msg ports.Message) bool {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return false
		}
		go func() { defer func() { <-sem }(); _ = w.Handle(ctx, msg) }()
		return true
	}
	drain := func() {
		if len(sem) == 0 {
			return
		}
		timer := time.NewTimer(w.cfg.DrainTimeout)
		defer timer.Stop()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			if len(sem) == 0 {
				return
			}
			select {
			case <-ticker.C:
			case <-timer.C:
				return
			}
		}
	}
	streams := []string{w.cfg.EventStream, w.cfg.InvalidStream}
	reclaim := func() error {
		for _, stream := range streams {
			messages, err := w.queue.ReclaimStale(ctx, stream, w.cfg.Group, w.cfg.Consumer, w.cfg.ReclaimIdle, workerReadCount)
			if err != nil {
				return fmt.Errorf("reclaim stale messages from %q: %w", stream, err)
			}
			for _, msg := range messages {
				if !launch(msg) {
					return ctx.Err()
				}
			}
		}
		return nil
	}
	if err := reclaim(); err != nil {
		if ctx.Err() != nil {
			drain()
			return nil
		}
		drain()
		return err
	}
	reclaimEvery := w.cfg.ReclaimIdle / 2
	if reclaimEvery <= 0 {
		reclaimEvery = time.Nanosecond
	}
	if reclaimEvery > 30*time.Second {
		reclaimEvery = 30 * time.Second
	}
	nextReclaim := time.Now().Add(reclaimEvery)
	for {
		if !time.Now().Before(nextReclaim) {
			if err := reclaim(); err != nil {
				if ctx.Err() != nil {
					drain()
					return nil
				}
				drain()
				return err
			}
			nextReclaim = time.Now().Add(reclaimEvery)
		}
		for _, stream := range streams {
			if ctx.Err() != nil {
				drain()
				return nil
			}
			messages, err := w.queue.Consume(ctx, stream, w.cfg.Group, w.cfg.Consumer, workerReadCount, workerReadBlock)
			if err != nil {
				if ctx.Err() != nil {
					drain()
					return nil
				}
				drain()
				return fmt.Errorf("consume messages from %q: %w", stream, err)
			}
			for _, msg := range messages {
				if !launch(msg) {
					drain()
					return nil
				}
			}
		}
	}
}
