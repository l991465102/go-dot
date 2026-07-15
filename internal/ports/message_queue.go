package ports

import (
	"context"
	"time"
)

const DeadLetterStream = "tracking:{queue}:dead"

type Message struct {
	ID         string
	Stream     string
	BatchID    string
	Payload    []byte
	Deliveries int
	LastError  string
	Malformed  *MalformedMessage
}

type MalformedMessage struct {
	Reason      string
	RawEnvelope []byte
}

type BatchEnvelope struct {
	BatchID   string
	Payload   []byte
	CreatedAt time.Time
}

type MessageQueue interface {
	PublishBatch(ctx context.Context, stream string, batch BatchEnvelope) (string, error)
	Consume(ctx context.Context, stream, group, consumer string, count int, block time.Duration) ([]Message, error)
	Ack(ctx context.Context, stream, group string, ids ...string) error
	Retry(ctx context.Context, stream, group string, msg Message, cause error, delay time.Duration) error
	PromoteDue(ctx context.Context, stream string, count int) (int, error)
	MoveToDLQ(ctx context.Context, sourceStream, group string, msg Message, cause error) error
	ReclaimStale(ctx context.Context, stream, group, consumer string, idle time.Duration, count int) ([]Message, error)
}
