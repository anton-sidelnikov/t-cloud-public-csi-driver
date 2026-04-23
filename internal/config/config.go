package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	defaultDriverName = "csi.evs.tcloudpublic.com"
	defaultEndpoint   = "unix:///var/lib/kubelet/plugins/csi.evs.tcloudpublic.com/csi.sock"
	defaultBackend    = "evs"
)

type Config struct {
	Backend          string `env:"CSI_BACKEND" envDefault:"evs"`
	DriverName       string `env:"CSI_DRIVER_NAME" envDefault:"csi.evs.tcloudpublic.com"`
	Endpoint         string `env:"CSI_ENDPOINT" envDefault:"unix:///var/lib/kubelet/plugins/csi.evs.tcloudpublic.com/csi.sock"`
	NodeID           string `env:"CSI_NODE_ID"`
	Region           string `env:"OS_REGION,required,notEmpty"`
	AvailabilityZone string `env:"OS_AVAILABILITY_ZONE"`

	AuthURL     string `env:"OS_AUTH_URL,required,notEmpty"`
	DomainName  string `env:"OS_DOMAIN_NAME"`
	UserName    string `env:"OS_USERNAME,required,notEmpty"`
	Password    string `env:"OS_PASSWORD,required,notEmpty"`
	ProjectID   string `env:"OS_PROJECT_ID"`
	ProjectName string `env:"OS_PROJECT_NAME"`

	MaxVolumesPerNode int64         `env:"CSI_MAX_VOLUMES_PER_NODE" envDefault:"32"`
	Timeout           time.Duration `env:"CSI_REQUEST_TIMEOUT" envDefault:"2m"`
}

func FromEnv() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse environment config: %w", err)
	}

	cfg.normalize()
	if cfg.Region == "" {
		return Config{}, fmt.Errorf("OS_REGION is required")
	}
	if cfg.AuthURL == "" {
		return Config{}, fmt.Errorf("OS_AUTH_URL is required")
	}
	if cfg.UserName == "" {
		return Config{}, fmt.Errorf("OS_USERNAME is required")
	}
	if strings.TrimSpace(cfg.Password) == "" {
		return Config{}, fmt.Errorf("OS_PASSWORD is required")
	}
	if cfg.ProjectID == "" && cfg.ProjectName == "" {
		return Config{}, fmt.Errorf("either OS_PROJECT_ID or OS_PROJECT_NAME is required")
	}

	return cfg, nil
}

func (c *Config) normalize() {
	c.Backend = strings.TrimSpace(c.Backend)
	c.DriverName = strings.TrimSpace(c.DriverName)
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.NodeID = strings.TrimSpace(c.NodeID)
	c.Region = strings.TrimSpace(c.Region)
	c.AvailabilityZone = strings.TrimSpace(c.AvailabilityZone)
	c.AuthURL = strings.TrimSpace(c.AuthURL)
	c.DomainName = strings.TrimSpace(c.DomainName)
	c.UserName = strings.TrimSpace(c.UserName)
	c.ProjectID = strings.TrimSpace(c.ProjectID)
	c.ProjectName = strings.TrimSpace(c.ProjectName)
}
