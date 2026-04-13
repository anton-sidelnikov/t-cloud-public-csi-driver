package driver

import (
	"context"
	"fmt"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"t-cloud-public-csi-driver/internal/backend"
	backendevs "t-cloud-public-csi-driver/internal/backend/evs"
	"t-cloud-public-csi-driver/internal/config"
)

type fakeControllerService struct {
	createVolumeReq backend.CreateVolumeRequest
	createVolumeRes *backend.Volume
	createVolumeErr error

	deleteVolumeID  string
	deleteVolumeErr error

	attachVolumeID string
	attachNodeID   string
	attachRes      *backend.Attachment
	attachErr      error

	detachVolumeID string
	detachNodeID   string
	detachErr      error

	expandVolumeID string
	expandSize     int64
	expandRes      int64
	expandErr      error
}

func (f *fakeControllerService) CreateVolume(_ context.Context, req backend.CreateVolumeRequest) (*backend.Volume, error) {
	f.createVolumeReq = req
	return f.createVolumeRes, f.createVolumeErr
}

func (f *fakeControllerService) DeleteVolume(_ context.Context, volumeID string) error {
	f.deleteVolumeID = volumeID
	return f.deleteVolumeErr
}

func (f *fakeControllerService) AttachVolume(_ context.Context, volumeID, nodeID string) (*backend.Attachment, error) {
	f.attachVolumeID = volumeID
	f.attachNodeID = nodeID
	return f.attachRes, f.attachErr
}

func (f *fakeControllerService) DetachVolume(_ context.Context, volumeID, nodeID string) error {
	f.detachVolumeID = volumeID
	f.detachNodeID = nodeID
	return f.detachErr
}

func (f *fakeControllerService) ExpandVolume(_ context.Context, volumeID string, size int64) (int64, error) {
	f.expandVolumeID = volumeID
	f.expandSize = size
	return f.expandRes, f.expandErr
}

func TestCreateVolumeValidatesRequest(t *testing.T) {
	server := newControllerServer(config.Config{}, backendevs.New(), &fakeControllerService{})

	_, err := server.CreateVolume(context.Background(), &csi.CreateVolumeRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestCreateVolumePassesParametersToService(t *testing.T) {
	service := &fakeControllerService{
		createVolumeRes: &backend.Volume{
			ID:               "vol-1",
			Name:             "pvc-1",
			Status:           "available",
			AvailabilityZone: "eu-de-01",
			VolumeType:       "SSD",
			SizeBytes:        10,
		},
	}
	server := newControllerServer(config.Config{AvailabilityZone: "fallback-az"}, backendevs.New(), service)

	resp, err := server.CreateVolume(context.Background(), &csi.CreateVolumeRequest{
		Name: "pvc-1",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10,
		},
		Parameters: map[string]string{
			"volumeType":  "SSD",
			"description": "claim volume",
		},
	})
	if err != nil {
		t.Fatalf("CreateVolume returned error: %v", err)
	}

	if service.createVolumeReq.AvailabilityZone != "fallback-az" {
		t.Fatalf("unexpected availability zone: %q", service.createVolumeReq.AvailabilityZone)
	}
	if service.createVolumeReq.VolumeType != "SSD" {
		t.Fatalf("unexpected volume type: %q", service.createVolumeReq.VolumeType)
	}
	if service.createVolumeReq.Description != "claim volume" {
		t.Fatalf("unexpected description: %q", service.createVolumeReq.Description)
	}
	if resp.GetVolume().GetVolumeId() != "vol-1" {
		t.Fatalf("unexpected volume id: %q", resp.GetVolume().GetVolumeId())
	}
}

func TestCreateVolumeUsesExplicitAvailabilityZone(t *testing.T) {
	service := &fakeControllerService{
		createVolumeRes: &backend.Volume{
			ID:               "vol-1",
			Name:             "pvc-1",
			Status:           "available",
			AvailabilityZone: "eu-de-02",
			VolumeType:       "SSD",
			SizeBytes:        10,
		},
	}
	server := newControllerServer(config.Config{AvailabilityZone: "fallback-az"}, backendevs.New(), service)

	_, err := server.CreateVolume(context.Background(), &csi.CreateVolumeRequest{
		Name: "pvc-1",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10,
		},
		Parameters: map[string]string{
			"availabilityZone": "eu-de-02",
		},
	})
	if err != nil {
		t.Fatalf("CreateVolume returned error: %v", err)
	}
	if service.createVolumeReq.AvailabilityZone != "eu-de-02" {
		t.Fatalf("unexpected availability zone: %q", service.createVolumeReq.AvailabilityZone)
	}
}

func TestControllerPublishVolumeReturnsPublishContext(t *testing.T) {
	service := &fakeControllerService{
		attachRes: &backend.Attachment{
			ID:       "att-1",
			ServerID: "node-1",
			VolumeID: "vol-1",
			Device:   "/dev/vdb",
		},
	}
	server := newControllerServer(config.Config{}, backendevs.New(), service)

	resp, err := server.ControllerPublishVolume(context.Background(), &csi.ControllerPublishVolumeRequest{
		VolumeId: "vol-1",
		NodeId:   "node-1",
	})
	if err != nil {
		t.Fatalf("ControllerPublishVolume returned error: %v", err)
	}

	if resp.PublishContext["attachmentID"] != "att-1" {
		t.Fatalf("unexpected publish context: %+v", resp.PublishContext)
	}
	if resp.PublishContext["devicePath"] != "/dev/vdb" {
		t.Fatalf("unexpected device path in publish context: %+v", resp.PublishContext)
	}
}

func TestControllerExpandVolumePassesCapacity(t *testing.T) {
	service := &fakeControllerService{expandRes: 20}
	server := newControllerServer(config.Config{}, backendevs.New(), service)

	resp, err := server.ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
		VolumeId: "vol-1",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 20,
		},
	})
	if err != nil {
		t.Fatalf("ControllerExpandVolume returned error: %v", err)
	}

	if service.expandVolumeID != "vol-1" || service.expandSize != 20 {
		t.Fatalf("unexpected expand request: volume=%q size=%d", service.expandVolumeID, service.expandSize)
	}
	if !resp.NodeExpansionRequired {
		t.Fatal("expected node expansion to be required")
	}
}

func TestValidateVolumeCapabilitiesRejectsUnsupportedMode(t *testing.T) {
	server := newControllerServer(config.Config{}, backendevs.New(), &fakeControllerService{})

	resp, err := server.ValidateVolumeCapabilities(context.Background(), &csi.ValidateVolumeCapabilitiesRequest{
		VolumeId: "vol-1",
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}},
				AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER},
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateVolumeCapabilities returned error: %v", err)
	}
	if resp.GetConfirmed() != nil {
		t.Fatal("expected rejected capabilities")
	}
	if resp.GetMessage() == "" {
		t.Fatal("expected validation message")
	}
}

func TestDeleteVolumePropagatesServiceErrors(t *testing.T) {
	service := &fakeControllerService{deleteVolumeErr: fmt.Errorf("boom")}
	server := newControllerServer(config.Config{}, backendevs.New(), service)

	_, err := server.DeleteVolume(context.Background(), &csi.DeleteVolumeRequest{VolumeId: "vol-1"})
	assertCode(t, err, codes.Internal)
}

func TestNodeGetInfoUsesConfig(t *testing.T) {
	server := &nodeServer{
		cfg: config.Config{
			NodeID:            "node-1",
			MaxVolumesPerNode: 64,
			AvailabilityZone:  "eu-de-01",
		},
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "123e4567-e89b-12d3-a456-426614174000"},
	}

	resp, err := server.NodeGetInfo(context.Background(), &csi.NodeGetInfoRequest{})
	if err != nil {
		t.Fatalf("NodeGetInfo returned error: %v", err)
	}
	if resp.NodeId != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected node id: %q", resp.NodeId)
	}
	if resp.AccessibleTopology.GetSegments()["topology.evs.tcloudpublic.com/zone"] != "eu-de-01" {
		t.Fatalf("unexpected topology: %+v", resp.AccessibleTopology)
	}
}

func TestValidateVolumeCapabilityAcceptsSupportedModes(t *testing.T) {
	driver := backendevs.New()
	supported := []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
	}

	for _, mode := range supported {
		t.Run(mode.String(), func(t *testing.T) {
			err := driver.ValidateVolumeCapability(&csi.VolumeCapability{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{FsType: "ext4"},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{Mode: mode},
			})
			if err != nil {
				t.Fatalf("validateVolumeCapability returned error: %v", err)
			}
		})
	}
}

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected gRPC error with code %s", want)
	}
	if got := status.Code(err); got != want {
		t.Fatalf("unexpected gRPC code: got %s want %s", got, want)
	}
}
