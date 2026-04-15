package driver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"google.golang.org/grpc"

	backendapi "t-cloud-public-csi-driver/internal/backend"
	backendevs "t-cloud-public-csi-driver/internal/backend/evs"
	"t-cloud-public-csi-driver/internal/cloud/evs"
	"t-cloud-public-csi-driver/internal/config"
)

type Driver struct {
	cfg        config.Config
	driver     backendapi.Driver
	service    backendapi.Service
	logger     *slog.Logger
	grpcServer *grpc.Server
}

func New(cfg config.Config) (*Driver, error) {
	authOpts := golangsdk.AuthOptions{
		IdentityEndpoint: cfg.AuthURL,
		Username:         cfg.UserName,
		Password:         cfg.Password,
		DomainName:       cfg.DomainName,
		AllowReauth:      true,
		TenantID:         cfg.ProjectID,
		TenantName:       cfg.ProjectName,
	}

	driver, service, err := loadBackend(cfg, authOpts)
	if err != nil {
		return nil, err
	}

	return &Driver{
		cfg:     cfg,
		driver:  driver,
		service: service,
		logger:  slog.Default().With("component", "driver", "backend", cfg.Backend, "driver_name", cfg.DriverName),
	}, nil
}

func (d *Driver) Run(ctx context.Context) error {
	endpoint := strings.TrimPrefix(d.cfg.Endpoint, "unix://")
	if err := os.Remove(endpoint); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", endpoint, err)
	}
	defer func() {
		_ = listener.Close()
	}()

	server := grpc.NewServer()
	d.grpcServer = server

	csi.RegisterIdentityServer(server, newIdentityServer(d.cfg))
	csi.RegisterControllerServer(server, newControllerServer(d.cfg, d.driver, d.service))
	csi.RegisterNodeServer(server, newNodeServer(d.cfg, d.driver))

	errCh := make(chan error, 1)
	go func() {
		d.logger.Info("starting CSI server", "endpoint", d.cfg.Endpoint)
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		d.logger.Info("stopping CSI server")
		server.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}

func loadBackend(cfg config.Config, authOpts golangsdk.AuthOptions) (backendapi.Driver, backendapi.Service, error) {
	switch cfg.Backend {
	case "evs":
		driver := backendevs.New()
		service, err := evs.NewService(cfg, authOpts)
		if err != nil {
			return nil, nil, err
		}
		return driver, service, nil
	default:
		return nil, nil, fmt.Errorf("unsupported CSI_BACKEND %q", cfg.Backend)
	}
}
