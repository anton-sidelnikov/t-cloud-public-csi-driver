package backend

import (
	"context"
	"errors"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"t-cloud-public-csi-driver/internal/config"
)

// ErrVolumeAlreadyExists is wrapped by Service.CreateVolume when a volume with the requested
// name exists but does not match the request. The controller reports it as AlreadyExists.
var ErrVolumeAlreadyExists = errors.New("volume already exists with different parameters")

// ErrVolumeNotFound is wrapped by VolumeGetter.GetVolume when the volume does not exist.
var ErrVolumeNotFound = errors.New("volume not found")

// ErrVolumeOperationNotSupported is wrapped when an operation is not possible for the
// given volume, for example deleting a static volume that is only a share subdirectory.
// The controller reports it as FailedPrecondition.
var ErrVolumeOperationNotSupported = errors.New("operation is not supported for this volume")

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
	// SFS Turbo file system type, empty for Standard and Performance.
	ExpandType   string
	HPCBandwidth string
	// KMSKeyID is the encryption key of a file share, empty if it is not encrypted.
	KMSKeyID string
	// Condition is the health of the volume, reported with VOLUME_CONDITION.
	Condition VolumeCondition
}

// VolumeCondition describes whether a volume is usable.
type VolumeCondition struct {
	Abnormal bool
	Message  string
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
	// Network placement and encryption of file shares.
	VPCID           string
	SubnetID        string
	SecurityGroupID string
	KMSKeyID        string
	// SFS Turbo file system type: expand_type (bandwidth or hpc) and hpc_bw.
	ExpandType   string
	HPCBandwidth string
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

// VolumeGetter is implemented by services that can look up a volume by ID
// (ControllerServiceCapability GET_VOLUME). File share backends also use it on the node
// to find the export location of statically provisioned volumes.
type VolumeGetter interface {
	GetVolume(context.Context, string) (*Volume, error)
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
	// ShareMount wraps ErrExportLocationUnknown when the volume context has no export
	// location (statically provisioned volumes); the node then looks the volume up with
	// VolumeGetter and retries with the volume context of the found volume.
	ShareMount(volumeID string, volumeContext map[string]string) (ShareMount, error)
}

// ErrExportLocationUnknown is wrapped by FileShareDriver.ShareMount, see there.
var ErrExportLocationUnknown = errors.New("export location is not in the volume context")

// ShareMount describes how a node mounts a file share.
type ShareMount struct {
	Source  string
	FSType  string
	Options []string
}
