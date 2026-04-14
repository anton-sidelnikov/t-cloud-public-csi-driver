package driver

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

type nodeServer struct {
	csi.UnimplementedNodeServer
	cfg            config.Config
	driver         backend.Driver
	nodeIDResolver nodeIDResolver
	deviceResolver devicePathResolver
	mounter        nodeMounter
	deviceManager  nodeDeviceManager
}

func newNodeServer(cfg config.Config, driver backend.Driver) *nodeServer {
	runner := &execRunner{}
	return &nodeServer{
		cfg:            cfg,
		driver:         driver,
		nodeIDResolver: newNodeIDResolver(cfg),
		deviceResolver: newDevicePathResolver(cfg),
		mounter:        &osMounter{},
		deviceManager:  newFilesystemManager(runner),
	}
}

func (s *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	if req.GetStagingTargetPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "staging_target_path is required")
	}
	if err := s.driver.ValidateVolumeCapability(req.GetVolumeCapability()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "volume_capability: %v", err)
	}
	if req.GetVolumeCapability().GetBlock() != nil {
		return &csi.NodeStageVolumeResponse{}, nil
	}

	devicePath := req.GetPublishContext()[s.driver.DevicePathKey()]
	if devicePath == "" {
		return nil, status.Error(codes.InvalidArgument, "publish_context.devicePath is required")
	}
	devicePath, err := s.deviceResolver.ResolveDevicePath(ctx, req.GetVolumeId(), devicePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve device path: %v", err)
	}

	stagingPath := req.GetStagingTargetPath()
	if err := s.mounter.EnsureDir(stagingPath); err != nil {
		return nil, status.Errorf(codes.Internal, "ensure staging path: %v", err)
	}

	mounted, err := s.mounter.IsMounted(stagingPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check staging mount: %v", err)
	}
	if mounted {
		return &csi.NodeStageVolumeResponse{}, nil
	}

	fsType := defaultFSType(req.GetVolumeCapability().GetMount().GetFsType())
	if err := s.deviceManager.EnsureFormatted(ctx, devicePath, fsType); err != nil {
		return nil, status.Errorf(codes.Internal, "ensure filesystem: %v", err)
	}

	if err := s.mounter.Mount(ctx, devicePath, stagingPath, fsType, mountOptions(false, req.GetVolumeCapability().GetMount().GetMountFlags())); err != nil {
		return nil, status.Errorf(codes.Internal, "mount staged volume: %v", err)
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (s *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	if req.GetStagingTargetPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "staging_target_path is required")
	}

	if err := s.unpublishPath(ctx, req.GetStagingTargetPath()); err != nil {
		return nil, err
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (s *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	if req.GetTargetPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_path is required")
	}
	if err := s.driver.ValidateVolumeCapability(req.GetVolumeCapability()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "volume_capability: %v", err)
	}

	targetPath := req.GetTargetPath()
	mounted, err := s.mounter.IsMounted(targetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check target mount: %v", err)
	}
	if mounted {
		return &csi.NodePublishVolumeResponse{}, nil
	}

	if req.GetVolumeCapability().GetBlock() != nil {
		devicePath := req.GetPublishContext()[s.driver.DevicePathKey()]
		if devicePath == "" {
			return nil, status.Errorf(codes.InvalidArgument, "publish_context.%s is required", s.driver.DevicePathKey())
		}
		devicePath, err = s.deviceResolver.ResolveDevicePath(ctx, req.GetVolumeId(), devicePath)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "resolve device path: %v", err)
		}
		if err := s.mounter.EnsureFile(targetPath); err != nil {
			return nil, status.Errorf(codes.Internal, "ensure block target: %v", err)
		}
		if err := s.mounter.Mount(ctx, devicePath, targetPath, "", mountOptions(req.GetReadonly(), nil, "bind")); err != nil {
			return nil, status.Errorf(codes.Internal, "bind mount block device: %v", err)
		}
		return &csi.NodePublishVolumeResponse{}, nil
	}

	if req.GetStagingTargetPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "staging_target_path is required for filesystem volumes")
	}
	if err := s.mounter.EnsureDir(targetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "ensure target path: %v", err)
	}

	if err := s.mounter.Mount(ctx, req.GetStagingTargetPath(), targetPath, "", mountOptions(req.GetReadonly(), req.GetVolumeCapability().GetMount().GetMountFlags(), "bind")); err != nil {
		return nil, status.Errorf(codes.Internal, "bind mount staged volume: %v", err)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	if req.GetTargetPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_path is required")
	}

	if err := s.unpublishPath(ctx, req.GetTargetPath()); err != nil {
		return nil, err
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *nodeServer) NodeGetInfo(context.Context, *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	nodeID, err := s.nodeIDResolver.Resolve()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve node instance id: %v", err)
	}

	return &csi.NodeGetInfoResponse{
		NodeId:            nodeID,
		MaxVolumesPerNode: s.cfg.MaxVolumesPerNode,
		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				s.driver.TopologyKey(): s.cfg.AvailabilityZone,
			},
		},
	}, nil
}

func (s *nodeServer) NodeGetCapabilities(context.Context, *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: nodeCapabilities(s.driver.NodeCapabilities()),
	}, nil
}

func (s *nodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	if req.GetCapacityRange() == nil || req.GetCapacityRange().GetRequiredBytes() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "required capacity must be set")
	}
	if err := s.driver.ValidateVolumeCapability(req.GetVolumeCapability()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "volume_capability: %v", err)
	}
	if req.GetVolumeCapability().GetBlock() != nil {
		return &csi.NodeExpandVolumeResponse{CapacityBytes: req.GetCapacityRange().GetRequiredBytes()}, nil
	}

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		volumePath = req.GetStagingTargetPath()
	}
	if volumePath == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_path or staging_target_path is required")
	}

	fsType := defaultFSType(req.GetVolumeCapability().GetMount().GetFsType())
	if err := s.deviceManager.Resize(ctx, volumePath, fsType); err != nil {
		return nil, status.Errorf(codes.Internal, "resize filesystem: %v", err)
	}

	return &csi.NodeExpandVolumeResponse{CapacityBytes: req.GetCapacityRange().GetRequiredBytes()}, nil
}

func (s *nodeServer) unpublishPath(ctx context.Context, targetPath string) error {
	mounted, err := s.mounter.IsMounted(targetPath)
	if err != nil {
		return status.Errorf(codes.Internal, "check mount state: %v", err)
	}
	if mounted {
		if err := s.mounter.Unmount(ctx, targetPath); err != nil {
			return status.Errorf(codes.Internal, "unmount target: %v", err)
		}
	}
	if err := s.mounter.Remove(targetPath); err != nil {
		return status.Errorf(codes.Internal, "remove target path: %v", err)
	}
	return nil
}

func nodeCapability(cap csi.NodeServiceCapability_RPC_Type) *csi.NodeServiceCapability {
	return &csi.NodeServiceCapability{
		Type: &csi.NodeServiceCapability_Rpc{
			Rpc: &csi.NodeServiceCapability_RPC{Type: cap},
		},
	}
}

func nodeCapabilities(caps []csi.NodeServiceCapability_RPC_Type) []*csi.NodeServiceCapability {
	result := make([]*csi.NodeServiceCapability, 0, len(caps))
	for _, cap := range caps {
		result = append(result, nodeCapability(cap))
	}
	return result
}
