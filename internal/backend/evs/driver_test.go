package evs

import (
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

func TestBuildCreateVolumeRequestUsesDefaults(t *testing.T) {
	driver := New()

	req, err := driver.BuildCreateVolumeRequest(config.Config{AvailabilityZone: "eu-de-01"}, &csi.CreateVolumeRequest{
		Name: "pvc-1",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10,
		},
		Parameters: map[string]string{
			"volumeType": "SSD",
		},
	})
	if err != nil {
		t.Fatalf("BuildCreateVolumeRequest returned error: %v", err)
	}
	if req.AvailabilityZone != "eu-de-01" {
		t.Fatalf("unexpected availability zone: %q", req.AvailabilityZone)
	}
	if req.VolumeType != "SSD" {
		t.Fatalf("unexpected volume type: %q", req.VolumeType)
	}
}

func TestPublishContextIncludesDevicePath(t *testing.T) {
	driver := New()

	ctx := driver.PublishContext(&backend.Attachment{
		ID:       "att-1",
		ServerID: "node-1",
		VolumeID: "vol-1",
		Device:   "/dev/vdb",
	})

	if ctx["devicePath"] != "/dev/vdb" {
		t.Fatalf("unexpected publish context: %+v", ctx)
	}
}

func TestValidateVolumeCapabilityRejectsUnsupportedMode(t *testing.T) {
	driver := New()

	err := driver.ValidateVolumeCapability(&csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}},
		AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
