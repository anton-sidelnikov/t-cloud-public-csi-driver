package driver

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

type controllerServer struct {
	csi.UnimplementedControllerServer
	cfg     config.Config
	driver  backend.Driver
	service controllerService
}

type controllerService interface {
	CreateVolume(context.Context, backend.CreateVolumeRequest) (*backend.Volume, error)
	DeleteVolume(context.Context, string) error
	AttachVolume(context.Context, string, string) (*backend.Attachment, error)
	DetachVolume(context.Context, string, string) error
	ExpandVolume(context.Context, string, int64) (int64, error)
}

func newControllerServer(cfg config.Config, driver backend.Driver, service controllerService) *controllerServer {
	return &controllerServer{
		cfg:     cfg,
		driver:  driver,
		service: service,
	}
}

func (s *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	createReq, err := s.driver.BuildCreateVolumeRequest(s.cfg, req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "create volume request: %v", err)
	}

	vol, err := s.service.CreateVolume(ctx, createReq)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create volume: %v", err)
	}

	return &csi.CreateVolumeResponse{
		Volume: toCSIVolume(s.driver, vol),
	}, nil
}

func (s *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}

	if err := s.service.DeleteVolume(ctx, req.GetVolumeId()); err != nil {
		return nil, status.Errorf(codes.Internal, "delete volume: %v", err)
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func (s *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if req.GetVolumeId() == "" || req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id and node_id are required")
	}

	attachment, err := s.service.AttachVolume(ctx, req.GetVolumeId(), req.GetNodeId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "attach volume: %v", err)
	}

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: s.driver.PublishContext(attachment),
	}, nil
}

func (s *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if req.GetVolumeId() == "" || req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id and node_id are required")
	}

	if err := s.service.DetachVolume(ctx, req.GetVolumeId(), req.GetNodeId()); err != nil {
		return nil, status.Errorf(codes.Internal, "detach volume: %v", err)
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (s *controllerServer) ValidateVolumeCapabilities(_ context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	if len(req.GetVolumeCapabilities()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "volume_capabilities are required")
	}

	for _, capability := range req.GetVolumeCapabilities() {
		if err := s.driver.ValidateVolumeCapability(capability); err != nil {
			return &csi.ValidateVolumeCapabilitiesResponse{
				Message: err.Error(),
			}, nil
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.GetVolumeCapabilities(),
			Parameters:         req.GetParameters(),
		},
	}, nil
}

func (s *controllerServer) ControllerGetCapabilities(context.Context, *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: controllerCapabilities(s.driver.ControllerCapabilities()),
	}, nil
}

func (s *controllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	if req.GetCapacityRange() == nil || req.GetCapacityRange().GetRequiredBytes() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "required capacity must be set")
	}

	size, err := s.service.ExpandVolume(ctx, req.GetVolumeId(), req.GetCapacityRange().GetRequiredBytes())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "expand volume: %v", err)
	}

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         size,
		NodeExpansionRequired: true,
	}, nil
}

func (s *controllerServer) GetCapacity(context.Context, *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetCapacity is not implemented yet")
}

func (s *controllerServer) CreateSnapshot(context.Context, *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "CreateSnapshot is not implemented yet")
}

func (s *controllerServer) DeleteSnapshot(context.Context, *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "DeleteSnapshot is not implemented yet")
}

func (s *controllerServer) ListVolumes(context.Context, *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListVolumes is not implemented yet")
}

func (s *controllerServer) ListSnapshots(context.Context, *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListSnapshots is not implemented yet")
}

func (s *controllerServer) ControllerGetVolume(context.Context, *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ControllerGetVolume is not implemented yet")
}

func toCSIVolume(driver backend.Driver, vol *backend.Volume) *csi.Volume {
	return &csi.Volume{
		CapacityBytes: vol.SizeBytes,
		VolumeId:      vol.ID,
		AccessibleTopology: []*csi.Topology{
			{Segments: map[string]string{driver.TopologyKey(): vol.AvailabilityZone}},
		},
		VolumeContext: driver.VolumeContext(vol),
	}
}

func controllerCapability(cap csi.ControllerServiceCapability_RPC_Type) *csi.ControllerServiceCapability {
	return &csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: cap,
			},
		},
	}
}

func controllerCapabilities(caps []csi.ControllerServiceCapability_RPC_Type) []*csi.ControllerServiceCapability {
	result := make([]*csi.ControllerServiceCapability, 0, len(caps))
	for _, cap := range caps {
		result = append(result, controllerCapability(cap))
	}
	return result
}
