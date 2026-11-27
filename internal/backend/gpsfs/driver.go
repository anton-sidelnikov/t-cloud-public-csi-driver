// Package gpsfs implements the CSI behavior of SFS General Purpose file systems.
package gpsfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

const (
	driverName = "csi.gpsfs.tcloudpublic.com"

	ContextExportLocation = "exportLocation"
	contextName           = "name"

	paramVPCID  = "vpcId"
	paramFSType = "csi.storage.k8s.io/fstype"

	fsTypeNFS = "nfs"

	// hashedNamePrefix starts the file system name of CSI volume names that are not valid
	// file system names.
	hashedNamePrefix = "csi-"

	// subPathSeparator separates the file system name from the subdirectory in the volume
	// ID of a static volume, for example "<name>:data/app".
	subPathSeparator = ":"
)

// mountOptions are the NFS options recommended by the OTC documentation, plus nosharecache
// so that every mount of a file system gets its own NFS superblock (see SFS Turbo).
var mountOptions = []string{"vers=3", "timeo=600", "noresvport", "nolock", "tcp", "nosharecache"}

// fileSystemNamePattern accepts names of 3 to 63 lowercase letters, digits and hyphens that
// start and end with a letter or digit. Periods, which file system names may contain, are
// not accepted, because the name is part of the mount host name.
var fileSystemNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// FileSystemName returns the file system name of a CSI volume name. Valid names, such as
// the external-provisioner default "pvc-<uuid>", are used as they are; other names are
// replaced by a hash so that retries of the same request use the same name.
func FileSystemName(volumeName string) string {
	if fileSystemNamePattern.MatchString(volumeName) {
		return volumeName
	}
	sum := sha256.Sum256([]byte(volumeName))
	return hashedNamePrefix + hex.EncodeToString(sum[:])[:40]
}

// ParseVolumeID splits a volume ID into the file system name and an optional subdirectory.
// Dynamic volumes are whole file systems, static volumes may be "<name>:<subdirectory>".
// The subdirectory is returned without leading and trailing slashes.
func ParseVolumeID(volumeID string) (name, subPath string, err error) {
	name, rawSubPath, hasSubPath := strings.Cut(volumeID, subPathSeparator)
	if !fileSystemNamePattern.MatchString(name) {
		return "", "", fmt.Errorf("volume ID %q must be an SFS General Purpose file system name (3-63 lowercase letters, digits and hyphens), optionally followed by %q and a subdirectory", volumeID, subPathSeparator)
	}
	if !hasSubPath {
		return name, "", nil
	}
	segments := strings.Split(strings.Trim(rawSubPath, "/"), "/")
	if slices.Contains(segments, "..") || slices.Contains(segments, ".") || slices.Contains(segments, "") {
		return "", "", fmt.Errorf("volume ID %q has an invalid subdirectory %q", volumeID, rawSubPath)
	}
	return name, path.Join(segments...), nil
}

// MountSource returns the NFS export of a file system.
func MountSource(region, name string) string {
	return name + ".sfs3." + region + ".otc.t-systems.com:/" + name
}

type Driver struct {
	region string
}

func New(region string) *Driver {
	return &Driver{region: region}
}

func (d *Driver) Name() string {
	return driverName
}

// TopologyKey is empty because a file system is reachable from every node of an allowed VPC.
func (d *Driver) TopologyKey() string {
	return ""
}

func (d *Driver) ValidateVolumeCapability(capability *csi.VolumeCapability) error {
	if capability == nil {
		return fmt.Errorf("nil capability")
	}
	if capability.GetBlock() != nil {
		return fmt.Errorf("block access type is not supported by SFS General Purpose")
	}
	mount := capability.GetMount()
	if mount == nil {
		return fmt.Errorf("mount access type is required")
	}
	if fsType := strings.TrimSpace(mount.GetFsType()); fsType != "" && fsType != fsTypeNFS {
		return fmt.Errorf("fsType %q is not supported by SFS General Purpose, only %q", fsType, fsTypeNFS)
	}
	if capability.GetAccessMode() == nil {
		return fmt.Errorf("access mode is required")
	}
	if capability.GetAccessMode().GetMode() == csi.VolumeCapability_AccessMode_UNKNOWN {
		return fmt.Errorf("access mode %s is not supported", capability.GetAccessMode().GetMode().String())
	}
	return nil
}

// BuildCreateVolumeRequest keeps the requested capacity: file systems have no size, the
// capacity is only reported back.
func (d *Driver) BuildCreateVolumeRequest(cfg config.Config, req *csi.CreateVolumeRequest) (backend.CreateVolumeRequest, error) {
	if req.GetName() == "" {
		return backend.CreateVolumeRequest{}, fmt.Errorf("name is required")
	}
	if req.GetVolumeContentSource() != nil {
		return backend.CreateVolumeRequest{}, fmt.Errorf("SFS General Purpose does not support volume content sources")
	}
	required, limit := req.GetCapacityRange().GetRequiredBytes(), req.GetCapacityRange().GetLimitBytes()
	if required < 0 || limit < 0 {
		return backend.CreateVolumeRequest{}, fmt.Errorf("capacity must not be negative")
	}
	sizeBytes := required
	if sizeBytes == 0 {
		sizeBytes = limit
	}

	var vpcID string
	for key, value := range req.GetParameters() {
		value = strings.TrimSpace(value)
		switch key {
		case paramVPCID:
			vpcID = value
		case paramFSType:
			if value != "" && value != fsTypeNFS {
				return backend.CreateVolumeRequest{}, fmt.Errorf("StorageClass parameter %s=%q is not supported by SFS General Purpose, only %q", key, value, fsTypeNFS)
			}
		default:
			return backend.CreateVolumeRequest{}, fmt.Errorf("unsupported SFS General Purpose StorageClass parameter %q", key)
		}
	}
	if vpcID == "" {
		vpcID = cfg.VPCID
	}
	if vpcID == "" {
		return backend.CreateVolumeRequest{}, fmt.Errorf("missing SFS General Purpose settings: %s (or OS_VPC_ID)", paramVPCID)
	}

	return backend.CreateVolumeRequest{
		Name:      FileSystemName(req.GetName()),
		SizeBytes: sizeBytes,
		VPCID:     vpcID,
	}, nil
}

func (d *Driver) VolumeContext(vol *backend.Volume) map[string]string {
	return map[string]string{
		ContextExportLocation: vol.ExportLocation,
		contextName:           vol.Name,
	}
}

// ShareMount mounts the export of the volume context, or the export derived from the
// file system name for static volumes.
func (d *Driver) ShareMount(volumeID string, volumeContext map[string]string) (backend.ShareMount, error) {
	name, subPath, err := ParseVolumeID(volumeID)
	if err != nil {
		return backend.ShareMount{}, err
	}
	source := strings.TrimSpace(volumeContext[ContextExportLocation])
	if source == "" {
		source = MountSource(d.region, name)
	}
	if subPath != "" {
		source = strings.TrimRight(source, "/") + "/" + subPath
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
		csi.ControllerServiceCapability_RPC_GET_VOLUME,
		csi.ControllerServiceCapability_RPC_VOLUME_CONDITION,
	}
}

func (d *Driver) NodeCapabilities() []csi.NodeServiceCapability_RPC_Type {
	return []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
	}
}
