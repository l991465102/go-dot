package cmd

import (
	"context"
	"fmt"
	"net/http"

	"github.com/lzw7758/go-dot/internal/observability"
)

func runWithMetrics(ctx context.Context, address string, handler http.Handler, run func(context.Context) error) error {
	server, err := observability.StartMetricsServer(address, handler)
	if err != nil {
		return fmt.Errorf("start metrics server: %w", err)
	}
	defer server.Close()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- run(runCtx) }()

	select {
	case err := <-result:
		return err
	case err := <-server.Errors():
		if err != nil {
			return fmt.Errorf("serve metrics: %w", err)
		}
		return nil
	case <-ctx.Done():
		cancel()
		return <-result
	}
}
