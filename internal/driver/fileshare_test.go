package driver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

type fakeShareDriver struct{}

func (fakeShareDriver) Name() string        { return "csi.share.test" }
func (fakeShareDriver) TopologyKey() string { return "" }

func (fakeShareDriver) ValidateVolumeCapability(capability *csi.VolumeCapability) error {
	if capability == nil || capability.GetAccessMode() == nil {
		return errors.New("access mode is required")
	}
	return nil
}

func (fakeShareDriver) BuildCreateVolumeRequest(_ config.Config, req *csi.CreateVolumeRequest) (backend.CreateVolumeRequest, error) {
	return backend.CreateVolumeRequest{Name: req.GetName(), SizeBytes: req.GetCapacityRange().GetRequiredBytes()}, nil
}

func (fakeShareDriver) VolumeContext(vol *backend.Volume) map[string]string {
	return map[string]string{"exportLocation": vol.ExportLocation}
}

func (fakeShareDriver) ControllerCapabilities() []csi.ControllerServiceCapability_RPC_Type {
	return []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	}
}

func (fakeShareDriver) NodeCapabilities() []csi.NodeServiceCapability_RPC_Type {
	return []csi.NodeServiceCapability_RPC_Type{csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME}
}

func (fakeShareDriver) ShareMount(_ string, volumeContext map[string]string) (backend.ShareMount, error) {
	source := volumeContext["exportLocation"]
	if source == "" {
		return backend.ShareMount{}, fmt.Errorf("exportLocation is required: %w", backend.ErrExportLocationUnknown)
	}
	return backend.ShareMount{Source: source, FSType: "nfs", Options: []string{"vers=3", "nolock"}}, nil
}

// fakeShareService implements only the core backend.Service.
type fakeShareService struct {
	createRes *backend.Volume
	createErr error
	expandRes int64
}

func (f *fakeShareService) CreateVolume(context.Context, backend.CreateVolumeRequest) (*backend.Volume, error) {
	return f.createRes, f.createErr
}

func (f *fakeShareService) DeleteVolume(context.Context, string) error { return nil }

func (f *fakeShareService) ExpandVolume(context.Context, string, int64) (int64, error) {
	return f.expandRes, nil
}

type failingZoneResolver struct{}

func (failingZoneResolver) Resolve(context.Context) (string, error) {
	return "", errors.New("zone resolver must not be called")
}

func multiNodeMountCapability(flags ...string) *csi.VolumeCapability {
	return &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{MountFlags: flags}},
		AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER},
	}
}

func newShareNodeServer(mounter *fakeMounter, deviceManager *fakeDeviceManager) *nodeServer {
	return &nodeServer{
		driver:         fakeShareDriver{},
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		zoneResolver:   failingZoneResolver{},
		deviceResolver: &fakeDeviceResolver{err: errors.New("device resolver must not be called")},
		mounter:        mounter,
		deviceManager:  deviceManager,
	}
}

func TestValidateAcceptsFileShareBackend(t *testing.T) {
	if err := backend.Validate(fakeShareDriver{}, &fakeShareService{}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestNodeStageVolumeMountsFileShare(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{}}
	deviceManager := &fakeDeviceManager{}
	server := newShareNodeServer(mounter, deviceManager)

	_, err := server.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
		VolumeId:          "share-1",
		StagingTargetPath: "/staging/share-1",
		VolumeContext:     map[string]string{"exportLocation": "10.0.0.5:/share-1"},
		VolumeCapability:  multiNodeMountCapability("timeo=600"),
	})
	if err != nil {
		t.Fatalf("NodeStageVolume: %v", err)
	}

	if len(mounter.mountCalls) != 1 {
		t.Fatalf("expected one mount call, got %d", len(mounter.mountCalls))
	}
	call := mounter.mountCalls[0]
	if call.source != "10.0.0.5:/share-1" || call.target != "/staging/share-1" || call.fsType != "nfs" {
		t.Fatalf("unexpected mount call: %+v", call)
	}
	if want := []string{"vers=3", "nolock", "timeo=600"}; !slices.Equal(call.options, want) {
		t.Fatalf("unexpected mount options: %v, want %v", call.options, want)
	}
	if deviceManager.ensureFormattedDevice != "" {
		t.Fatalf("file share must not be formatted, got %q", deviceManager.ensureFormattedDevice)
	}
}

func TestNodeStageVolumeFileShareIsIdempotent(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{"/staging/share-1": true}}
	server := newShareNodeServer(mounter, &fakeDeviceManager{})

	_, err := server.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
		VolumeId:          "share-1",
		StagingTargetPath: "/staging/share-1",
		VolumeContext:     map[string]string{"exportLocation": "10.0.0.5:/share-1"},
		VolumeCapability:  multiNodeMountCapability(),
	})
	if err != nil {
		t.Fatalf("NodeStageVolume: %v", err)
	}
	if len(mounter.mountCalls) != 0 {
		t.Fatalf("expected no mount calls, got %+v", mounter.mountCalls)
	}
}

func TestNodeStageVolumeFileShareRejectsInvalidRequests(t *testing.T) {
	tests := map[string]*csi.NodeStageVolumeRequest{
		"block access": {
			VolumeId:          "share-1",
			StagingTargetPath: "/staging/share-1",
			VolumeContext:     map[string]string{"exportLocation": "10.0.0.5:/share-1"},
			VolumeCapability: &csi.VolumeCapability{
				AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}},
				AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER},
			},
		},
		"missing export location": {
			VolumeId:          "share-1",
			StagingTargetPath: "/staging/share-1",
			VolumeCapability:  multiNodeMountCapability(),
		},
	}
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			mounter := &fakeMounter{mounted: map[string]bool{}}
			_, err := newShareNodeServer(mounter, &fakeDeviceManager{}).NodeStageVolume(context.Background(), req)
			assertCode(t, err, codes.InvalidArgument)
			if len(mounter.mountCalls) != 0 {
				t.Fatalf("expected no mount calls, got %+v", mounter.mountCalls)
			}
		})
	}
}

func TestNodeExpandVolumeRejectsFileShare(t *testing.T) {
	server := newShareNodeServer(&fakeMounter{}, &fakeDeviceManager{})

	_, err := server.NodeExpandVolume(context.Background(), &csi.NodeExpandVolumeRequest{
		VolumeId:         "share-1",
		VolumePath:       "/pods/share-1",
		CapacityRange:    &csi.CapacityRange{RequiredBytes: 1 << 30},
		VolumeCapability: multiNodeMountCapability(),
	})
	assertCode(t, err, codes.Unimplemented)
}

func TestNodeGetInfoForFileShareOmitsTopologyAndVolumeLimit(t *testing.T) {
	server := newShareNodeServer(&fakeMounter{}, &fakeDeviceManager{})
	server.cfg = config.Config{MaxVolumesPerNode: 32}

	resp, err := server.NodeGetInfo(context.Background(), &csi.NodeGetInfoRequest{})
	if err != nil {
		t.Fatalf("NodeGetInfo: %v", err)
	}
	if resp.GetNodeId() != "node-id" || resp.GetMaxVolumesPerNode() != 0 || resp.GetAccessibleTopology() != nil {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestControllerForFileShareBackend(t *testing.T) {
	server := newControllerServer(config.Config{}, fakeShareDriver{}, &fakeShareService{
		createRes: &backend.Volume{ID: "share-1", SizeBytes: 1 << 30, AvailabilityZone: "eu-de-01", ExportLocation: "10.0.0.5:/share-1"},
		expandRes: 2 << 30,
	})
	ctx := context.Background()

	created, err := server.CreateVolume(ctx, &csi.CreateVolumeRequest{
		Name:               "pvc-1",
		CapacityRange:      &csi.CapacityRange{RequiredBytes: 1 << 30},
		VolumeCapabilities: []*csi.VolumeCapability{multiNodeMountCapability()},
	})
	if err != nil {
		t.Fatalf("CreateVolume: %v", err)
	}
	if created.GetVolume().GetAccessibleTopology() != nil {
		t.Fatalf("file share must not report topology: %+v", created.GetVolume().GetAccessibleTopology())
	}
	if created.GetVolume().GetVolumeContext()["exportLocation"] != "10.0.0.5:/share-1" {
		t.Fatalf("unexpected volume context: %v", created.GetVolume().GetVolumeContext())
	}

	expanded, err := server.ControllerExpandVolume(ctx, &csi.ControllerExpandVolumeRequest{
		VolumeId:      "share-1",
		CapacityRange: &csi.CapacityRange{RequiredBytes: 2 << 30},
	})
	if err != nil {
		t.Fatalf("ControllerExpandVolume: %v", err)
	}
	if expanded.GetNodeExpansionRequired() {
		t.Fatal("file share expansion must not require node expansion")
	}

	_, err = server.ControllerPublishVolume(ctx, &csi.ControllerPublishVolumeRequest{VolumeId: "share-1", NodeId: "node-1"})
	assertCode(t, err, codes.Unimplemented)
	_, err = server.ControllerUnpublishVolume(ctx, &csi.ControllerUnpublishVolumeRequest{VolumeId: "share-1", NodeId: "node-1"})
	assertCode(t, err, codes.Unimplemented)
	_, err = server.CreateSnapshot(ctx, &csi.CreateSnapshotRequest{SourceVolumeId: "share-1", Name: "snap-1"})
	assertCode(t, err, codes.Unimplemented)
	_, err = server.ListSnapshots(ctx, &csi.ListSnapshotsRequest{})
	assertCode(t, err, codes.Unimplemented)
	_, err = server.CreateVolume(ctx, &csi.CreateVolumeRequest{
		Name:          "pvc-2",
		CapacityRange: &csi.CapacityRange{RequiredBytes: 1 << 30},
		VolumeContentSource: &csi.VolumeContentSource{Type: &csi.VolumeContentSource_Snapshot{
			Snapshot: &csi.VolumeContentSource_SnapshotSource{SnapshotId: "snap-1"},
		}},
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestIdentityCapabilitiesForFileShareBackend(t *testing.T) {
	resp, err := newIdentityServer(config.Config{}, fakeShareDriver{}).GetPluginCapabilities(context.Background(), &csi.GetPluginCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("GetPluginCapabilities: %v", err)
	}

	var services []csi.PluginCapability_Service_Type
	expansion := false
	for _, capability := range resp.GetCapabilities() {
		if service := capability.GetService(); service != nil {
			services = append(services, service.GetType())
		}
		if capability.GetVolumeExpansion() != nil {
			expansion = true
		}
	}
	if !slices.Equal(services, []csi.PluginCapability_Service_Type{csi.PluginCapability_Service_CONTROLLER_SERVICE}) {
		t.Fatalf("unexpected service capabilities: %v", services)
	}
	if !expansion {
		t.Fatal("expected online volume expansion capability")
	}
}

func TestCreateVolumeReportsAlreadyExists(t *testing.T) {
	server := newControllerServer(config.Config{}, fakeShareDriver{}, &fakeShareService{
		createErr: fmt.Errorf("share pvc-1: %w: VPC differs", backend.ErrVolumeAlreadyExists),
	})

	_, err := server.CreateVolume(context.Background(), &csi.CreateVolumeRequest{
		Name:               "pvc-1",
		CapacityRange:      &csi.CapacityRange{RequiredBytes: 1 << 30},
		VolumeCapabilities: []*csi.VolumeCapability{multiNodeMountCapability()},
	})
	assertCode(t, err, codes.AlreadyExists)
}

type fakeStatter struct {
	stats volumeStats
	err   error
}

func (f fakeStatter) Stats(string) (volumeStats, error) { return f.stats, f.err }

func TestNodeGetVolumeStats(t *testing.T) {
	stats := volumeStats{TotalBytes: 500 << 30, AvailableBytes: 400 << 30, UsedBytes: 100 << 30, TotalInodes: 1000, AvailableInodes: 900, UsedInodes: 100}
	tests := map[string]struct {
		req     *csi.NodeGetVolumeStatsRequest
		statter volumeStatter
		want    codes.Code
	}{
		"ok":                  {req: &csi.NodeGetVolumeStatsRequest{VolumeId: "share-1", VolumePath: "/pods/share-1"}, statter: fakeStatter{stats: stats}, want: codes.OK},
		"missing volume id":   {req: &csi.NodeGetVolumeStatsRequest{VolumePath: "/pods/share-1"}, statter: fakeStatter{stats: stats}, want: codes.InvalidArgument},
		"missing volume path": {req: &csi.NodeGetVolumeStatsRequest{VolumeId: "share-1"}, statter: fakeStatter{stats: stats}, want: codes.InvalidArgument},
		"path does not exist": {req: &csi.NodeGetVolumeStatsRequest{VolumeId: "share-1", VolumePath: "/pods/share-1"}, statter: fakeStatter{err: fs.ErrNotExist}, want: codes.NotFound},
		"statfs fails":        {req: &csi.NodeGetVolumeStatsRequest{VolumeId: "share-1", VolumePath: "/pods/share-1"}, statter: fakeStatter{err: errors.New("stale file handle")}, want: codes.Internal},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := newShareNodeServer(&fakeMounter{}, &fakeDeviceManager{})
			server.statter = tt.statter

			resp, err := server.NodeGetVolumeStats(context.Background(), tt.req)
			if tt.want != codes.OK {
				assertCode(t, err, tt.want)
				return
			}
			if err != nil {
				t.Fatalf("NodeGetVolumeStats: %v", err)
			}
			want := []*csi.VolumeUsage{
				{Unit: csi.VolumeUsage_BYTES, Total: 500 << 30, Available: 400 << 30, Used: 100 << 30},
				{Unit: csi.VolumeUsage_INODES, Total: 1000, Available: 900, Used: 100},
			}
			if len(resp.GetUsage()) != len(want) {
				t.Fatalf("usage = %v, want %v", resp.GetUsage(), want)
			}
			for i := range want {
				if !proto.Equal(resp.GetUsage()[i], want[i]) {
					t.Fatalf("usage[%d] = %v, want %v", i, resp.GetUsage()[i], want[i])
				}
			}
		})
	}
}

func TestOSVolumeStatter(t *testing.T) {
	stats, err := osVolumeStatter{}.Stats(t.TempDir())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalBytes <= 0 || stats.UsedBytes < 0 || stats.AvailableBytes > stats.TotalBytes {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if _, err := (osVolumeStatter{}).Stats(t.TempDir() + "/missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stats(missing) error = %v, want fs.ErrNotExist", err)
	}
}

// listingShareDriver advertises the get volume and volume condition capabilities.
type listingShareDriver struct{ fakeShareDriver }

func (listingShareDriver) ControllerCapabilities() []csi.ControllerServiceCapability_RPC_Type {
	return []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_GET_VOLUME,
		csi.ControllerServiceCapability_RPC_VOLUME_CONDITION,
	}
}

// listingShareService adds VolumeGetter to fakeShareService.
type listingShareService struct {
	fakeShareService
	volumes   []*backend.Volume
	deleteErr error
}

func (f *listingShareService) DeleteVolume(context.Context, string) error { return f.deleteErr }

func (f *listingShareService) GetVolume(_ context.Context, volumeID string) (*backend.Volume, error) {
	for _, vol := range f.volumes {
		if vol.ID == volumeID {
			return vol, nil
		}
	}
	return nil, fmt.Errorf("volume %s: %w", volumeID, backend.ErrVolumeNotFound)
}

func listingService() *listingShareService {
	return &listingShareService{volumes: []*backend.Volume{
		{ID: "share-1", SizeBytes: 1, ExportLocation: "10.0.0.5:/", Condition: backend.VolumeCondition{Message: "available"}},
		{ID: "share-2", SizeBytes: 2, ExportLocation: "10.0.0.6:/", Condition: backend.VolumeCondition{Abnormal: true, Message: "frozen"}},
		{ID: "share-3", SizeBytes: 3, ExportLocation: "10.0.0.7:/"},
	}}
}

func TestValidateAcceptsListingFileShareBackend(t *testing.T) {
	if err := backend.Validate(listingShareDriver{}, listingService()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestControllerGetVolume(t *testing.T) {
	server := newControllerServer(config.Config{}, listingShareDriver{}, listingService())

	resp, err := server.ControllerGetVolume(context.Background(), &csi.ControllerGetVolumeRequest{VolumeId: "share-2"})
	if err != nil {
		t.Fatalf("ControllerGetVolume: %v", err)
	}
	if resp.GetVolume().GetVolumeContext()["exportLocation"] != "10.0.0.6:/" || !resp.GetStatus().GetVolumeCondition().GetAbnormal() {
		t.Fatalf("unexpected response: %+v", resp)
	}

	_, err = server.ControllerGetVolume(context.Background(), &csi.ControllerGetVolumeRequest{VolumeId: "missing"})
	assertCode(t, err, codes.NotFound)

	_, err = newControllerServer(config.Config{}, fakeShareDriver{}, &fakeShareService{}).ControllerGetVolume(context.Background(), &csi.ControllerGetVolumeRequest{VolumeId: "share-1"})
	assertCode(t, err, codes.Unimplemented)
}

func TestDeleteVolumeRefusesUnsupportedVolume(t *testing.T) {
	service := listingService()
	service.deleteErr = fmt.Errorf("subdirectory: %w", backend.ErrVolumeOperationNotSupported)
	_, err := newControllerServer(config.Config{}, listingShareDriver{}, service).DeleteVolume(context.Background(), &csi.DeleteVolumeRequest{VolumeId: "share-1:data"})
	assertCode(t, err, codes.FailedPrecondition)
}

func TestNodeStageVolumeLooksUpExportLocation(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{}}
	server := newShareNodeServer(mounter, &fakeDeviceManager{})
	server.volumeGetter = listingService()

	_, err := server.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
		VolumeId:          "share-2",
		StagingTargetPath: "/staging/share-2",
		VolumeCapability:  multiNodeMountCapability(),
	})
	if err != nil {
		t.Fatalf("NodeStageVolume: %v", err)
	}
	if len(mounter.mountCalls) != 1 || mounter.mountCalls[0].source != "10.0.0.6:/" {
		t.Fatalf("unexpected mount calls: %+v", mounter.mountCalls)
	}

	_, err = server.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
		VolumeId:          "missing",
		StagingTargetPath: "/staging/missing",
		VolumeCapability:  multiNodeMountCapability(),
	})
	assertCode(t, err, codes.NotFound)
}
