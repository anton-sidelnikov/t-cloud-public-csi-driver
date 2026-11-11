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
			"volumeType":           " SSD ",
			"metadata.application": " database ",
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
	if req.Metadata["application"] != "database" {
		t.Fatalf("unexpected metadata: %+v", req.Metadata)
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

func TestBuildCreateVolumeRequestRejectsUnsupportedParameters(t *testing.T) {
	driver := New()

	_, err := driver.BuildCreateVolumeRequest(config.Config{AvailabilityZone: "eu-de-01"}, &csi.CreateVolumeRequest{
		Name: "pvc-1",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10,
		},
		Parameters: map[string]string{
			"volumeTypo": "SSD",
		},
	})
	if err == nil {
		t.Fatal("expected unsupported parameter error")
	}
}

func TestBuildCreateVolumeRequestAcceptsFSTypeParameter(t *testing.T) {
	driver := New()

	req, err := driver.BuildCreateVolumeRequest(config.Config{AvailabilityZone: "eu-de-01"}, &csi.CreateVolumeRequest{
		Name: "pvc-1",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10,
		},
		Parameters: map[string]string{
			"csi.storage.k8s.io/fstype": "ext4",
		},
	})
	if err != nil {
		t.Fatalf("BuildCreateVolumeRequest returned error: %v", err)
	}
	if len(req.Metadata) != 0 {
		t.Fatalf("did not expect CSI fstype parameter in EVS metadata: %+v", req.Metadata)
	}
}

func TestBuildCreateVolumeRequestIncludesSnapshotSource(t *testing.T) {
	driver := New()

	req, err := driver.BuildCreateVolumeRequest(config.Config{AvailabilityZone: "eu-de-01"}, &csi.CreateVolumeRequest{
		Name: "pvc-1",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10,
		},
		VolumeContentSource: &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Snapshot{
				Snapshot: &csi.VolumeContentSource_SnapshotSource{SnapshotId: "snap-1"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildCreateVolumeRequest returned error: %v", err)
	}
	if req.SnapshotID != "snap-1" {
		t.Fatalf("unexpected snapshot id: %q", req.SnapshotID)
	}
}

func TestBuildCreateVolumeRequestRejectsEmptyMetadataKey(t *testing.T) {
	driver := New()

	_, err := driver.BuildCreateVolumeRequest(config.Config{AvailabilityZone: "eu-de-01"}, &csi.CreateVolumeRequest{
		Name: "pvc-1",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10,
		},
		Parameters: map[string]string{
			"metadata.": "value",
		},
	})
	if err == nil {
		t.Fatal("expected empty metadata key error")
	}
}

func TestBuildCreateVolumeRequestSelectsAvailabilityZone(t *testing.T) {
	zones := func(names ...string) []*csi.Topology {
		out := make([]*csi.Topology, 0, len(names))
		for _, name := range names {
			out = append(out, &csi.Topology{Segments: map[string]string{topologyKey: name}})
		}
		return out
	}
	cases := []struct {
		name         string
		param        string
		requirements *csi.TopologyRequirement
		want         string
		wantErr      bool
	}{
		{name: "config fallback", want: "eu-de-01"},
		{name: "preferred wins over requisite and config", requirements: &csi.TopologyRequirement{Requisite: zones("eu-de-01", "eu-de-02"), Preferred: zones("eu-de-02")}, want: "eu-de-02"},
		{name: "requisite without preferred", requirements: &csi.TopologyRequirement{Requisite: zones("eu-de-03")}, want: "eu-de-03"},
		{name: "parameter within requisite", param: "eu-de-01", requirements: &csi.TopologyRequirement{Requisite: zones("eu-de-01", "eu-de-02"), Preferred: zones("eu-de-02")}, want: "eu-de-01"},
		{name: "parameter outside requisite", param: "eu-de-01", requirements: &csi.TopologyRequirement{Requisite: zones("eu-de-02")}, wantErr: true},
		{name: "parameter without topology", param: "eu-de-03", want: "eu-de-03"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]string{}
			if tc.param != "" {
				params[paramAvailabilityZone] = tc.param
			}
			req, err := New().BuildCreateVolumeRequest(config.Config{AvailabilityZone: "eu-de-01"}, &csi.CreateVolumeRequest{
				Name:                      "pvc-1",
				CapacityRange:             &csi.CapacityRange{RequiredBytes: 10},
				Parameters:                params,
				AccessibilityRequirements: tc.requirements,
			})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got zone %q", req.AvailabilityZone)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildCreateVolumeRequest: %v", err)
			}
			if req.AvailabilityZone != tc.want {
				t.Fatalf("got zone %q want %q", req.AvailabilityZone, tc.want)
			}
		})
	}
}
