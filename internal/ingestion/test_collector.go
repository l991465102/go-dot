package ingestion

import (
	"context"
	"strings"
	"time"

	"github.com/lzw7758/go-dot/internal/project"
	"github.com/lzw7758/go-dot/internal/tracking"
)

// TestRecord 仅保存验收所需元数据，不保存身份和属性值。
type TestRecord struct {
	PointCode  string    `json:"point_code"`
	Platform   string    `json:"platform"`
	AppVersion string    `json:"app_version"`
	Status     string    `json:"status"`
	Code       string    `json:"code"`
	ReceivedAt time.Time `json:"received_at"`
}
type TestSink interface {
	SaveTestRecords(context.Context, uint64, []TestRecord) error
}
type TestCollector struct {
	collector *Collector
	sink      TestSink
}

// NewTestCollector 创建不进入正式队列的测试采集器。
func NewTestCollector(collector *Collector, sink TestSink) *TestCollector {
	return &TestCollector{collector: collector, sink: sink}
}

// Collect 执行与正式上报相同的校验并记录脱敏结果，不写入正式统计链路。
func (c *TestCollector) Collect(ctx context.Context, p project.Project, req CollectRequest) (CollectResult, error) {
	if p.ID == 0 || !p.Active || len(req.Events) < 1 || len(req.Events) > c.collector.cfg.MaxEvents {
		return CollectResult{}, ErrInvalidBatch
	}
	now := c.collector.cfg.Now().UTC()
	batchID, err := stableBatchID(p.ID, req.Events)
	if err != nil {
		return CollectResult{}, err
	}
	result := CollectResult{BatchID: batchID, Results: make([]EventResult, 0, len(req.Events))}
	records := make([]TestRecord, 0, len(req.Events))
	for i, event := range req.Events {
		_, err = c.collector.normalize(ctx, p, now, event)
		item := EventResult{Index: i, EventID: event.EventID, Status: "accepted"}
		record := TestRecord{PointCode: bounded(event.PointCode, 128), Platform: bounded(event.Platform, 32), AppVersion: bounded(event.AppVersion, 128), Status: "accepted", ReceivedAt: now}
		if err != nil {
			if _, ok := tracking.ErrorCode(err); !ok {
				return CollectResult{}, err
			}
			item.Status = "rejected"
			item.Code, item.Message = eventError(err)
			record.Status = "rejected"
			record.Code = item.Code
			result.Rejected++
		} else {
			result.Accepted++
		}
		result.Results = append(result.Results, item)
		records = append(records, record)
	}
	if err = c.sink.SaveTestRecords(ctx, p.ID, records); err != nil {
		return CollectResult{}, err
	}
	return result, nil
}
func bounded(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
