package backend

import (
	"context"

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
}

type Service interface {
	CreateVolume(context.Context, CreateVolumeRequest) (*Volume, error)
	DeleteVolume(context.Context, string) error
	AttachVolume(context.Context, string, string) (*Attachment, error)
	DetachVolume(context.Context, string, string) error
	ExpandVolume(context.Context, string, int64) (int64, error)
}

type Driver interface {
	Name() string
	TopologyKey() string
	DevicePathKey() string
	ValidateVolumeCapability(*csi.VolumeCapability) error
	BuildCreateVolumeRequest(config.Config, *csi.CreateVolumeRequest) (CreateVolumeRequest, error)
	VolumeContext(*Volume) map[string]string
	PublishContext(*Attachment) map[string]string
	ControllerCapabilities() []csi.ControllerServiceCapability_RPC_Type
	NodeCapabilities() []csi.NodeServiceCapability_RPC_Type
}
