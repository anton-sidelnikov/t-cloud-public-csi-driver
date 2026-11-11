package backend

import (
	"context"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"t-cloud-public-csi-driver/internal/config"
)

type Volume struct {
	ID               string
	Name             string
	Status           string
	AvailabilityZone string
	VolumeType       string
	SizeBytes        int64
	Attachments      []Attachment
	// ExportLocation is the network address of a file share, for example host:/path.
	ExportLocation string
}

type Attachment struct {
	ID       string
	ServerID string
	VolumeID string
	Device   string
}

type CreateVolumeRequest struct {
	Name             string
	SizeBytes        int64
	AvailabilityZone string
	VolumeType       string
	Description      string
	Metadata         map[string]string
	SnapshotID       string
}

type Snapshot struct {
	ID             string
	Name           string
	Description    string
	SourceVolumeID string
	Status         string
	SizeBytes      int64
	CreatedAt      time.Time
	ReadyToUse     bool
}

type ListSnapshotsRequest struct {
	ID             string
	Name           string
	SourceVolumeID string
}

// Service is the cloud API every backend implements.
type Service interface {
	CreateVolume(context.Context, CreateVolumeRequest) (*Volume, error)
	DeleteVolume(context.Context, string) error
	ExpandVolume(context.Context, string, int64) (int64, error)
}

// Attacher is implemented by services whose volumes are attached to nodes
// (ControllerServiceCapability PUBLISH_UNPUBLISH_VOLUME).
type Attacher interface {
	AttachVolume(context.Context, string, string) (*Attachment, error)
	DetachVolume(context.Context, string, string) error
}

// Snapshotter is implemented by services that support volume snapshots
// (ControllerServiceCapability CREATE_DELETE_SNAPSHOT).
type Snapshotter interface {
	CreateSnapshot(context.Context, string, string) (*Snapshot, error)
	DeleteSnapshot(context.Context, string) error
	GetSnapshot(context.Context, string) (*Snapshot, error)
	ListSnapshots(context.Context, ListSnapshotsRequest) ([]*Snapshot, error)
}

// Driver describes the CSI behavior of a backend. Every backend must implement
// exactly one of BlockDriver or FileShareDriver.
type Driver interface {
	Name() string
	// TopologyKey returns the node label used for volume topology. An empty key
	// means volumes are reachable from every node and no topology is reported.
	TopologyKey() string
	ValidateVolumeCapability(*csi.VolumeCapability) error
	BuildCreateVolumeRequest(config.Config, *csi.CreateVolumeRequest) (CreateVolumeRequest, error)
	VolumeContext(*Volume) map[string]string
	ControllerCapabilities() []csi.ControllerServiceCapability_RPC_Type
	NodeCapabilities() []csi.NodeServiceCapability_RPC_Type
}

// BlockDriver is implemented by backends whose volumes are attached to nodes as
// block devices, then formatted and mounted by the node plugin.
type BlockDriver interface {
	Driver
	DevicePathKey() string
	PublishContext(*Attachment) map[string]string
}

// FileShareDriver is implemented by backends whose volumes are network file shares
// mounted directly by the node plugin, without attach, formatting, or resize.
type FileShareDriver interface {
	Driver
	ShareMount(volumeID string, volumeContext map[string]string) (ShareMount, error)
}

// ShareMount describes how a node mounts a file share.
type ShareMount struct {
	Source  string
	FSType  string
	Options []string
}
