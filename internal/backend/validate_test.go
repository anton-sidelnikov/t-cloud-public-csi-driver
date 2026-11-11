package backend_test

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"t-cloud-public-csi-driver/internal/backend"
	backendevs "t-cloud-public-csi-driver/internal/backend/evs"
	"t-cloud-public-csi-driver/internal/config"
)

type coreService struct{}

func (coreService) CreateVolume(context.Context, backend.CreateVolumeRequest) (*backend.Volume, error) {
	return nil, nil
}
func (coreService) DeleteVolume(context.Context, string) error                 { return nil }
func (coreService) ExpandVolume(context.Context, string, int64) (int64, error) { return 0, nil }

type attachingService struct{ coreService }

func (attachingService) AttachVolume(context.Context, string, string) (*backend.Attachment, error) {
	return nil, nil
}
func (attachingService) DetachVolume(context.Context, string, string) error { return nil }

type fullService struct{ attachingService }

func (fullService) CreateSnapshot(context.Context, string, string) (*backend.Snapshot, error) {
	return nil, nil
}
func (fullService) DeleteSnapshot(context.Context, string) error { return nil }
func (fullService) GetSnapshot(context.Context, string) (*backend.Snapshot, error) {
	return nil, nil
}
func (fullService) ListSnapshots(context.Context, backend.ListSnapshotsRequest) ([]*backend.Snapshot, error) {
	return nil, nil
}

type testDriver struct {
	controllerCaps []csi.ControllerServiceCapability_RPC_Type
	nodeCaps       []csi.NodeServiceCapability_RPC_Type
}

func (testDriver) Name() string        { return "csi.test" }
func (testDriver) TopologyKey() string { return "" }
func (testDriver) ValidateVolumeCapability(*csi.VolumeCapability) error {
	return nil
}
func (testDriver) BuildCreateVolumeRequest(config.Config, *csi.CreateVolumeRequest) (backend.CreateVolumeRequest, error) {
	return backend.CreateVolumeRequest{}, nil
}
func (testDriver) VolumeContext(*backend.Volume) map[string]string { return nil }
func (d testDriver) ControllerCapabilities() []csi.ControllerServiceCapability_RPC_Type {
	return d.controllerCaps
}
func (d testDriver) NodeCapabilities() []csi.NodeServiceCapability_RPC_Type { return d.nodeCaps }

type testShareDriver struct{ testDriver }

func (testShareDriver) ShareMount(string, map[string]string) (backend.ShareMount, error) {
	return backend.ShareMount{}, nil
}

type testBothDriver struct{ testShareDriver }

func (testBothDriver) DevicePathKey() string                                { return "devicePath" }
func (testBothDriver) PublishContext(*backend.Attachment) map[string]string { return nil }

func TestValidate(t *testing.T) {
	stage := []csi.NodeServiceCapability_RPC_Type{csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME}
	create := []csi.ControllerServiceCapability_RPC_Type{csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME}
	publish := []csi.ControllerServiceCapability_RPC_Type{csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME}
	tests := []struct {
		name    string
		driver  backend.Driver
		service backend.Service
		wantErr bool
	}{
		{name: "evs", driver: backendevs.New(), service: fullService{}},
		{name: "evs without snapshotter", driver: backendevs.New(), service: attachingService{}, wantErr: true},
		{name: "evs without attacher", driver: backendevs.New(), service: coreService{}, wantErr: true},
		{name: "neither block nor share driver", driver: testDriver{controllerCaps: create, nodeCaps: stage}, service: coreService{}, wantErr: true},
		{name: "block and share driver", driver: testBothDriver{testShareDriver{testDriver{controllerCaps: publish, nodeCaps: stage}}}, service: fullService{}, wantErr: true},
		{
			name:    "file share",
			driver:  testShareDriver{testDriver{controllerCaps: create, nodeCaps: stage}},
			service: coreService{},
		},
		{
			name:    "file share with publish",
			driver:  testShareDriver{testDriver{controllerCaps: publish, nodeCaps: stage}},
			service: attachingService{},
			wantErr: true,
		},
		{
			name:    "file share with node expand",
			driver:  testShareDriver{testDriver{controllerCaps: create, nodeCaps: append(stage, csi.NodeServiceCapability_RPC_EXPAND_VOLUME)}},
			service: coreService{},
			wantErr: true,
		},
		{
			name:    "file share without stage",
			driver:  testShareDriver{testDriver{controllerCaps: create}},
			service: coreService{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := backend.Validate(tt.driver, tt.service)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
