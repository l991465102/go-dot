package main

import (
	"context"
	"fmt"
	"os"

	"github.com/lzw7758/go-dot/internal/cmd"
)

func main() {
	if err := cmd.Run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
