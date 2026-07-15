package cmd

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"

	"github.com/lzw7758/go-dot/internal/adapter/httpapi"
	"github.com/lzw7758/go-dot/internal/config"
)

func runAPI(ctx context.Context) error {
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}

	server := g.Server()
	server.SetAddr(cfg.HTTP.Address)
	server.BindHandler("GET:/health/live", httpapi.SystemController{}.Live)
	server.Run()
	return nil
}
