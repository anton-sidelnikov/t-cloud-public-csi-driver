package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"t-cloud-public-csi-driver/internal/config"
	"t-cloud-public-csi-driver/internal/driver"
	"t-cloud-public-csi-driver/internal/version"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	slog.SetDefault(logger)
	build := version.Get()
	logger.Info("starting T Cloud Public CSI driver", "version", build.Version, "commit", build.Commit, "build_date", build.Date)

	cfg, err := config.FromEnv()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	drv, err := driver.New(cfg)
	if err != nil {
		logger.Error("init driver", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := drv.Run(ctx); err != nil {
		logger.Error("run driver", "error", err)
		os.Exit(1)
	}
}
