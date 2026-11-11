package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"t-cloud-public-csi-driver/internal/config"
)

const (
	defaultMetadataURL      = "http://169.254.169.254/openstack/latest/meta_data.json"
	defaultMetadataAttempts = 3
	defaultMetadataBackoff  = 2 * time.Second
	metadataRequestTimeout  = 5 * time.Second
)

type nodeZoneResolver interface {
	Resolve(ctx context.Context) (string, error)
}

// metadataZoneResolver reads the availability zone of the current ECS instance from the
// metadata service and falls back to OS_AVAILABILITY_ZONE when the service is unavailable.
type metadataZoneResolver struct {
	url      string
	client   *http.Client
	fallback string
	attempts int
	backoff  time.Duration
	logger   *slog.Logger

	mu     sync.Mutex
	cached string
}

func newNodeZoneResolver(cfg config.Config) nodeZoneResolver {
	return &metadataZoneResolver{
		url:      defaultMetadataURL,
		client:   &http.Client{Timeout: metadataRequestTimeout},
		fallback: cfg.AvailabilityZone,
		attempts: defaultMetadataAttempts,
		backoff:  defaultMetadataBackoff,
		logger:   slog.Default().With("component", "node-zone-resolver"),
	}
}

func (r *metadataZoneResolver) Resolve(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached != "" {
		return r.cached, nil
	}

	logger := r.logger
	if logger == nil {
		logger = slog.Default().With("component", "node-zone-resolver")
	}

	zone, err := r.fetchWithRetry(ctx)
	switch {
	case err == nil:
		if r.fallback != "" && r.fallback != zone {
			logger.Warn("OS_AVAILABILITY_ZONE differs from the instance availability zone, using the instance zone", "configured_zone", r.fallback, "instance_zone", zone)
		}
		logger.Info("resolved node availability zone", "source", "metadata", "availability_zone", zone)
		r.cached = zone
	case r.fallback != "":
		logger.Warn("failed to read availability zone from metadata service, using OS_AVAILABILITY_ZONE", "error", err, "availability_zone", r.fallback)
		r.cached = r.fallback
	default:
		return "", fmt.Errorf("availability zone is unknown: metadata service failed (%v) and OS_AVAILABILITY_ZONE is not set", err)
	}

	return r.cached, nil
}

func (r *metadataZoneResolver) fetchWithRetry(ctx context.Context) (string, error) {
	attempts := max(r.attempts, 1)
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(r.backoff):
			}
		}
		zone, err := r.fetch(ctx)
		if err == nil {
			return zone, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func (r *metadataZoneResolver) fetch(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return "", err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata service returned %s", resp.Status)
	}

	var metadata struct {
		AvailabilityZone string `json:"availability_zone"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&metadata); err != nil {
		return "", fmt.Errorf("decode metadata: %w", err)
	}
	zone := strings.TrimSpace(metadata.AvailabilityZone)
	if zone == "" {
		return "", fmt.Errorf("metadata does not contain availability_zone")
	}
	return zone, nil
}
