package sfsturbo

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

const (
	driverName = "csi.sfsturbo.tcloudpublic.com"

	gibibyte int64 = 1024 * 1024 * 1024
	// Capacity limits of SFS Turbo Standard and Performance file systems, in GiB.
	MinSizeGiB int64 = 500
	MaxSizeGiB int64 = 32768

	ShareTypeStandard    = "STANDARD"
	ShareTypePerformance = "PERFORMANCE"

	ContextExportLocation   = "exportLocation"
	contextName             = "name"
	contextAvailabilityZone = "availabilityZone"
	contextShareType        = "shareType"

	paramAvailabilityZone = "availabilityZone"
	paramDescription      = "description"
	paramFSType           = "csi.storage.k8s.io/fstype"
	paramKMSKeyID         = "kmsKeyId"
	paramSecurityGroupID  = "securityGroupId"
	paramShareType        = "shareType"
	paramSubnetID         = "subnetId"
	paramVPCID            = "vpcId"

	fsTypeNFS = "nfs"
)

// mountOptions are the NFS options recommended by the OTC SFS Turbo documentation.
var mountOptions = []string{"vers=3", "timeo=600", "noresvport", "nolock", "tcp"}

// SFS Turbo names have 4 to 64 characters, start with a letter, and contain letters, digits, hyphens, and underscores.
var shareNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{3,63}$`)

type Driver struct{}

func New() *Driver {
	return &Driver{}
}

func (d *Driver) Name() string {
	return driverName
}

// TopologyKey is empty because an SFS Turbo share is reachable from every node in its VPC.
func (d *Driver) TopologyKey() string {
	return ""
}

func (d *Driver) ValidateVolumeCapability(capability *csi.VolumeCapability) error {
	if capability == nil {
		return fmt.Errorf("nil capability")
	}
	if capability.GetBlock() != nil {
		return fmt.Errorf("block access type is not supported by SFS Turbo")
	}
	mount := capability.GetMount()
	if mount == nil {
		return fmt.Errorf("mount access type is required")
	}
	if fsType := strings.TrimSpace(mount.GetFsType()); fsType != "" && fsType != fsTypeNFS {
		return fmt.Errorf("fsType %q is not supported by SFS Turbo, only %q", fsType, fsTypeNFS)
	}
	if capability.GetAccessMode() == nil {
		return fmt.Errorf("access mode is required")
	}
	if capability.GetAccessMode().GetMode() == csi.VolumeCapability_AccessMode_UNKNOWN {
		return fmt.Errorf("access mode %s is not supported", capability.GetAccessMode().GetMode().String())
	}
	return nil
}

func (d *Driver) BuildCreateVolumeRequest(cfg config.Config, req *csi.CreateVolumeRequest) (backend.CreateVolumeRequest, error) {
	if !shareNamePattern.MatchString(req.GetName()) {
		return backend.CreateVolumeRequest{}, fmt.Errorf("name %q is not a valid SFS Turbo name: 4-64 characters, starting with a letter, containing letters, digits, hyphens, and underscores", req.GetName())
	}
	if req.GetVolumeContentSource() != nil {
		return backend.CreateVolumeRequest{}, fmt.Errorf("SFS Turbo does not support volume content sources")
	}
	sizeBytes, err := shareSizeBytes(req.GetCapacityRange())
	if err != nil {
		return backend.CreateVolumeRequest{}, err
	}

	params, err := validateAndNormalizeParameters(req.GetParameters())
	if err != nil {
		return backend.CreateVolumeRequest{}, err
	}

	shareType := strings.ToUpper(params[paramShareType])
	switch shareType {
	case "":
		shareType = ShareTypeStandard
	case ShareTypeStandard, ShareTypePerformance:
	default:
		return backend.CreateVolumeRequest{}, fmt.Errorf("StorageClass parameter %s=%q must be %s or %s", paramShareType, params[paramShareType], ShareTypeStandard, ShareTypePerformance)
	}

	createReq := backend.CreateVolumeRequest{
		Name:             req.GetName(),
		SizeBytes:        sizeBytes,
		AvailabilityZone: firstNonEmpty(params[paramAvailabilityZone], cfg.AvailabilityZone),
		VolumeType:       shareType,
		Description:      params[paramDescription],
		VPCID:            firstNonEmpty(params[paramVPCID], cfg.VPCID),
		SubnetID:         firstNonEmpty(params[paramSubnetID], cfg.SubnetID),
		SecurityGroupID:  firstNonEmpty(params[paramSecurityGroupID], cfg.SecurityGroupID),
		KMSKeyID:         params[paramKMSKeyID],
	}

	var missing []string
	for _, field := range []struct{ value, name string }{
		{createReq.AvailabilityZone, paramAvailabilityZone + " (or OS_AVAILABILITY_ZONE)"},
		{createReq.VPCID, paramVPCID + " (or OS_VPC_ID)"},
		{createReq.SubnetID, paramSubnetID + " (or OS_SUBNET_ID)"},
		{createReq.SecurityGroupID, paramSecurityGroupID + " (or OS_SECURITY_GROUP_ID)"},
	} {
		if field.value == "" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		return backend.CreateVolumeRequest{}, fmt.Errorf("missing SFS Turbo settings: %s", strings.Join(missing, ", "))
	}

	return createReq, nil
}

func (d *Driver) VolumeContext(vol *backend.Volume) map[string]string {
	return map[string]string{
		ContextExportLocation:   vol.ExportLocation,
		contextName:             vol.Name,
		contextAvailabilityZone: vol.AvailabilityZone,
		contextShareType:        vol.VolumeType,
	}
}

func (d *Driver) ShareMount(volumeID string, volumeContext map[string]string) (backend.ShareMount, error) {
	source := strings.TrimSpace(volumeContext[ContextExportLocation])
	if source == "" {
		return backend.ShareMount{}, fmt.Errorf("volume %s has no %s in its volume context", volumeID, ContextExportLocation)
	}
	return backend.ShareMount{
		Source:  source,
		FSType:  fsTypeNFS,
		Options: append([]string(nil), mountOptions...),
	}, nil
}

func (d *Driver) ControllerCapabilities() []csi.ControllerServiceCapability_RPC_Type {
	return []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	}
}

func (d *Driver) NodeCapabilities() []csi.NodeServiceCapability_RPC_Type {
	return []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
	}
}

// shareSizeBytes rounds the requested capacity up to whole GiB and to the SFS Turbo minimum.
func shareSizeBytes(capacity *csi.CapacityRange) (int64, error) {
	required := capacity.GetRequiredBytes()
	limit := capacity.GetLimitBytes()
	if required < 0 || limit < 0 {
		return 0, fmt.Errorf("capacity must not be negative")
	}

	sizeGiB := max(SizeBytesToGiB(required), MinSizeGiB)
	if limit > 0 && sizeGiB*gibibyte > limit {
		return 0, fmt.Errorf("capacity limit %d bytes is below %d GiB (the request rounded up to whole GiB and the SFS Turbo minimum of %d GiB)", limit, sizeGiB, MinSizeGiB)
	}
	if sizeGiB > MaxSizeGiB {
		return 0, fmt.Errorf("requested capacity %d GiB exceeds the SFS Turbo maximum of %d GiB", sizeGiB, MaxSizeGiB)
	}
	return sizeGiB * gibibyte, nil
}

// SizeBytesToGiB rounds bytes up to whole GiB.
func SizeBytesToGiB(sizeBytes int64) int64 {
	return (sizeBytes + gibibyte - 1) / gibibyte
}

func validateAndNormalizeParameters(params map[string]string) (map[string]string, error) {
	normalized := make(map[string]string, len(params))
	for key, value := range params {
		value = strings.TrimSpace(value)
		switch key {
		case paramAvailabilityZone, paramDescription, paramKMSKeyID, paramSecurityGroupID,
			paramShareType, paramSubnetID, paramVPCID:
			normalized[key] = value
		case paramFSType:
			if value != "" && value != fsTypeNFS {
				return nil, fmt.Errorf("StorageClass parameter %s=%q is not supported by SFS Turbo, only %q", key, value, fsTypeNFS)
			}
		default:
			return nil, fmt.Errorf("unsupported SFS Turbo StorageClass parameter %q", key)
		}
	}
	return normalized, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
