package evs

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

const gibibyte int64 = 1024 * 1024 * 1024

func sizeBytesToGiB(sizeBytes int64) int64 {
	sizeGiB := int64(math.Ceil(float64(sizeBytes) / float64(gibibyte)))
	if sizeGiB < 1 {
		sizeGiB = 1
	}

	return sizeGiB
}

type Service struct {
	cfg        config.Config
	blockStore *golangsdk.ServiceClient
	compute    *golangsdk.ServiceClient
}

func NewService(cfg config.Config, authOpts golangsdk.AuthOptions) (*Service, error) {
	provider, err := openstack.AuthenticatedClient(authOpts)
	if err != nil {
		return nil, fmt.Errorf("authenticate against T Cloud Public: %w", err)
	}

	blockStore, err := openstack.NewBlockStorageV3(provider, golangsdk.EndpointOpts{Region: cfg.Region})
	if err != nil {
		return nil, fmt.Errorf("init block storage client: %w", err)
	}

	compute, err := openstack.NewComputeV2(provider, golangsdk.EndpointOpts{Region: cfg.Region})
	if err != nil {
		return nil, fmt.Errorf("init compute client: %w", err)
	}

	return &Service{
		cfg:        cfg,
		blockStore: blockStore,
		compute:    compute,
	}, nil
}

func (s *Service) CreateVolume(ctx context.Context, req backend.CreateVolumeRequest) (*backend.Volume, error) {
	body := map[string]any{
		"volume": map[string]any{
			"name":              req.Name,
			"size":              sizeBytesToGiB(req.SizeBytes),
			"availability_zone": req.AvailabilityZone,
			"volume_type":       req.VolumeType,
			"description":       req.Description,
			"metadata":          req.Metadata,
		},
	}

	var resp struct {
		Volume volumePayload `json:"volume"`
	}
	if err := s.doJSON(ctx, http.MethodPost, s.blockStore, s.blockStore.ServiceURL("volumes"), body, &resp); err != nil {
		return nil, err
	}

	volume, err := s.waitForVolumeStatus(ctx, resp.Volume.ID, "available")
	if err != nil {
		return nil, err
	}

	return volume, nil
}

func (s *Service) DeleteVolume(ctx context.Context, volumeID string) error {
	if err := s.doJSON(ctx, http.MethodDelete, s.blockStore, s.blockStore.ServiceURL("volumes", volumeID), nil, nil); err != nil {
		return err
	}
	return nil
}

func (s *Service) AttachVolume(ctx context.Context, volumeID, serverID string) (*backend.Attachment, error) {
	body := map[string]any{
		"volumeAttachment": map[string]any{
			"volumeId": volumeID,
		},
	}

	var resp struct {
		Attachment attachmentPayload `json:"volumeAttachment"`
	}
	if err := s.doJSON(ctx, http.MethodPost, s.compute, s.compute.ServiceURL("servers", serverID, "os-volume_attachments"), body, &resp); err != nil {
		return nil, err
	}

	return &backend.Attachment{
		ID:       resp.Attachment.ID,
		ServerID: serverID,
		VolumeID: volumeID,
		Device:   resp.Attachment.Device,
	}, nil
}

func (s *Service) DetachVolume(ctx context.Context, volumeID, serverID string) error {
	volume, err := s.GetVolume(ctx, volumeID)
	if err != nil {
		return err
	}

	for _, attachment := range volume.Attachments {
		if attachment.ServerID != serverID {
			continue
		}

		return s.doJSON(ctx, http.MethodDelete, s.compute, s.compute.ServiceURL("servers", serverID, "os-volume_attachments", attachment.ID), nil, nil)
	}

	return nil
}

func (s *Service) ExpandVolume(ctx context.Context, volumeID string, newSizeBytes int64) (int64, error) {
	body := map[string]any{
		"os-extend": map[string]any{
			"new_size": sizeBytesToGiB(newSizeBytes),
		},
	}

	if err := s.doJSON(ctx, http.MethodPost, s.blockStore, s.blockStore.ServiceURL("volumes", volumeID, "action"), body, nil); err != nil {
		return 0, err
	}

	volume, err := s.waitForVolumeStatus(ctx, volumeID, "available")
	if err != nil {
		return 0, err
	}

	return volume.SizeBytes, nil
}

func (s *Service) GetVolume(ctx context.Context, volumeID string) (*backend.Volume, error) {
	var resp struct {
		Volume volumePayload `json:"volume"`
	}
	if err := s.doJSON(ctx, http.MethodGet, s.blockStore, s.blockStore.ServiceURL("volumes", volumeID), nil, &resp); err != nil {
		return nil, err
	}

	return resp.Volume.toDomain(), nil
}

func (s *Service) waitForVolumeStatus(ctx context.Context, volumeID, desired string) (*backend.Volume, error) {
	deadline := time.Now().Add(s.cfg.Timeout)

	for {
		volume, err := s.GetVolume(ctx, volumeID)
		if err != nil {
			return nil, err
		}
		if volume.Status == desired {
			return volume, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for volume %s to reach %s, last state %s", volumeID, desired, volume.Status)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *Service) doJSON(ctx context.Context, method string, client *golangsdk.ServiceClient, url string, body any, out any) error {
	_ = ctx

	headers := map[string]string{
		"Accept": "application/json",
	}

	options := &golangsdk.RequestOpts{
		MoreHeaders:  headers,
		OkCodes:      okCodes(method),
		JSONBody:     body,
		JSONResponse: out,
	}

	_, err := client.Request(method, url, options)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, url, err)
	}

	return nil
}

func okCodes(method string) []int {
	switch method {
	case http.MethodPost:
		return []int{http.StatusAccepted, http.StatusCreated, http.StatusOK}
	case http.MethodDelete:
		return []int{http.StatusAccepted, http.StatusNoContent, http.StatusNotFound}
	default:
		return []int{http.StatusOK}
	}
}

type volumePayload struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Status           string              `json:"status"`
	AvailabilityZone string              `json:"availability_zone"`
	VolumeType       string              `json:"volume_type"`
	SizeGiB          int64               `json:"size"`
	Attachments      []attachmentPayload `json:"attachments"`
}

func (p volumePayload) toDomain() *backend.Volume {
	attachments := make([]backend.Attachment, 0, len(p.Attachments))
	for _, attachment := range p.Attachments {
		attachments = append(attachments, backend.Attachment{
			ID:       attachment.ID,
			ServerID: attachment.ServerID,
			VolumeID: attachment.VolumeID,
			Device:   attachment.Device,
		})
	}

	return &backend.Volume{
		ID:               p.ID,
		Name:             p.Name,
		Status:           p.Status,
		AvailabilityZone: p.AvailabilityZone,
		VolumeType:       p.VolumeType,
		SizeBytes:        p.SizeGiB * gibibyte,
		Attachments:      attachments,
	}
}

type attachmentPayload struct {
	ID       string `json:"id"`
	ServerID string `json:"server_id"`
	VolumeID string `json:"volume_id"`
	Device   string `json:"device"`
}
