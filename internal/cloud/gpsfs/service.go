package gpsfs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack/gpfs"

	"t-cloud-public-csi-driver/internal/backend"
	backendgpsfs "t-cloud-public-csi-driver/internal/backend/gpsfs"
	"t-cloud-public-csi-driver/internal/config"
)

// Error codes of the General Purpose SFS API.
const (
	codeNoSuchFileSystem   = "NoSuchBucket"
	codeNoSuchAccessRules  = "FsNoSuchSfsAclException"
	codeAlreadyOwnedByYou  = "BucketAlreadyOwnedByYou"
	codeNameTaken          = "BucketAlreadyExists"
	codeFileSystemNotEmpty = "BucketNotEmpty"
	codeTooManyFileSystems = "TooManyBuckets"

	bucketTypeSFS = "SFS"
	redundancy3AZ = "3az"
)

// fsAPI is the subset of the General Purpose SFS API used by the service.
type fsAPI interface {
	ListFS(ctx context.Context) ([]gpfs.Bucket, error)
	CreateFS(ctx context.Context, name, region string) error
	DeleteFS(ctx context.Context, name string) error
	GetAccessRules(ctx context.Context, name string) ([]gpfs.AccessRule, error)
	PutAccessRules(ctx context.Context, name string, rules []gpfs.AccessRule) error
}

type sdkAPI struct {
	clients *Clients
}

func (a sdkAPI) ListFS(ctx context.Context) ([]gpfs.Bucket, error) {
	client, err := a.clients.Client(ctx)
	if err != nil {
		return nil, err
	}
	out, err := client.ListFS(&gpfs.ListFSInput{BucketType: bucketTypeSFS})
	if err != nil {
		return nil, err
	}
	return out.Buckets, nil
}

func (a sdkAPI) CreateFS(ctx context.Context, name, region string) error {
	client, err := a.clients.Client(ctx)
	if err != nil {
		return err
	}
	_, err = client.CreateFS(&gpfs.CreateFSInput{
		FSName: name, BucketType: bucketTypeSFS, Redundancy: redundancy3AZ,
		BucketLocation: gpfs.BucketLocation{Location: region},
	})
	return err
}

func (a sdkAPI) DeleteFS(ctx context.Context, name string) error {
	client, err := a.clients.Client(ctx)
	if err != nil {
		return err
	}
	_, err = client.DeleteFS(name)
	return err
}

func (a sdkAPI) GetAccessRules(ctx context.Context, name string) ([]gpfs.AccessRule, error) {
	client, err := a.clients.Client(ctx)
	if err != nil {
		return nil, err
	}
	out, err := client.GetFSAccessRules(name)
	if err != nil {
		return nil, err
	}
	return out.Statement, nil
}

func (a sdkAPI) PutAccessRules(ctx context.Context, name string, rules []gpfs.AccessRule) error {
	client, err := a.clients.Client(ctx)
	if err != nil {
		return err
	}
	_, err = client.CreateFSAccessRules(&gpfs.CreateFSAccessRulesInput{FSName: name, Statement: rules})
	return err
}

type Service struct {
	cfg    config.Config
	api    fsAPI
	logger *slog.Logger
}

func NewService(cfg config.Config, authOpts golangsdk.AuthOptions) (*Service, error) {
	clients, err := NewClients(cfg, authOpts)
	if err != nil {
		return nil, err
	}
	return newService(cfg, sdkAPI{clients: clients}), nil
}

func newService(cfg config.Config, api fsAPI) *Service {
	return &Service{
		cfg:    cfg,
		api:    api,
		logger: slog.Default().With("component", "gpsfs-service", "region", cfg.Region),
	}
}

// CreateVolume is idempotent by name: the API accepts creating an own file system again.
// The file system is then made accessible from the requested VPC.
func (s *Service) CreateVolume(ctx context.Context, req backend.CreateVolumeRequest) (*backend.Volume, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	logger := s.logger.With("file_system", req.Name, "vpc_id", req.VPCID)

	logger.Info("creating SFS General Purpose file system")
	err := s.api.CreateFS(ctx, req.Name, s.cfg.Region)
	switch {
	case err == nil, hasCode(err, codeAlreadyOwnedByYou):
	case hasCode(err, codeNameTaken):
		return nil, fmt.Errorf("SFS General Purpose file system name %s is used by another account: %w", req.Name, backend.ErrVolumeAlreadyExists)
	case hasCode(err, codeTooManyFileSystems):
		return nil, fmt.Errorf("create SFS General Purpose file system %s: the file system quota is exhausted: %w", req.Name, err)
	default:
		logger.Error("create SFS General Purpose file system request failed", "error", err)
		return nil, fmt.Errorf("create SFS General Purpose file system %s: %w", req.Name, err)
	}

	if err := s.allowVPC(ctx, req.Name, req.VPCID); err != nil {
		logger.Error("allowing access from the VPC failed", "error", err)
		return nil, err
	}
	logger.Info("SFS General Purpose file system created")
	return &backend.Volume{
		ID:             req.Name,
		Name:           req.Name,
		SizeBytes:      req.SizeBytes,
		ExportLocation: backendgpsfs.MountSource(s.cfg.Region, req.Name),
		Condition:      backend.VolumeCondition{Message: "SFS General Purpose file system is available"},
	}, nil
}

// allowVPC adds read-write access from a VPC to the access rules of a file system. The API
// replaces all rules, so the existing rules are kept.
func (s *Service) allowVPC(ctx context.Context, name, vpcID string) error {
	rules, err := s.api.GetAccessRules(ctx, name)
	if err != nil && !hasCode(err, codeNoSuchAccessRules) {
		return fmt.Errorf("get access rules of SFS General Purpose file system %s: %w", name, err)
	}
	for _, rule := range rules {
		if rule.Condition.SourceVPC == vpcID && rule.Action == gpfs.AccessRuleActionFullControl && rule.Effect == gpfs.AccessRuleEffectAllow {
			return nil
		}
	}
	rules = append(rules, gpfs.AccessRule{
		Action:    gpfs.AccessRuleActionFullControl,
		Effect:    gpfs.AccessRuleEffectAllow,
		Condition: gpfs.AccessRuleCondition{SourceVPC: vpcID},
	})
	s.logger.Info("allowing access to SFS General Purpose file system", "file_system", name, "vpc_id", vpcID)
	if err := s.api.PutAccessRules(ctx, name, rules); err != nil {
		return fmt.Errorf("allow VPC %s to access SFS General Purpose file system %s: %w", vpcID, name, err)
	}
	return nil
}

// DeleteVolume deletes the file system. The API refuses to delete file systems that
// contain data; their contents must be deleted first. Static volumes that are a
// subdirectory of a file system are not deleted.
func (s *Service) DeleteVolume(ctx context.Context, volumeID string) error {
	name, subPath, err := backendgpsfs.ParseVolumeID(volumeID)
	if err != nil {
		s.logger.Warn("not deleting a volume with an invalid ID", "volume_id", volumeID, "error", err)
		return nil
	}
	if subPath != "" {
		return fmt.Errorf("volume %s is the subdirectory %q of SFS General Purpose file system %s, which is not deleted; use the Retain reclaim policy: %w", volumeID, subPath, name, backend.ErrVolumeOperationNotSupported)
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	logger := s.logger.With("file_system", name)

	logger.Info("deleting SFS General Purpose file system")
	err = s.api.DeleteFS(ctx, name)
	switch {
	case err == nil:
		logger.Info("SFS General Purpose file system deleted")
		return nil
	case hasCode(err, codeNoSuchFileSystem):
		logger.Info("SFS General Purpose file system already deleted")
		return nil
	case hasCode(err, codeFileSystemNotEmpty):
		logger.Warn("SFS General Purpose file system is not empty")
		return fmt.Errorf("SFS General Purpose file system %s is not empty; delete its contents first: %w", name, backend.ErrVolumeOperationNotSupported)
	default:
		logger.Error("delete SFS General Purpose file system request failed", "error", err)
		return fmt.Errorf("delete SFS General Purpose file system %s: %w", name, err)
	}
}

// ExpandVolume only reports the requested size, because file systems have no size.
func (s *Service) ExpandVolume(_ context.Context, volumeID string, newSizeBytes int64) (int64, error) {
	if _, _, err := backendgpsfs.ParseVolumeID(volumeID); err != nil {
		return 0, fmt.Errorf("%w: %w", err, backend.ErrVolumeNotFound)
	}
	return newSizeBytes, nil
}

// GetVolume returns the file system of a volume. For a subdirectory volume the file
// system is returned with the volume ID.
func (s *Service) GetVolume(ctx context.Context, volumeID string) (*backend.Volume, error) {
	name, _, err := backendgpsfs.ParseVolumeID(volumeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, backend.ErrVolumeNotFound)
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	fileSystems, err := s.api.ListFS(ctx)
	if err != nil {
		return nil, fmt.Errorf("list SFS General Purpose file systems: %w", err)
	}
	for _, fs := range fileSystems {
		if fs.Name == name {
			return &backend.Volume{
				ID:             volumeID,
				Name:           name,
				ExportLocation: backendgpsfs.MountSource(s.cfg.Region, name),
				Condition:      backend.VolumeCondition{Message: "SFS General Purpose file system is available"},
			}, nil
		}
	}
	return nil, fmt.Errorf("SFS General Purpose file system %s: %w", name, backend.ErrVolumeNotFound)
}

func (s *Service) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.cfg.Timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, s.cfg.Timeout)
}

func hasCode(err error, code string) bool {
	var serviceErr gpfs.Error
	return errors.As(err, &serviceErr) && serviceErr.Code == code
}
