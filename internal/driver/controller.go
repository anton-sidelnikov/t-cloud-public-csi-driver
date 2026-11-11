package driver

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

type controllerServer struct {
	csi.UnimplementedControllerServer
	cfg     config.Config
	driver  backend.Driver
	service backend.Service
	logger  *slog.Logger
}

func newControllerServer(cfg config.Config, driver backend.Driver, service backend.Service) *controllerServer {
	return &controllerServer{
		cfg:     cfg,
		driver:  driver,
		service: service,
		logger:  slog.Default().With("component", "controller", "backend", cfg.Backend),
	}
}

func (s *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	logger := s.loggerOrDefault().With("volume_name", req.GetName())
	logger.Info("create volume requested")

	req = cloneCreateVolumeRequest(req)
	if source := req.GetVolumeContentSource(); source != nil && source.GetSnapshot() != nil {
		snapshotter, ok := s.snapshotter()
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "backend does not support creating volumes from snapshots")
		}
		snapshotID := source.GetSnapshot().GetSnapshotId()
		snapshot, err := snapshotter.GetSnapshot(ctx, snapshotID)
		if err != nil {
			logger.Error("get source snapshot failed", "snapshot_id", snapshotID, "error", err)
			return nil, status.Errorf(codes.Internal, "get source snapshot: %v", err)
		}
		if snapshot == nil {
			return nil, status.Errorf(codes.NotFound, "source snapshot %q was not found", snapshotID)
		}
		if req.GetCapacityRange() == nil || req.GetCapacityRange().GetRequiredBytes() <= 0 {
			req.CapacityRange = &csi.CapacityRange{RequiredBytes: snapshot.SizeBytes}
		} else if req.GetCapacityRange().GetRequiredBytes() < snapshot.SizeBytes {
			return nil, status.Errorf(codes.InvalidArgument, "requested capacity %d must be at least snapshot size %d", req.GetCapacityRange().GetRequiredBytes(), snapshot.SizeBytes)
		}
	}

	createReq, err := s.driver.BuildCreateVolumeRequest(s.cfg, req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "create volume request: %v", err)
	}
	logger = logger.With("availability_zone", createReq.AvailabilityZone, "volume_type", createReq.VolumeType, "size_bytes", createReq.SizeBytes)

	vol, err := s.service.CreateVolume(ctx, createReq)
	if err != nil {
		logger.Error("create volume failed", "error", err)
		return nil, status.Errorf(codes.Internal, "create volume: %v", err)
	}
	logger.Info("create volume completed", "volume_id", vol.ID)

	return &csi.CreateVolumeResponse{
		Volume: toCSIVolume(s.driver, vol, req.GetVolumeContentSource()),
	}, nil
}

func (s *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	logger := s.loggerOrDefault().With("volume_id", req.GetVolumeId())
	logger.Info("delete volume requested")

	if err := s.service.DeleteVolume(ctx, req.GetVolumeId()); err != nil {
		logger.Error("delete volume failed", "error", err)
		return nil, status.Errorf(codes.Internal, "delete volume: %v", err)
	}
	logger.Info("delete volume completed")

	return &csi.DeleteVolumeResponse{}, nil
}

func (s *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if req.GetVolumeId() == "" || req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id and node_id are required")
	}
	attacher, blockDriver, ok := s.attacher()
	if !ok {
		return nil, status.Error(codes.Unimplemented, "backend does not attach volumes to nodes")
	}
	logger := s.loggerOrDefault().With("volume_id", req.GetVolumeId(), "node_id", req.GetNodeId())
	logger.Info("controller publish requested")

	attachment, err := attacher.AttachVolume(ctx, req.GetVolumeId(), req.GetNodeId())
	if err != nil {
		logger.Error("controller publish failed", "error", err)
		return nil, status.Errorf(codes.Internal, "attach volume: %v", err)
	}
	logger.Info("controller publish completed", "attachment_id", attachment.ID, "device", attachment.Device)

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: blockDriver.PublishContext(attachment),
	}, nil
}

func (s *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if req.GetVolumeId() == "" || req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id and node_id are required")
	}
	attacher, _, ok := s.attacher()
	if !ok {
		return nil, status.Error(codes.Unimplemented, "backend does not attach volumes to nodes")
	}
	logger := s.loggerOrDefault().With("volume_id", req.GetVolumeId(), "node_id", req.GetNodeId())
	logger.Info("controller unpublish requested")

	if err := attacher.DetachVolume(ctx, req.GetVolumeId(), req.GetNodeId()); err != nil {
		logger.Error("controller unpublish failed", "error", err)
		return nil, status.Errorf(codes.Internal, "detach volume: %v", err)
	}
	logger.Info("controller unpublish completed")

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
	logger := s.loggerOrDefault().With("volume_id", req.GetVolumeId(), "requested_bytes", req.GetCapacityRange().GetRequiredBytes())
	logger.Info("controller expand requested")

	size, err := s.service.ExpandVolume(ctx, req.GetVolumeId(), req.GetCapacityRange().GetRequiredBytes())
	if err != nil {
		logger.Error("controller expand failed", "error", err)
		return nil, status.Errorf(codes.Internal, "expand volume: %v", err)
	}
	logger.Info("controller expand completed", "capacity_bytes", size)

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         size,
		NodeExpansionRequired: slices.Contains(s.driver.NodeCapabilities(), csi.NodeServiceCapability_RPC_EXPAND_VOLUME),
	}, nil
}

func (s *controllerServer) attacher() (backend.Attacher, backend.BlockDriver, bool) {
	attacher, ok := s.service.(backend.Attacher)
	if !ok {
		return nil, nil, false
	}
	blockDriver, ok := s.driver.(backend.BlockDriver)
	return attacher, blockDriver, ok
}

func (s *controllerServer) snapshotter() (backend.Snapshotter, bool) {
	snapshotter, ok := s.service.(backend.Snapshotter)
	return snapshotter, ok
}

func (s *controllerServer) loggerOrDefault() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default().With("component", "controller")
}

func (s *controllerServer) GetCapacity(context.Context, *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetCapacity is not implemented yet")
}

func (s *controllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	if req.GetSourceVolumeId() == "" || req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "source_volume_id and name are required")
	}

	snapshotter, ok := s.snapshotter()
	if !ok {
		return nil, status.Error(codes.Unimplemented, "backend does not support snapshots")
	}
	logger := s.loggerOrDefault().With("source_volume_id", req.GetSourceVolumeId(), "snapshot_name", req.GetName())
	logger.Info("create snapshot requested")

	existing, err := snapshotter.ListSnapshots(ctx, backend.ListSnapshotsRequest{
		Name:           req.GetName(),
		SourceVolumeID: req.GetSourceVolumeId(),
	})
	if err != nil {
		logger.Error("list snapshots for idempotency failed", "error", err)
		return nil, status.Errorf(codes.Internal, "list snapshots: %v", err)
	}
	if len(existing) > 0 {
		logger.Info("create snapshot returning existing snapshot", "snapshot_id", existing[0].ID)
		return &csi.CreateSnapshotResponse{Snapshot: toCSISnapshot(existing[0])}, nil
	}

	snapshot, err := snapshotter.CreateSnapshot(ctx, req.GetSourceVolumeId(), req.GetName())
	if err != nil {
		logger.Error("create snapshot failed", "error", err)
		return nil, status.Errorf(codes.Internal, "create snapshot: %v", err)
	}
	logger.Info("create snapshot completed", "snapshot_id", snapshot.ID)

	return &csi.CreateSnapshotResponse{Snapshot: toCSISnapshot(snapshot)}, nil
}

func (s *controllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	if req.GetSnapshotId() == "" {
		return nil, status.Error(codes.InvalidArgument, "snapshot_id is required")
	}

	snapshotter, ok := s.snapshotter()
	if !ok {
		return nil, status.Error(codes.Unimplemented, "backend does not support snapshots")
	}
	logger := s.loggerOrDefault().With("snapshot_id", req.GetSnapshotId())
	logger.Info("delete snapshot requested")

	if err := snapshotter.DeleteSnapshot(ctx, req.GetSnapshotId()); err != nil {
		logger.Error("delete snapshot failed", "error", err)
		return nil, status.Errorf(codes.Internal, "delete snapshot: %v", err)
	}
	logger.Info("delete snapshot completed")

	return &csi.DeleteSnapshotResponse{}, nil
}

func (s *controllerServer) ListVolumes(context.Context, *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListVolumes is not implemented yet")
}

func (s *controllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	snapshotter, ok := s.snapshotter()
	if !ok {
		return nil, status.Error(codes.Unimplemented, "backend does not support snapshots")
	}
	logger := s.loggerOrDefault().With("snapshot_id", req.GetSnapshotId(), "source_volume_id", req.GetSourceVolumeId(), "max_entries", req.GetMaxEntries(), "starting_token", req.GetStartingToken())
	logger.Info("list snapshots requested")

	items, err := snapshotter.ListSnapshots(ctx, backend.ListSnapshotsRequest{
		ID:             req.GetSnapshotId(),
		SourceVolumeID: req.GetSourceVolumeId(),
	})
	if err != nil {
		logger.Error("list snapshots failed", "error", err)
		return nil, status.Errorf(codes.Internal, "list snapshots: %v", err)
	}

	start, err := parseStartingToken(req.GetStartingToken(), len(items))
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "invalid starting token: %v", err)
	}

	end := len(items)
	nextToken := ""
	if req.GetMaxEntries() > 0 && start+int(req.GetMaxEntries()) < end {
		end = start + int(req.GetMaxEntries())
		nextToken = strconv.Itoa(end)
	}

	entries := make([]*csi.ListSnapshotsResponse_Entry, 0, max(end-start, 0))
	for _, snapshot := range items[start:end] {
		entries = append(entries, &csi.ListSnapshotsResponse_Entry{Snapshot: toCSISnapshot(snapshot)})
	}

	logger.Info("list snapshots completed", "entries", len(entries), "next_token", nextToken)
	return &csi.ListSnapshotsResponse{Entries: entries, NextToken: nextToken}, nil
}

func (s *controllerServer) ControllerGetVolume(context.Context, *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ControllerGetVolume is not implemented yet")
}

func toCSIVolume(driver backend.Driver, vol *backend.Volume, source *csi.VolumeContentSource) *csi.Volume {
	volume := &csi.Volume{
		CapacityBytes: vol.SizeBytes,
		VolumeId:      vol.ID,
		VolumeContext: driver.VolumeContext(vol),
		ContentSource: source,
	}
	if key := driver.TopologyKey(); key != "" && vol.AvailabilityZone != "" {
		volume.AccessibleTopology = []*csi.Topology{{Segments: map[string]string{key: vol.AvailabilityZone}}}
	}
	return volume
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
	for _, c := range caps {
		result = append(result, controllerCapability(c))
	}
	return result
}

func toCSISnapshot(snapshot *backend.Snapshot) *csi.Snapshot {
	if snapshot == nil {
		return nil
	}

	var createdAt *timestamppb.Timestamp
	if !snapshot.CreatedAt.IsZero() {
		createdAt = timestamppb.New(snapshot.CreatedAt)
	}

	return &csi.Snapshot{
		SizeBytes:      snapshot.SizeBytes,
		SnapshotId:     snapshot.ID,
		SourceVolumeId: snapshot.SourceVolumeID,
		ReadyToUse:     snapshot.ReadyToUse,
		CreationTime:   createdAt,
	}
}

func cloneCreateVolumeRequest(req *csi.CreateVolumeRequest) *csi.CreateVolumeRequest {
	if req == nil {
		return nil
	}

	cloned := &csi.CreateVolumeRequest{
		Name:                      req.GetName(),
		Parameters:                req.GetParameters(),
		Secrets:                   req.GetSecrets(),
		VolumeCapabilities:        req.GetVolumeCapabilities(),
		AccessibilityRequirements: req.GetAccessibilityRequirements(),
		MutableParameters:         req.GetMutableParameters(),
		VolumeContentSource:       req.GetVolumeContentSource(),
	}
	if req.CapacityRange != nil {
		cloned.CapacityRange = &csi.CapacityRange{
			RequiredBytes: req.GetCapacityRange().GetRequiredBytes(),
			LimitBytes:    req.GetCapacityRange().GetLimitBytes(),
		}
	}
	return cloned
}

func parseStartingToken(token string, length int) (int, error) {
	if token == "" {
		return 0, nil
	}

	start, err := strconv.Atoi(token)
	if err != nil {
		return 0, err
	}
	if start < 0 || start > length {
		return 0, fmt.Errorf("starting token out of range")
	}

	return start, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
