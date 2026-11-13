package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	BackendEVS      = "evs"
	BackendSFSTurbo = "sfsturbo"
	BackendGPSFS    = "gpsfs"

	defaultBackend = BackendEVS
)

// backendDriverNames maps each CSI_BACKEND to its default CSI driver name. Each backend
// runs as a separate CSI driver so that EVS and SFS can be deployed side by side.
var backendDriverNames = map[string]string{
	BackendEVS:      "csi.evs.tcloudpublic.com",
	BackendSFSTurbo: "csi.sfsturbo.tcloudpublic.com",
	BackendGPSFS:    "csi.gpsfs.tcloudpublic.com",
}

type Config struct {
	Backend          string `env:"CSI_BACKEND" envDefault:"evs"`
	DriverName       string `env:"CSI_DRIVER_NAME"`
	Endpoint         string `env:"CSI_ENDPOINT"`
	NodeID           string `env:"CSI_NODE_ID"`
	Region           string `env:"OS_REGION,required,notEmpty"`
	AvailabilityZone string `env:"OS_AVAILABILITY_ZONE"`

	AuthURL     string `env:"OS_AUTH_URL,required,notEmpty"`
	DomainName  string `env:"OS_DOMAIN_NAME"`
	UserName    string `env:"OS_USERNAME,required,notEmpty"`
	Password    string `env:"OS_PASSWORD,required,notEmpty"`
	ProjectID   string `env:"OS_PROJECT_ID"`
	ProjectName string `env:"OS_PROJECT_NAME"`

	// Default network for SFS file shares; StorageClass parameters take precedence.
	VPCID           string `env:"OS_VPC_ID"`
	SubnetID        string `env:"OS_SUBNET_ID"`
	SecurityGroupID string `env:"OS_SECURITY_GROUP_ID"`

	MaxVolumesPerNode int64         `env:"CSI_MAX_VOLUMES_PER_NODE" envDefault:"32"`
	Timeout           time.Duration `env:"CSI_REQUEST_TIMEOUT" envDefault:"2m"`
}

func FromEnv() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse environment config: %w", err)
	}

	cfg.normalize()
	defaultDriverName, ok := backendDriverNames[cfg.Backend]
	if !ok {
		return Config{}, fmt.Errorf("unsupported CSI_BACKEND %q", cfg.Backend)
	}
	if cfg.DriverName == "" {
		cfg.DriverName = defaultDriverName
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultEndpoint(cfg.DriverName)
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
	if strings.TrimSpace(cfg.Password) == "" {
		return Config{}, fmt.Errorf("OS_PASSWORD is required")
	}
	if cfg.ProjectID == "" && cfg.ProjectName == "" {
		return Config{}, fmt.Errorf("either OS_PROJECT_ID or OS_PROJECT_NAME is required")
	}

	return cfg, nil
}

func defaultEndpoint(driverName string) string {
	return "unix:///var/lib/kubelet/plugins/" + driverName + "/csi.sock"
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
	c.VPCID = strings.TrimSpace(c.VPCID)
	c.SubnetID = strings.TrimSpace(c.SubnetID)
	c.SecurityGroupID = strings.TrimSpace(c.SecurityGroupID)
}
