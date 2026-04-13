package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"t-cloud-public-csi-driver/internal/config"
	"t-cloud-public-csi-driver/internal/driver"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	drv, err := driver.New(cfg)
	if err != nil {
		log.Fatalf("init driver: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := drv.Run(ctx); err != nil {
		log.Fatalf("run driver: %v", err)
	}
}
