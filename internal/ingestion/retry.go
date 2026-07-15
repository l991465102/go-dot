package ingestion

import (
	"context"
	"fmt"

	"github.com/lzw7758/go-dot/internal/ports"
)

func (w *Worker) finishFailure(ctx context.Context, msg ports.Message, cause error) error {
	if isPermanent(cause) || msg.Deliveries >= w.cfg.MaxDeliveries {
		if err := w.queue.MoveToDLQ(ctx, msg.Stream, w.cfg.Group, msg, cause); err != nil {
			return fmt.Errorf("move failed message to DLQ: %w", err)
		}
		// MoveToDLQ atomically acknowledges the source message. Calling Ack here
		// would be both redundant and incorrect for queue implementations.
		return nil
	}
	if err := w.queue.Retry(ctx, msg.Stream, w.cfg.Group, msg, cause); err != nil {
		return fmt.Errorf("schedule message retry after %v: %w", cause, err)
	}
	return cause
}
