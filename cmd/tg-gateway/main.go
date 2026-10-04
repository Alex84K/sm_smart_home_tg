// Package main is the entrypoint for the Telegram gateway service.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/Alex84K/sm_smart_home_tg/internal/platform/config"
	"github.com/Alex84K/sm_smart_home_tg/internal/platform/log"
	"github.com/Alex84K/sm_smart_home_tg/internal/platform/run"
	tgapp "github.com/Alex84K/sm_smart_home_tg/internal/telegram/app"
)

func main() {
	cfg, err := config.LoadTGConfig("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		os.Exit(1)
	}

	logger := log.New(cfg.LogLevel, os.Stdout)
	logger.Info("starting telegram gateway")

	app, err := tgapp.New(cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Initialization error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := run.SignalContext()
	defer cancel()

	group := run.NewGroup(app)
	if err := group.Run(ctx); err != nil {
		logger.Error("telegram gateway terminated with error", slog.Any("err", err))
		os.Exit(1)
	}

	logger.Info("telegram gateway stopped cleanly")
}
