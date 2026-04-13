package driver

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"google.golang.org/grpc"

	"t-cloud-public-csi-driver/internal/cloud/evs"
	"t-cloud-public-csi-driver/internal/config"
)

type Driver struct {
	cfg        config.Config
	service    *evs.Service
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

	service, err := evs.NewService(cfg, authOpts)
	if err != nil {
		return nil, err
	}

	return &Driver{
		cfg:     cfg,
		service: service,
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
	defer listener.Close()

	server := grpc.NewServer()
	d.grpcServer = server

	csi.RegisterIdentityServer(server, newIdentityServer(d.cfg))
	csi.RegisterControllerServer(server, newControllerServer(d.cfg, d.service))
	csi.RegisterNodeServer(server, newNodeServer(d.cfg))

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		server.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}
