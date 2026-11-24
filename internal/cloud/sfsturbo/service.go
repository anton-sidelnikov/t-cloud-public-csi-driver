package sfsturbo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack/sfs_turbo/v1/shares"

	"t-cloud-public-csi-driver/internal/backend"
	backendsfsturbo "t-cloud-public-csi-driver/internal/backend/sfsturbo"
	"t-cloud-public-csi-driver/internal/config"
)

const (
	gibibyte int64 = 1024 * 1024 * 1024

	// File system statuses and substatuses from the OTC SFS Turbo API reference.
	statusCreating     = "100"
	statusActive       = "200"
	statusFailed       = "300"
	statusCreateFailed = "303"
	statusDeleted      = "400"
	statusFrozen       = "800"

	subStatusExpanding    = "121"
	subStatusExpandFailed = "321"

	defaultPollInterval = 5 * time.Second
)

// turboAPI is the subset of the SFS Turbo API used by the service.
type turboAPI interface {
	Create(shares.CreateOpts) (*shares.TurboResponse, error)
	Get(string) (*shares.Turbo, error)
	List() ([]shares.Turbo, error)
	Delete(string) error
	Expand(string, int) error
}

type sdkAPI struct {
	client *golangsdk.ServiceClient
}

func (a sdkAPI) Create(opts shares.CreateOpts) (*shares.TurboResponse, error) {
	return shares.Create(a.client, opts)
}

func (a sdkAPI) Get(id string) (*shares.Turbo, error) {
	return shares.Get(a.client, id)
}

func (a sdkAPI) List() ([]shares.Turbo, error) {
	return shares.List(a.client, shares.ListOpts{})
}

func (a sdkAPI) Delete(id string) error {
	return shares.Delete(a.client, id)
}

func (a sdkAPI) Expand(id string, newSizeGiB int) error {
	return shares.Expand(a.client, id, shares.ExpandOpts{Extend: shares.ExtendOpts{NewSize: newSizeGiB}})
}

type Service struct {
	cfg          config.Config
	api          turboAPI
	pollInterval time.Duration
	logger       *slog.Logger
}

func NewService(cfg config.Config, authOpts golangsdk.AuthOptions) (*Service, error) {
	provider, err := openstack.AuthenticatedClient(authOpts)
	if err != nil {
		return nil, fmt.Errorf("authenticate against T Cloud Public: %w", err)
	}

	client, err := openstack.NewSharedFileSystemTurboV1(provider, golangsdk.EndpointOpts{Region: cfg.Region})
	if err != nil {
		return nil, fmt.Errorf("init SFS Turbo client: %w", err)
	}

	return newService(cfg, sdkAPI{client: client}), nil
}

func newService(cfg config.Config, api turboAPI) *Service {
	return &Service{
		cfg:          cfg,
		api:          api,
		pollInterval: defaultPollInterval,
		logger:       slog.Default().With("component", "sfsturbo-service", "region", cfg.Region),
	}
}

// CreateVolume is idempotent by name: a retry waits for the share created by an earlier call.
func (s *Service) CreateVolume(ctx context.Context, req backend.CreateVolumeRequest) (*backend.Volume, error) {
	sizeGiB := backendsfsturbo.SizeBytesToGiB(req.SizeBytes)
	logger := s.logger.With("share_name", req.Name, "availability_zone", req.AvailabilityZone, "share_type", req.VolumeType, "size_gib", sizeGiB)

	existing, err := s.findByName(req.Name)
	if err != nil {
		return nil, err
	}

	var shareID string
	if existing != nil {
		shareID = existing.ID
		logger = logger.With("share_id", shareID)
		if err := checkExistingShare(existing, req, sizeGiB); err != nil {
			logger.Error("existing SFS Turbo share does not match the request", "error", err)
			return nil, err
		}
		logger.Info("reusing existing SFS Turbo share", "status", existing.Status)
	} else {
		logger.Info("creating SFS Turbo share")
		resp, err := s.api.Create(shares.CreateOpts{
			Name:             req.Name,
			Description:      req.Description,
			AvailabilityZone: req.AvailabilityZone,
			ShareProto:       "NFS",
			ShareType:        req.VolumeType,
			Size:             int(sizeGiB),
			VpcID:            req.VPCID,
			SubnetID:         req.SubnetID,
			SecurityGroupID:  req.SecurityGroupID,
			Metadata: shares.Metadata{
				CryptKeyID: req.KMSKeyID,
				ExpandType: req.ExpandType,
				HpcBW:      req.HPCBandwidth,
			},
		})
		switch {
		case isConflict(err):
			// An earlier request created the share, but the list did not show it yet.
			ex, findErr := s.findByName(req.Name)
			if findErr != nil || ex == nil {
				logger.Warn("SFS Turbo share name already taken but share not listed yet", "error", err)
				return nil, fmt.Errorf("create SFS Turbo share %s: %w", req.Name, err)
			}
			shareID = ex.ID
			logger = logger.With("share_id", shareID)
			if err := checkExistingShare(ex, req, sizeGiB); err != nil {
				logger.Error("existing SFS Turbo share does not match the request", "error", err)
				return nil, err
			}
			logger.Info("reusing SFS Turbo share created by an earlier request", "status", ex.Status)
		case err != nil:
			logger.Error("create SFS Turbo share request failed", "error", err)
			return nil, fmt.Errorf("create SFS Turbo share %s: %w", req.Name, err)
		default:
			shareID = resp.ID
			logger = logger.With("share_id", shareID)
		}
	}

	logger.Info("waiting for SFS Turbo share to become active")
	share, err := s.waitForActive(ctx, shareID)
	if err != nil {
		logger.Error("SFS Turbo share did not become active", "error", err)
		return nil, err
	}
	if err := s.checkEncryption(share, req.KMSKeyID); err != nil {
		logger.Error("SFS Turbo share is not encrypted with the requested KMS key", "error", err)
		return nil, err
	}
	logger.Info("SFS Turbo share created", "export_location", share.ExportLocation)
	return toVolume(share), nil
}

// DeleteVolume returns only after the share is gone, so a failed deletion is retried.
// Static volumes that are a subdirectory of a share are not deleted.
func (s *Service) DeleteVolume(ctx context.Context, volumeID string) error {
	shareID, subPath, err := backendsfsturbo.ParseVolumeID(volumeID)
	if err != nil {
		s.logger.Warn("not deleting a volume with an invalid ID", "volume_id", volumeID, "error", err)
		return nil
	}
	if subPath != "" {
		return fmt.Errorf("volume %s is the subdirectory %q of SFS Turbo share %s, which is not deleted; use the Retain reclaim policy: %w", volumeID, subPath, shareID, backend.ErrVolumeOperationNotSupported)
	}
	logger := s.logger.With("share_id", shareID)

	share, err := s.api.Get(shareID)
	if isNotFound(err) {
		logger.Info("SFS Turbo share already deleted")
		return nil
	}
	if err != nil {
		return fmt.Errorf("get SFS Turbo share %s: %w", shareID, err)
	}
	if share.Status == statusDeleted {
		return nil
	}
	if share.Status == statusCreating {
		return fmt.Errorf("SFS Turbo share %s is still being created", shareID)
	}

	logger.Info("deleting SFS Turbo share", "status", share.Status)
	if err := s.api.Delete(shareID); err != nil && !isNotFound(err) {
		logger.Error("delete SFS Turbo share request failed", "error", err)
		return fmt.Errorf("delete SFS Turbo share %s: %w", shareID, err)
	}

	err = s.poll(ctx, func() (bool, error) {
		share, err := s.api.Get(shareID)
		if isNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return share.Status == statusDeleted, nil
	})
	if err != nil {
		logger.Error("SFS Turbo share was not deleted", "error", err)
		return fmt.Errorf("wait for SFS Turbo share %s deletion: %w", shareID, err)
	}
	logger.Info("SFS Turbo share deleted")
	return nil
}

// ExpandVolume grows the share to at least the requested size. SFS Turbo requires a minimum
// increment (100 GiB, or 1.2 TiB steps for HPC), so the result may be larger than requested.
func (s *Service) ExpandVolume(ctx context.Context, volumeID string, newSizeBytes int64) (int64, error) {
	shareID, subPath, err := backendsfsturbo.ParseVolumeID(volumeID)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", err, backend.ErrVolumeNotFound)
	}
	if subPath != "" {
		return 0, fmt.Errorf("volume %s is the subdirectory %q of SFS Turbo share %s and cannot be expanded: %w", volumeID, subPath, shareID, backend.ErrVolumeOperationNotSupported)
	}
	requestedGiB := backendsfsturbo.SizeBytesToGiB(newSizeBytes)
	logger := s.logger.With("share_id", shareID, "requested_gib", requestedGiB)

	share, err := s.waitForExpansionIdle(ctx, shareID)
	if err != nil {
		return 0, err
	}
	currentGiB, ok := parseSizeGiB(share.Size)
	if !ok {
		return 0, fmt.Errorf("SFS Turbo share %s reports an invalid size %q", shareID, share.Size)
	}
	if currentGiB >= requestedGiB {
		logger.Info("SFS Turbo share already has the requested capacity", "size_gib", currentGiB)
		return currentGiB * gibibyte, nil
	}

	targetGiB, err := backendsfsturbo.CapacityRulesFor(share.ExpandType, share.HpcBW).ExpandSizeGiB(currentGiB, requestedGiB)
	if err != nil {
		return 0, fmt.Errorf("SFS Turbo share %s cannot grow from %d GiB: %w", shareID, currentGiB, err)
	}

	logger.Info("expanding SFS Turbo share", "current_gib", currentGiB, "target_gib", targetGiB)
	if err := s.api.Expand(shareID, int(targetGiB)); err != nil {
		logger.Error("expand SFS Turbo share request failed", "error", err)
		return 0, fmt.Errorf("expand SFS Turbo share %s: %w", shareID, err)
	}

	var (
		sawExpanding bool
		sizeGiB      int64
	)
	err = s.poll(ctx, func() (bool, error) {
		share, err := s.api.Get(shareID)
		if err != nil {
			return false, err
		}
		switch share.SubStatus {
		case subStatusExpanding:
			sawExpanding = true
			return false, nil
		case subStatusExpandFailed:
			// A failure substatus can be left over from an earlier expansion.
			if sawExpanding {
				return false, fmt.Errorf("SFS Turbo share %s expansion failed", shareID)
			}
		}
		sizeGiB, _ = parseSizeGiB(share.Size)
		return share.Status == statusActive && sizeGiB >= targetGiB, nil
	})
	if err != nil {
		logger.Error("SFS Turbo share expansion did not complete", "error", err)
		return 0, fmt.Errorf("wait for SFS Turbo share %s expansion: %w", shareID, err)
	}
	logger.Info("SFS Turbo share expanded", "size_gib", sizeGiB)
	return sizeGiB * gibibyte, nil
}

// GetVolume returns the share of a volume. For a subdirectory volume the share is returned
// with the volume ID.
func (s *Service) GetVolume(_ context.Context, volumeID string) (*backend.Volume, error) {
	shareID, _, err := backendsfsturbo.ParseVolumeID(volumeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, backend.ErrVolumeNotFound)
	}
	share, err := s.api.Get(shareID)
	if isNotFound(err) || (err == nil && share.Status == statusDeleted) {
		return nil, fmt.Errorf("SFS Turbo share %s: %w", shareID, backend.ErrVolumeNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get SFS Turbo share %s: %w", shareID, err)
	}
	vol := toVolume(share)
	vol.ID = volumeID
	return vol, nil
}

func (s *Service) findByName(name string) (*shares.Turbo, error) {
	items, err := s.api.List()
	if err != nil {
		return nil, fmt.Errorf("list SFS Turbo shares: %w", err)
	}
	var found *shares.Turbo
	for i := range items {
		if items[i].Name != name || items[i].Status == statusDeleted {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("found several SFS Turbo shares named %s", name)
		}
		found = &items[i]
	}
	return found, nil
}

// checkExistingShare verifies that a share found by name matches the request, so that
// a retried CreateVolume does not silently return a share with other parameters.
// Attributes the API does not report (empty values) are not compared.
func checkExistingShare(existing *shares.Turbo, req backend.CreateVolumeRequest, sizeGiB int64) error {
	var mismatches []string
	if existingGiB, ok := parseSizeGiB(existing.Size); ok && existingGiB < sizeGiB {
		mismatches = append(mismatches, fmt.Sprintf("size %d GiB is smaller than the requested %d GiB", existingGiB, sizeGiB))
	}
	attrs := []struct {
		name, existing, requested string
	}{
		{"availability zone", existing.AvailabilityZone, req.AvailabilityZone},
		{"VPC", existing.VpcID, req.VPCID},
		{"subnet", existing.SubnetID, req.SubnetID},
		{"security group", existing.SecurityGroupID, req.SecurityGroupID},
		{"KMS key", existing.CryptKeyID, req.KMSKeyID},
		{"expand type", existing.ExpandType, req.ExpandType},
		{"HPC bandwidth", existing.HpcBW, req.HPCBandwidth},
	}
	// The API does not verify the share type of HPC file systems.
	if req.ExpandType != backendsfsturbo.ExpandTypeHPC {
		attrs = append(attrs, struct{ name, existing, requested string }{"share type", existing.ShareType, req.VolumeType})
	}
	for _, attr := range attrs {
		if attr.existing != "" && !strings.EqualFold(attr.existing, attr.requested) {
			mismatches = append(mismatches, fmt.Sprintf("%s is %q, requested %q", attr.name, attr.existing, attr.requested))
		}
	}
	if len(mismatches) == 0 {
		return nil
	}
	return fmt.Errorf("SFS Turbo share %s (%s): %w: %s", existing.Name, existing.ID, backend.ErrVolumeAlreadyExists, strings.Join(mismatches, "; "))
}

func (s *Service) waitForActive(ctx context.Context, shareID string) (*shares.Turbo, error) {
	var share *shares.Turbo
	var lastErr error
	err := s.poll(ctx, func() (bool, error) {
		var err error
		share, err = s.api.Get(shareID)
		// Right after creation the API may answer 404 or 5xx until the share is registered.
		if isTransient(err) {
			s.logger.Debug("SFS Turbo share not available yet", "share_id", shareID, "error", err)
			lastErr = err
			return false, nil
		}
		if err != nil {
			return false, err
		}
		switch share.Status {
		case statusActive:
			return share.ExportLocation != "", nil
		case statusFailed, statusCreateFailed:
			return false, s.cleanupFailedShare(shareID, share.Status)
		case statusFrozen, statusDeleted:
			return false, fmt.Errorf("SFS Turbo share %s is in status %s", shareID, share.Status)
		default:
			return false, nil
		}
	})
	if err != nil {
		if lastErr != nil {
			return nil, fmt.Errorf("wait for SFS Turbo share %s: %w (last error: %v)", shareID, err, lastErr)
		}
		return nil, fmt.Errorf("wait for SFS Turbo share %s: %w", shareID, err)
	}
	return share, nil
}

// checkEncryption verifies that an active share uses the requested KMS key. The API reports
// crypt_key_id only for encrypted shares, so a share without it was created unencrypted.
// Such a share is deleted, because encryption cannot be enabled later and a retry
// must not reuse it.
func (s *Service) checkEncryption(share *shares.Turbo, kmsKeyID string) error {
	if kmsKeyID == "" || strings.EqualFold(share.CryptKeyID, kmsKeyID) {
		return nil
	}
	s.logger.Warn("deleting SFS Turbo share without the requested encryption", "share_id", share.ID, "crypt_key_id", share.CryptKeyID, "kms_key_id", kmsKeyID)
	if err := s.api.Delete(share.ID); err != nil && !isNotFound(err) {
		return fmt.Errorf("SFS Turbo share %s has KMS key %q instead of %q, and deleting it failed: %w", share.ID, share.CryptKeyID, kmsKeyID, err)
	}
	return fmt.Errorf("SFS Turbo share %s has KMS key %q instead of %q and was deleted", share.ID, share.CryptKeyID, kmsKeyID)
}

// cleanupFailedShare deletes a share whose creation failed so that a retry can create it again.
func (s *Service) cleanupFailedShare(shareID, status string) error {
	s.logger.Warn("deleting failed SFS Turbo share", "share_id", shareID, "status", status)
	if err := s.api.Delete(shareID); err != nil && !isNotFound(err) {
		return fmt.Errorf("SFS Turbo share %s failed with status %s, and deleting it failed: %w", shareID, status, err)
	}
	return fmt.Errorf("SFS Turbo share %s failed with status %s and was deleted", shareID, status)
}

func (s *Service) waitForExpansionIdle(ctx context.Context, shareID string) (*shares.Turbo, error) {
	var share *shares.Turbo
	err := s.poll(ctx, func() (bool, error) {
		var err error
		share, err = s.api.Get(shareID)
		if err != nil {
			return false, err
		}
		return share.Status == statusActive && share.SubStatus != subStatusExpanding, nil
	})
	if err != nil {
		return nil, fmt.Errorf("wait for SFS Turbo share %s to become active: %w", shareID, err)
	}
	return share, nil
}

// poll calls check until it reports done, returns an error, or the request timeout expires.
func (s *Service) poll(ctx context.Context, check func() (bool, error)) error {
	deadline := time.Now().Add(s.cfg.Timeout)
	for {
		done, err := check()
		if err != nil || done {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s", s.cfg.Timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.pollInterval):
		}
	}
}

func toVolume(share *shares.Turbo) *backend.Volume {
	sizeGiB, _ := parseSizeGiB(share.Size)
	return &backend.Volume{
		ID:               share.ID,
		Name:             share.Name,
		Status:           share.Status,
		AvailabilityZone: share.AvailabilityZone,
		VolumeType:       share.ShareType,
		SizeBytes:        sizeGiB * gibibyte,
		ExportLocation:   share.ExportLocation,
		ExpandType:       share.ExpandType,
		HPCBandwidth:     share.HpcBW,
		KMSKeyID:         share.CryptKeyID,
		Condition:        shareCondition(share),
	}
}

// shareCondition reports a share as abnormal unless it is available.
func shareCondition(share *shares.Turbo) backend.VolumeCondition {
	switch share.Status {
	case statusActive:
		if share.SubStatus == subStatusExpanding {
			return backend.VolumeCondition{Message: "SFS Turbo share is being expanded"}
		}
		return backend.VolumeCondition{Message: "SFS Turbo share is available"}
	case statusCreating:
		return backend.VolumeCondition{Abnormal: true, Message: "SFS Turbo share is being created"}
	case statusFailed, statusCreateFailed:
		return backend.VolumeCondition{Abnormal: true, Message: fmt.Sprintf("SFS Turbo share failed (status %s)", share.Status)}
	case statusFrozen:
		return backend.VolumeCondition{Abnormal: true, Message: "SFS Turbo share is frozen"}
	default:
		return backend.VolumeCondition{Abnormal: true, Message: fmt.Sprintf("SFS Turbo share has status %s", share.Status)}
	}
}

// parseSizeGiB parses the share size, which the API returns as a decimal string such as "500.00".
func parseSizeGiB(size string) (int64, bool) {
	value, err := strconv.ParseFloat(size, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	return int64(value), true
}

func isNotFound(err error) bool {
	var notFound golangsdk.ErrDefault404
	return errors.As(err, &notFound)
}

func isConflict(err error) bool {
	var conflict golangsdk.ErrDefault409
	return errors.As(err, &conflict)
}

func isTransient(err error) bool {
	var internal golangsdk.ErrDefault500
	var unavailable golangsdk.ErrDefault503
	return isNotFound(err) || errors.As(err, &internal) || errors.As(err, &unavailable)
}
