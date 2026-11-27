package gpsfs

import (
	"slices"
	"strings"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"t-cloud-public-csi-driver/internal/config"
)

func TestFileSystemName(t *testing.T) {
	const pvcName = "pvc-2b1f3c4e-5d6a-4b7c-8d9e-0f1a2b3c4d5e"
	if got := FileSystemName(pvcName); got != pvcName {
		t.Fatalf("FileSystemName(%q) = %q, want unchanged", pvcName, got)
	}
	for _, name := range []string{"PVC-Upper", "with.dot", "ab", strings.Repeat("a", 64), "-leading"} {
		got := FileSystemName(name)
		if !fileSystemNamePattern.MatchString(got) || !strings.HasPrefix(got, hashedNamePrefix) {
			t.Errorf("FileSystemName(%q) = %q, want a valid hashed name", name, got)
		}
		if FileSystemName(name) != got {
			t.Errorf("FileSystemName(%q) is not deterministic", name)
		}
	}
	if FileSystemName("PVC-A") == FileSystemName("PVC-B") {
		t.Fatal("different names map to the same file system name")
	}
}

func TestParseVolumeID(t *testing.T) {
	for id, want := range map[string][2]string{
		"fs-name":           {"fs-name", ""},
		"fs-name:data/app/": {"fs-name", "data/app"},
		"fs-name:/data//":   {"fs-name", "data"},
	} {
		name, subPath, err := ParseVolumeID(id)
		if err != nil || name != want[0] || subPath != want[1] {
			t.Errorf("ParseVolumeID(%q) = %q, %q, %v; want %q, %q", id, name, subPath, err, want[0], want[1])
		}
	}
	for _, id := range []string{"", "Upper", "a.b", "fs-name:../x", "fs-name:a//b", "fs-name:"} {
		if _, _, err := ParseVolumeID(id); err == nil {
			t.Errorf("ParseVolumeID(%q) succeeded, want error", id)
		}
	}
}

func TestBuildCreateVolumeRequest(t *testing.T) {
	cfg := config.Config{VPCID: "vpc-config"}
	req := &csi.CreateVolumeRequest{Name: "pvc-1", CapacityRange: &csi.CapacityRange{RequiredBytes: 5 << 30}}

	got, err := New("eu-de").BuildCreateVolumeRequest(cfg, req)
	if err != nil {
		t.Fatalf("BuildCreateVolumeRequest() error = %v", err)
	}
	if got.Name != "pvc-1" || got.SizeBytes != 5<<30 || got.VPCID != "vpc-config" {
		t.Fatalf("BuildCreateVolumeRequest() = %+v", got)
	}

	req.Parameters = map[string]string{paramVPCID: " vpc-param ", paramFSType: "nfs"}
	req.CapacityRange = &csi.CapacityRange{LimitBytes: 1 << 30}
	got, err = New("eu-de").BuildCreateVolumeRequest(cfg, req)
	if err != nil || got.VPCID != "vpc-param" || got.SizeBytes != 1<<30 {
		t.Fatalf("BuildCreateVolumeRequest() with parameters = %+v, %v", got, err)
	}
}

func TestBuildCreateVolumeRequestErrors(t *testing.T) {
	cases := map[string]struct {
		cfg config.Config
		req *csi.CreateVolumeRequest
	}{
		"missing VPC":    {req: &csi.CreateVolumeRequest{Name: "pvc-1"}},
		"unknown param":  {cfg: config.Config{VPCID: "v"}, req: &csi.CreateVolumeRequest{Name: "pvc-1", Parameters: map[string]string{"shareType": "x"}}},
		"wrong fsType":   {cfg: config.Config{VPCID: "v"}, req: &csi.CreateVolumeRequest{Name: "pvc-1", Parameters: map[string]string{paramFSType: "ext4"}}},
		"content source": {cfg: config.Config{VPCID: "v"}, req: &csi.CreateVolumeRequest{Name: "pvc-1", VolumeContentSource: &csi.VolumeContentSource{}}},
		"negative size":  {cfg: config.Config{VPCID: "v"}, req: &csi.CreateVolumeRequest{Name: "pvc-1", CapacityRange: &csi.CapacityRange{RequiredBytes: -1}}},
		"missing name":   {cfg: config.Config{VPCID: "v"}, req: &csi.CreateVolumeRequest{}},
	}
	for name, tc := range cases {
		if _, err := New("eu-de").BuildCreateVolumeRequest(tc.cfg, tc.req); err == nil {
			t.Errorf("%s: BuildCreateVolumeRequest() succeeded, want error", name)
		}
	}
}

func TestShareMount(t *testing.T) {
	driver := New("eu-de")

	mount, err := driver.ShareMount("fs-a:data", nil)
	if err != nil {
		t.Fatalf("ShareMount() error = %v", err)
	}
	if mount.Source != "fs-a.sfs3.eu-de.otc.t-systems.com:/fs-a/data" || mount.FSType != "nfs" || !slices.Contains(mount.Options, "nosharecache") {
		t.Fatalf("ShareMount() = %+v", mount)
	}

	mount, err = driver.ShareMount("fs-a", map[string]string{ContextExportLocation: "host:/fs-a"})
	if err != nil || mount.Source != "host:/fs-a" {
		t.Fatalf("ShareMount() with context = %+v, %v", mount, err)
	}

	if _, err := driver.ShareMount("Invalid", nil); err == nil {
		t.Fatal("ShareMount() with an invalid volume ID succeeded")
	}
}
