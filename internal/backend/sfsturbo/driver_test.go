package sfsturbo

import (
	"context"
	"strings"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

type coreService struct{}

func (coreService) CreateVolume(context.Context, backend.CreateVolumeRequest) (*backend.Volume, error) {
	return nil, nil
}
func (coreService) DeleteVolume(context.Context, string) error { return nil }
func (coreService) ExpandVolume(context.Context, string, int64) (int64, error) {
	return 0, nil
}

func TestDriverPassesBackendValidation(t *testing.T) {
	if err := backend.Validate(New(), coreService{}); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func mountCapability(mode csi.VolumeCapability_AccessMode_Mode, fsType string) *csi.VolumeCapability {
	return &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{FsType: fsType}},
		AccessMode: &csi.VolumeCapability_AccessMode{Mode: mode},
	}
}

func TestValidateVolumeCapability(t *testing.T) {
	tests := []struct {
		name       string
		capability *csi.VolumeCapability
		wantErr    bool
	}{
		{name: "multi node writer", capability: mountCapability(csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER, "")},
		{name: "single node writer with nfs", capability: mountCapability(csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER, "nfs")},
		{name: "multi node reader", capability: mountCapability(csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY, "")},
		{name: "ext4 filesystem", capability: mountCapability(csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER, "ext4"), wantErr: true},
		{name: "unknown access mode", capability: mountCapability(csi.VolumeCapability_AccessMode_UNKNOWN, ""), wantErr: true},
		{
			name: "block",
			capability: &csi.VolumeCapability{
				AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}},
				AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New().ValidateVolumeCapability(tt.capability)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateVolumeCapability() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func networkConfig() config.Config {
	return config.Config{
		AvailabilityZone: "eu-de-01",
		VPCID:            "vpc-cfg",
		SubnetID:         "subnet-cfg",
		SecurityGroupID:  "sg-cfg",
	}
}

func TestBuildCreateVolumeRequestUsesConfigDefaults(t *testing.T) {
	req, err := New().BuildCreateVolumeRequest(networkConfig(), &csi.CreateVolumeRequest{
		Name:          "pvc-1234",
		CapacityRange: &csi.CapacityRange{RequiredBytes: 10 * gibibyte},
	})
	if err != nil {
		t.Fatalf("BuildCreateVolumeRequest() error = %v", err)
	}
	want := backend.CreateVolumeRequest{
		Name:             "pvc-1234",
		SizeBytes:        MinSizeGiB * gibibyte,
		AvailabilityZone: "eu-de-01",
		VolumeType:       ShareTypeStandard,
		VPCID:            "vpc-cfg",
		SubnetID:         "subnet-cfg",
		SecurityGroupID:  "sg-cfg",
	}
	if req.Name != want.Name || req.SizeBytes != want.SizeBytes || req.AvailabilityZone != want.AvailabilityZone ||
		req.VolumeType != want.VolumeType || req.VPCID != want.VPCID || req.SubnetID != want.SubnetID ||
		req.SecurityGroupID != want.SecurityGroupID || req.KMSKeyID != "" {
		t.Fatalf("BuildCreateVolumeRequest() = %+v, want %+v", req, want)
	}
}

func TestBuildCreateVolumeRequestParametersOverrideConfig(t *testing.T) {
	req, err := New().BuildCreateVolumeRequest(networkConfig(), &csi.CreateVolumeRequest{
		Name:          "pvc-1234",
		CapacityRange: &csi.CapacityRange{RequiredBytes: 600*gibibyte + 1},
		Parameters: map[string]string{
			"shareType":                 "performance",
			"availabilityZone":          "eu-de-02",
			"vpcId":                     "vpc-sc",
			"subnetId":                  "subnet-sc",
			"securityGroupId":           "sg-sc",
			"kmsKeyId":                  "kms-1",
			"description":               " shared data ",
			"csi.storage.k8s.io/fstype": "nfs",
		},
	})
	if err != nil {
		t.Fatalf("BuildCreateVolumeRequest() error = %v", err)
	}
	if req.SizeBytes != 601*gibibyte || req.VolumeType != ShareTypePerformance || req.AvailabilityZone != "eu-de-02" ||
		req.VPCID != "vpc-sc" || req.SubnetID != "subnet-sc" || req.SecurityGroupID != "sg-sc" ||
		req.KMSKeyID != "kms-1" || req.Description != "shared data" {
		t.Fatalf("BuildCreateVolumeRequest() = %+v", req)
	}
}

func TestBuildCreateVolumeRequestErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		req     *csi.CreateVolumeRequest
		wantErr string
	}{
		{
			name:    "missing network settings",
			cfg:     config.Config{},
			req:     &csi.CreateVolumeRequest{Name: "pvc-1234"},
			wantErr: "availabilityZone (or OS_AVAILABILITY_ZONE), vpcId (or OS_VPC_ID), subnetId (or OS_SUBNET_ID), securityGroupId (or OS_SECURITY_GROUP_ID)",
		},
		{
			name:    "invalid share type",
			cfg:     networkConfig(),
			req:     &csi.CreateVolumeRequest{Name: "pvc-1234", Parameters: map[string]string{"shareType": "HPC"}},
			wantErr: "must be STANDARD or PERFORMANCE",
		},
		{
			name:    "unknown parameter",
			cfg:     networkConfig(),
			req:     &csi.CreateVolumeRequest{Name: "pvc-1234", Parameters: map[string]string{"volumeType": "SSD"}},
			wantErr: `unsupported SFS Turbo StorageClass parameter "volumeType"`,
		},
		{
			name:    "non nfs fstype parameter",
			cfg:     networkConfig(),
			req:     &csi.CreateVolumeRequest{Name: "pvc-1234", Parameters: map[string]string{"csi.storage.k8s.io/fstype": "ext4"}},
			wantErr: "only \"nfs\"",
		},
		{
			name:    "limit below minimum size",
			cfg:     networkConfig(),
			req:     &csi.CreateVolumeRequest{Name: "pvc-1234", CapacityRange: &csi.CapacityRange{RequiredBytes: gibibyte, LimitBytes: 100 * gibibyte}},
			wantErr: "capacity limit",
		},
		{
			name:    "above maximum size",
			cfg:     networkConfig(),
			req:     &csi.CreateVolumeRequest{Name: "pvc-1234", CapacityRange: &csi.CapacityRange{RequiredBytes: (MaxSizeGiB + 1) * gibibyte}},
			wantErr: "exceeds the SFS Turbo maximum",
		},
		{
			name: "snapshot source",
			cfg:  networkConfig(),
			req: &csi.CreateVolumeRequest{
				Name: "pvc-1234",
				VolumeContentSource: &csi.VolumeContentSource{Type: &csi.VolumeContentSource_Snapshot{
					Snapshot: &csi.VolumeContentSource_SnapshotSource{SnapshotId: "snap"},
				}},
			},
			wantErr: "does not support volume content sources",
		},
		{
			name:    "invalid name",
			cfg:     networkConfig(),
			req:     &csi.CreateVolumeRequest{Name: "1-pvc"},
			wantErr: "is not a valid SFS Turbo name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().BuildCreateVolumeRequest(tt.cfg, tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("BuildCreateVolumeRequest() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestShareMount(t *testing.T) {
	driver := New()
	vol := &backend.Volume{ID: "share-1", Name: "pvc-1234", ExportLocation: "192.168.0.90:/", VolumeType: ShareTypeStandard}

	mount, err := driver.ShareMount(vol.ID, driver.VolumeContext(vol))
	if err != nil {
		t.Fatalf("ShareMount() error = %v", err)
	}
	if mount.Source != "192.168.0.90:/" || mount.FSType != "nfs" ||
		strings.Join(mount.Options, ",") != "vers=3,timeo=600,noresvport,nolock,tcp" {
		t.Fatalf("ShareMount() = %+v", mount)
	}

	if _, err := driver.ShareMount(vol.ID, map[string]string{}); err == nil {
		t.Fatal("ShareMount() without export location error = nil")
	}
}
