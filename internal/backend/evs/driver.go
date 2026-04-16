package evs

import (
	"fmt"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

const (
	driverName  = "csi.evs.tcloudpublic.com"
	topologyKey = "topology.evs.tcloudpublic.com/zone"
	deviceKey   = "devicePath"

	paramAvailabilityZone = "availabilityZone"
	paramDescription      = "description"
	paramFSType           = "csi.storage.k8s.io/fstype"
	paramMetadataPrefix   = "metadata."
	paramVolumeType       = "volumeType"
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

	params, err := validateAndNormalizeParameters(req.GetParameters())
	if err != nil {
		return backend.CreateVolumeRequest{}, err
	}

	return backend.CreateVolumeRequest{
		Name:             req.GetName(),
		SizeBytes:        req.GetCapacityRange().GetRequiredBytes(),
		AvailabilityZone: valueOrDefault(params[paramAvailabilityZone], cfg.AvailabilityZone),
		VolumeType:       params[paramVolumeType],
		Description:      params[paramDescription],
		Metadata:         volumeMetadata(params),
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

func validateAndNormalizeParameters(params map[string]string) (map[string]string, error) {
	normalized := make(map[string]string, len(params))
	for key, value := range params {
		switch {
		case key == paramAvailabilityZone,
			key == paramDescription,
			key == paramFSType,
			key == paramVolumeType:
			normalized[key] = strings.TrimSpace(value)
		case strings.HasPrefix(key, paramMetadataPrefix):
			metadataKey := strings.TrimPrefix(key, paramMetadataPrefix)
			if metadataKey == "" {
				return nil, fmt.Errorf("metadata parameter %q must include a metadata key after %q", key, paramMetadataPrefix)
			}
			normalized[key] = strings.TrimSpace(value)
		default:
			return nil, fmt.Errorf("unsupported EVS StorageClass parameter %q", key)
		}
	}

	return normalized, nil
}

func volumeMetadata(params map[string]string) map[string]string {
	metadata := make(map[string]string)
	for key, value := range params {
		if !strings.HasPrefix(key, paramMetadataPrefix) {
			continue
		}
		metadataKey := strings.TrimPrefix(key, paramMetadataPrefix)
		metadata[metadataKey] = value
	}

	return metadata
}
