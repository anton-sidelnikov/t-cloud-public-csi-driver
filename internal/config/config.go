package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDriverName = "csi.evs.tcloudpublic.com"
	defaultEndpoint   = "unix:///var/lib/kubelet/plugins/csi.evs.tcloudpublic.com/csi.sock"
)

type Config struct {
	DriverName       string
	Endpoint         string
	NodeID           string
	Region           string
	AvailabilityZone string

	AuthURL     string
	DomainName  string
	UserName    string
	Password    string
	ProjectID   string
	ProjectName string

	MaxVolumesPerNode int64
	Timeout           time.Duration
}

func FromEnv() (Config, error) {
	cfg := Config{
		DriverName:        envOrDefault("CSI_DRIVER_NAME", defaultDriverName),
		Endpoint:          envOrDefault("CSI_ENDPOINT", defaultEndpoint),
		NodeID:            strings.TrimSpace(os.Getenv("CSI_NODE_ID")),
		Region:            strings.TrimSpace(os.Getenv("OS_REGION")),
		AvailabilityZone:  strings.TrimSpace(os.Getenv("OS_AVAILABILITY_ZONE")),
		AuthURL:           strings.TrimSpace(os.Getenv("OS_AUTH_URL")),
		DomainName:        strings.TrimSpace(os.Getenv("OS_DOMAIN_NAME")),
		UserName:          strings.TrimSpace(os.Getenv("OS_USERNAME")),
		Password:          os.Getenv("OS_PASSWORD"),
		ProjectID:         strings.TrimSpace(os.Getenv("OS_PROJECT_ID")),
		ProjectName:       strings.TrimSpace(os.Getenv("OS_PROJECT_NAME")),
		MaxVolumesPerNode: envInt64OrDefault("CSI_MAX_VOLUMES_PER_NODE", 32),
		Timeout:           envDurationOrDefault("CSI_REQUEST_TIMEOUT", 2*time.Minute),
	}

	if cfg.Region == "" {
		return Config{}, fmt.Errorf("OS_REGION is required")
	}
	if cfg.AuthURL == "" {
		return Config{}, fmt.Errorf("OS_AUTH_URL is required")
	}
	if cfg.UserName == "" {
		return Config{}, fmt.Errorf("OS_USERNAME is required")
	}
	if cfg.Password == "" {
		return Config{}, fmt.Errorf("OS_PASSWORD is required")
	}
	if cfg.ProjectID == "" && cfg.ProjectName == "" {
		return Config{}, fmt.Errorf("either OS_PROJECT_ID or OS_PROJECT_NAME is required")
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt64OrDefault(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}

	return value
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}

	return value
}
