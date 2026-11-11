package driver

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"

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
		return backend.ShareMount{}, errors.New("exportLocation is required")
	}
	return backend.ShareMount{Source: source, FSType: "nfs", Options: []string{"vers=3", "nolock"}}, nil
}

// fakeShareService implements only the core backend.Service.
type fakeShareService struct {
	createRes *backend.Volume
	expandRes int64
}

func (f *fakeShareService) CreateVolume(context.Context, backend.CreateVolumeRequest) (*backend.Volume, error) {
	return f.createRes, nil
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
