package driver

import (
	"context"
	"fmt"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"

	backendevs "t-cloud-public-csi-driver/internal/backend/evs"
	"t-cloud-public-csi-driver/internal/config"
)

type fakeMounter struct {
	ensureDirPaths  []string
	ensureFilePaths []string
	removePaths     []string
	mounted         map[string]bool
	mountCalls      []mountCall
	unmountCalls    []string
	isMountedErr    error
	mountErr        error
	unmountErr      error
	removeErr       error
}

type mountCall struct {
	source  string
	target  string
	fsType  string
	options []string
}

func (m *fakeMounter) EnsureDir(path string) error {
	m.ensureDirPaths = append(m.ensureDirPaths, path)
	return nil
}

func (m *fakeMounter) EnsureFile(path string) error {
	m.ensureFilePaths = append(m.ensureFilePaths, path)
	return nil
}

func (m *fakeMounter) Remove(path string) error {
	m.removePaths = append(m.removePaths, path)
	return m.removeErr
}

func (m *fakeMounter) IsMounted(target string) (bool, error) {
	if m.isMountedErr != nil {
		return false, m.isMountedErr
	}
	return m.mounted[target], nil
}

func (m *fakeMounter) Mount(_ context.Context, source, target, fsType string, options []string) error {
	m.mountCalls = append(m.mountCalls, mountCall{source: source, target: target, fsType: fsType, options: options})
	return m.mountErr
}

func (m *fakeMounter) Unmount(_ context.Context, target string) error {
	m.unmountCalls = append(m.unmountCalls, target)
	return m.unmountErr
}

type fakeDeviceManager struct {
	ensureFormattedDevice string
	ensureFormattedFS     string
	ensureFormattedErr    error

	resizePath string
	resizeFS   string
	resizeErr  error
}

func (m *fakeDeviceManager) EnsureFormatted(_ context.Context, devicePath, fsType string) error {
	m.ensureFormattedDevice = devicePath
	m.ensureFormattedFS = fsType
	return m.ensureFormattedErr
}

func (m *fakeDeviceManager) Resize(_ context.Context, volumePath, fsType string) error {
	m.resizePath = volumePath
	m.resizeFS = fsType
	return m.resizeErr
}

func TestNodeStageVolumeMountsFilesystemVolume(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{}}
	deviceManager := &fakeDeviceManager{}
	server := &nodeServer{
		cfg:            config.Config{},
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: &fakeDeviceResolver{resolvedPath: "/dev/vdb"},
		mounter:        mounter,
		deviceManager:  deviceManager,
	}

	_, err := server.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
		VolumeId:          "vol-1",
		StagingTargetPath: "/staging/vol-1",
		PublishContext:    map[string]string{"devicePath": "/dev/vdb"},
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{FsType: "xfs", MountFlags: []string{"noatime"}},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
	})
	if err != nil {
		t.Fatalf("NodeStageVolume returned error: %v", err)
	}

	if deviceManager.ensureFormattedDevice != "/dev/vdb" || deviceManager.ensureFormattedFS != "xfs" {
		t.Fatalf("unexpected ensure formatted request: device=%q fs=%q", deviceManager.ensureFormattedDevice, deviceManager.ensureFormattedFS)
	}
	if len(mounter.mountCalls) != 1 {
		t.Fatalf("expected one mount call, got %d", len(mounter.mountCalls))
	}
	call := mounter.mountCalls[0]
	if call.source != "/dev/vdb" || call.target != "/staging/vol-1" || call.fsType != "xfs" {
		t.Fatalf("unexpected mount call: %+v", call)
	}
}

func TestNodeStageVolumeSkipsBlockVolumes(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{}}
	server := &nodeServer{
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: &fakeDeviceResolver{resolvedPath: "/dev/vdb"},
		mounter:        mounter,
		deviceManager:  &fakeDeviceManager{},
	}

	_, err := server.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
		VolumeId:          "vol-1",
		StagingTargetPath: "/staging/vol-1",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
	})
	if err != nil {
		t.Fatalf("NodeStageVolume returned error: %v", err)
	}
	if len(mounter.mountCalls) != 0 {
		t.Fatalf("expected no mount calls, got %d", len(mounter.mountCalls))
	}
}

func TestNodeStageVolumeReturnsWhenAlreadyMounted(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{"/staging/vol-1": true}}
	deviceManager := &fakeDeviceManager{}
	server := &nodeServer{
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: &fakeDeviceResolver{resolvedPath: "/dev/vdb"},
		mounter:        mounter,
		deviceManager:  deviceManager,
	}

	_, err := server.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
		VolumeId:          "vol-1",
		StagingTargetPath: "/staging/vol-1",
		PublishContext:    map[string]string{"devicePath": "/dev/vdb"},
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
	})
	if err != nil {
		t.Fatalf("NodeStageVolume returned error: %v", err)
	}
	if deviceManager.ensureFormattedDevice != "" {
		t.Fatalf("did not expect formatting for mounted volume, got %q", deviceManager.ensureFormattedDevice)
	}
	if len(mounter.mountCalls) != 0 {
		t.Fatalf("expected no mount calls, got %+v", mounter.mountCalls)
	}
}

func TestNodePublishVolumeBindMountsStagedPath(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{}}
	server := &nodeServer{
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: &fakeDeviceResolver{resolvedPath: "/staging/vol-1"},
		mounter:        mounter,
		deviceManager:  &fakeDeviceManager{},
	}

	_, err := server.NodePublishVolume(context.Background(), &csi.NodePublishVolumeRequest{
		VolumeId:          "vol-1",
		TargetPath:        "/pods/vol-1",
		StagingTargetPath: "/staging/vol-1",
		Readonly:          true,
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{FsType: "ext4", MountFlags: []string{"noatime"}},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
	})
	if err != nil {
		t.Fatalf("NodePublishVolume returned error: %v", err)
	}

	if len(mounter.mountCalls) != 1 {
		t.Fatalf("expected one mount call, got %d", len(mounter.mountCalls))
	}
	call := mounter.mountCalls[0]
	if call.source != "/staging/vol-1" || call.target != "/pods/vol-1" {
		t.Fatalf("unexpected bind mount call: %+v", call)
	}
	if call.options[0] != "bind" {
		t.Fatalf("expected bind mount options, got %+v", call.options)
	}
}

func TestNodePublishVolumeBlockUsesDevicePath(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{}}
	server := &nodeServer{
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: &fakeDeviceResolver{resolvedPath: "/dev/vdb"},
		mounter:        mounter,
		deviceManager:  &fakeDeviceManager{},
	}

	_, err := server.NodePublishVolume(context.Background(), &csi.NodePublishVolumeRequest{
		VolumeId:       "vol-1",
		TargetPath:     "/pods/blockvol",
		PublishContext: map[string]string{"devicePath": "/dev/vdb"},
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
	})
	if err != nil {
		t.Fatalf("NodePublishVolume returned error: %v", err)
	}
	if len(mounter.ensureFilePaths) != 1 || mounter.ensureFilePaths[0] != "/pods/blockvol" {
		t.Fatalf("unexpected ensured files: %+v", mounter.ensureFilePaths)
	}
	if len(mounter.mountCalls) != 1 || mounter.mountCalls[0].source != "/dev/vdb" {
		t.Fatalf("unexpected mount calls: %+v", mounter.mountCalls)
	}
}

func TestNodePublishVolumeReturnsWhenAlreadyMounted(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{"/pods/vol-1": true}}
	server := &nodeServer{
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: &fakeDeviceResolver{resolvedPath: "/dev/vdb"},
		mounter:        mounter,
		deviceManager:  &fakeDeviceManager{},
	}

	_, err := server.NodePublishVolume(context.Background(), &csi.NodePublishVolumeRequest{
		VolumeId:          "vol-1",
		TargetPath:        "/pods/vol-1",
		StagingTargetPath: "/staging/vol-1",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
	})
	if err != nil {
		t.Fatalf("NodePublishVolume returned error: %v", err)
	}
	if len(mounter.mountCalls) != 0 {
		t.Fatalf("expected no mount calls, got %+v", mounter.mountCalls)
	}
}

func TestNodePublishVolumeBlockPartialFailureCanBeRetried(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{}, mountErr: fmt.Errorf("mount failed")}
	server := &nodeServer{
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: &fakeDeviceResolver{resolvedPath: "/dev/vdb"},
		mounter:        mounter,
		deviceManager:  &fakeDeviceManager{},
	}

	req := &csi.NodePublishVolumeRequest{
		VolumeId:       "vol-1",
		TargetPath:     "/pods/blockvol",
		PublishContext: map[string]string{"devicePath": "/dev/vdb"},
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
	}

	_, err := server.NodePublishVolume(context.Background(), req)
	assertCode(t, err, codes.Internal)

	mounter.mountErr = nil
	_, err = server.NodePublishVolume(context.Background(), req)
	if err != nil {
		t.Fatalf("retry NodePublishVolume returned error: %v", err)
	}
	if len(mounter.ensureFilePaths) != 2 {
		t.Fatalf("expected target file to be re-ensured on retry, got %+v", mounter.ensureFilePaths)
	}
	if len(mounter.mountCalls) != 2 {
		t.Fatalf("expected second mount attempt, got %+v", mounter.mountCalls)
	}
}

func TestNodeExpandVolumeResizesFilesystem(t *testing.T) {
	deviceManager := &fakeDeviceManager{}
	server := &nodeServer{
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: &fakeDeviceResolver{resolvedPath: "/pods/vol-1"},
		mounter:        &fakeMounter{mounted: map[string]bool{}},
		deviceManager:  deviceManager,
	}

	resp, err := server.NodeExpandVolume(context.Background(), &csi.NodeExpandVolumeRequest{
		VolumeId:   "vol-1",
		VolumePath: "/pods/vol-1",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 50,
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{FsType: "ext4"},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
	})
	if err != nil {
		t.Fatalf("NodeExpandVolume returned error: %v", err)
	}
	if deviceManager.resizePath != "/pods/vol-1" || deviceManager.resizeFS != "ext4" {
		t.Fatalf("unexpected resize request: path=%q fs=%q", deviceManager.resizePath, deviceManager.resizeFS)
	}
	if resp.CapacityBytes != 50 {
		t.Fatalf("unexpected response capacity: %d", resp.CapacityBytes)
	}
}

func TestNodeUnpublishVolumeUnmountsAndRemovesPath(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{"/pods/vol-1": true}}
	server := &nodeServer{
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: &fakeDeviceResolver{resolvedPath: "/dev/vdb"},
		mounter:        mounter,
		deviceManager:  &fakeDeviceManager{},
	}

	_, err := server.NodeUnpublishVolume(context.Background(), &csi.NodeUnpublishVolumeRequest{
		VolumeId:   "vol-1",
		TargetPath: "/pods/vol-1",
	})
	if err != nil {
		t.Fatalf("NodeUnpublishVolume returned error: %v", err)
	}
	if len(mounter.unmountCalls) != 1 || mounter.unmountCalls[0] != "/pods/vol-1" {
		t.Fatalf("unexpected unmounts: %+v", mounter.unmountCalls)
	}
	if len(mounter.removePaths) != 1 || mounter.removePaths[0] != "/pods/vol-1" {
		t.Fatalf("unexpected removals: %+v", mounter.removePaths)
	}
}

func TestNodeStageVolumeRequiresDevicePathForFilesystem(t *testing.T) {
	server := &nodeServer{
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: &fakeDeviceResolver{},
		mounter:        &fakeMounter{mounted: map[string]bool{}},
		deviceManager:  &fakeDeviceManager{},
	}

	_, err := server.NodeStageVolume(context.Background(), &csi.NodeStageVolumeRequest{
		VolumeId:          "vol-1",
		StagingTargetPath: "/staging/vol-1",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestNodeCapabilitiesExposeStageAndExpand(t *testing.T) {
	server := newNodeServer(config.Config{}, backendevs.New())

	resp, err := server.NodeGetCapabilities(context.Background(), &csi.NodeGetCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("NodeGetCapabilities returned error: %v", err)
	}
	if len(resp.Capabilities) != 2 {
		t.Fatalf("unexpected capability count: %d", len(resp.Capabilities))
	}
}

func TestNodePublishVolumeBlockResolvesDevicePath(t *testing.T) {
	mounter := &fakeMounter{mounted: map[string]bool{}}
	resolver := &fakeDeviceResolver{resolvedPath: "/dev/disk/by-id/virtio-volume"}
	server := &nodeServer{
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		deviceResolver: resolver,
		mounter:        mounter,
		deviceManager:  &fakeDeviceManager{},
	}

	_, err := server.NodePublishVolume(context.Background(), &csi.NodePublishVolumeRequest{
		VolumeId:       "vol-1",
		TargetPath:     "/pods/blockvol",
		PublishContext: map[string]string{"devicePath": "/dev/vdb"},
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
	})
	if err != nil {
		t.Fatalf("NodePublishVolume returned error: %v", err)
	}
	if resolver.volumeID != "vol-1" || resolver.reportedPath != "/dev/vdb" {
		t.Fatalf("unexpected resolve request: volume=%q path=%q", resolver.volumeID, resolver.reportedPath)
	}
	if len(mounter.mountCalls) != 1 || mounter.mountCalls[0].source != "/dev/disk/by-id/virtio-volume" {
		t.Fatalf("unexpected mount calls: %+v", mounter.mountCalls)
	}
}
