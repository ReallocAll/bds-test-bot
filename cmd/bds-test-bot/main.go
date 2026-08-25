package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ReallocAll/bds-test-bot/internal/bot"
	"github.com/ReallocAll/bds-test-bot/internal/output"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := bot.ParseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "bds-test-bot:", err)
		return bot.ExitArgs
	}

	emitter := output.New(os.Stdout, cfg.JSON)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := bot.Run(ctx, cfg, emitter); err != nil {
		var stageErr *bot.StageError
		if errors.As(err, &stageErr) {
			_ = emitter.Emit("error", map[string]any{"stage": stageErr.Stage, "message": stageErr.Err.Error()})
			return stageErr.Code
		}
		_ = emitter.Emit("error", map[string]any{"stage": "runtime", "message": err.Error()})
		return bot.ExitRuntime
	}
	return 0
}
