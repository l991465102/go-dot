package cmd

import (
	"context"
	"errors"
)

const usage = "usage: go-dot <api|worker|scheduler|migrate>"

var migrateCommand = runMigrate
var workerCommand = runWorker
var schedulerCommand = runScheduler

func Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New(usage)
	}

	switch args[0] {
	case "api":
		return runAPI(ctx)
	case "migrate":
		return migrateCommand(ctx)
	case "worker":
		return workerCommand(ctx)
	case "scheduler":
		return schedulerCommand(ctx)
	default:
		return errors.New(usage)
	}
}
