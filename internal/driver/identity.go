package driver

import (
	"context"
	"slices"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

type identityServer struct {
	csi.UnimplementedIdentityServer
	cfg    config.Config
	driver backend.Driver
}

func newIdentityServer(cfg config.Config, driver backend.Driver) *identityServer {
	return &identityServer{cfg: cfg, driver: driver}
}

func (s *identityServer) GetPluginInfo(context.Context, *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          s.cfg.DriverName,
		VendorVersion: "0.1.0",
	}, nil
}

func (s *identityServer) GetPluginCapabilities(context.Context, *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	capabilities := []*csi.PluginCapability{
		{
			Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{
					Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
				},
			},
		},
	}
	if s.driver.TopologyKey() != "" {
		capabilities = append(capabilities, &csi.PluginCapability{
			Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{
					Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
				},
			},
		})
	}
	if slices.Contains(s.driver.ControllerCapabilities(), csi.ControllerServiceCapability_RPC_EXPAND_VOLUME) {
		capabilities = append(capabilities, &csi.PluginCapability{
			Type: &csi.PluginCapability_VolumeExpansion_{
				VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
					Type: csi.PluginCapability_VolumeExpansion_ONLINE,
				},
			},
		})
	}

	return &csi.GetPluginCapabilitiesResponse{Capabilities: capabilities}, nil
}

func (s *identityServer) Probe(context.Context, *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{Ready: wrapperspb.Bool(true)}, nil
}
