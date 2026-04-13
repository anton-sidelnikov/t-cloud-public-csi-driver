package evs

import (
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

const (
	driverName  = "csi.evs.tcloudpublic.com"
	topologyKey = "topology.evs.tcloudpublic.com/zone"
	deviceKey   = "devicePath"
)

type Driver struct{}

func New() *Driver {
	return &Driver{}
}

func (d *Driver) Name() string {
	return driverName
}

func (d *Driver) TopologyKey() string {
	return topologyKey
}

func (d *Driver) DevicePathKey() string {
	return deviceKey
}

func (d *Driver) ValidateVolumeCapability(capability *csi.VolumeCapability) error {
	if capability == nil {
		return fmt.Errorf("nil capability")
	}
	if capability.GetBlock() == nil && capability.GetMount() == nil {
		return fmt.Errorf("either block or mount access type is required")
	}
	if capability.GetAccessMode() == nil {
		return fmt.Errorf("access mode is required")
	}

	switch capability.GetAccessMode().GetMode() {
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER:
		return nil
	default:
		return fmt.Errorf("access mode %s is not supported", capability.GetAccessMode().GetMode().String())
	}
}

func (d *Driver) BuildCreateVolumeRequest(cfg config.Config, req *csi.CreateVolumeRequest) (backend.CreateVolumeRequest, error) {
	if req.GetName() == "" {
		return backend.CreateVolumeRequest{}, fmt.Errorf("name is required")
	}
	if req.GetCapacityRange() == nil || req.GetCapacityRange().GetRequiredBytes() <= 0 {
		return backend.CreateVolumeRequest{}, fmt.Errorf("required capacity must be set")
	}

	params := req.GetParameters()
	return backend.CreateVolumeRequest{
		Name:             req.GetName(),
		SizeBytes:        req.GetCapacityRange().GetRequiredBytes(),
		AvailabilityZone: valueOrDefault(params["availabilityZone"], cfg.AvailabilityZone),
		VolumeType:       params["volumeType"],
		Description:      params["description"],
		Metadata:         params,
	}, nil
}

func (d *Driver) VolumeContext(vol *backend.Volume) map[string]string {
	return map[string]string{
		"name":             vol.Name,
		"availabilityZone": vol.AvailabilityZone,
		"volumeType":       vol.VolumeType,
		"status":           vol.Status,
	}
}

func (d *Driver) PublishContext(attachment *backend.Attachment) map[string]string {
	return map[string]string{
		"attachmentID": attachment.ID,
		deviceKey:      attachment.Device,
		"nodeID":       attachment.ServerID,
		"volumeID":     attachment.VolumeID,
	}
}

func (d *Driver) ControllerCapabilities() []csi.ControllerServiceCapability_RPC_Type {
	return []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	}
}

func (d *Driver) NodeCapabilities() []csi.NodeServiceCapability_RPC_Type {
	return []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
	}
}

func valueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
