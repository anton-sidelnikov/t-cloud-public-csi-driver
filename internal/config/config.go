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

	// ModeNode is set in node plugins; the SFS General Purpose node plugin then needs no
	// cloud credentials. ModeAll is the default.
	ModeAll  = "all"
	ModeNode = "node"
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
	Mode             string `env:"CSI_MODE" envDefault:"all"`
	DriverName       string `env:"CSI_DRIVER_NAME"`
	Endpoint         string `env:"CSI_ENDPOINT"`
	NodeID           string `env:"CSI_NODE_ID"`
	Region           string `env:"OS_REGION,required,notEmpty"`
	AvailabilityZone string `env:"OS_AVAILABILITY_ZONE"`

	AuthURL     string `env:"OS_AUTH_URL"`
	DomainName  string `env:"OS_DOMAIN_NAME"`
	UserName    string `env:"OS_USERNAME"`
	Password    string `env:"OS_PASSWORD"`
	ProjectID   string `env:"OS_PROJECT_ID"`
	ProjectName string `env:"OS_PROJECT_NAME"`

	// Optional AK/SK for General Purpose SFS. When set, OS_AUTH_URL, OS_USERNAME, OS_PASSWORD
	// and the project are not needed; when unset, temporary AK/SK are issued with them.
	AccessKey     string `env:"OS_ACCESS_KEY"`
	SecretKey     string `env:"OS_SECRET_KEY"`
	SecurityToken string `env:"OS_SECURITY_TOKEN"`

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
	if cfg.Mode != ModeAll && cfg.Mode != ModeNode {
		return Config{}, fmt.Errorf("unsupported CSI_MODE %q", cfg.Mode)
	}
	if cfg.Region == "" {
		return Config{}, fmt.Errorf("OS_REGION is required")
	}
	if (cfg.AccessKey == "") != (cfg.SecretKey == "") {
		return Config{}, fmt.Errorf("OS_ACCESS_KEY and OS_SECRET_KEY must be set together")
	}
	if cfg.SecurityToken != "" && cfg.AccessKey == "" {
		return Config{}, fmt.Errorf("OS_SECURITY_TOKEN requires OS_ACCESS_KEY and OS_SECRET_KEY")
	}
	if cfg.UsesAccessKeys() || !cfg.NeedsCredentials() {
		return cfg, nil
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

// UsesAccessKeys reports whether the backend authenticates only with OS_ACCESS_KEY and
// OS_SECRET_KEY. Only General Purpose SFS supports this.
func (c Config) UsesAccessKeys() bool {
	return c.Backend == BackendGPSFS && c.AccessKey != ""
}

// NeedsCredentials reports whether the driver calls the cloud API. The SFS General Purpose
// node plugin derives the mount source from the volume and does not.
func (c Config) NeedsCredentials() bool {
	return c.Backend != BackendGPSFS || c.Mode != ModeNode
}

func defaultEndpoint(driverName string) string {
	return "unix:///var/lib/kubelet/plugins/" + driverName + "/csi.sock"
}

func (c *Config) normalize() {
	c.Backend = strings.TrimSpace(c.Backend)
	c.Mode = strings.TrimSpace(c.Mode)
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
	c.AccessKey = strings.TrimSpace(c.AccessKey)
	c.SecretKey = strings.TrimSpace(c.SecretKey)
	c.SecurityToken = strings.TrimSpace(c.SecurityToken)
	c.VPCID = strings.TrimSpace(c.VPCID)
	c.SubnetID = strings.TrimSpace(c.SubnetID)
	c.SecurityGroupID = strings.TrimSpace(c.SecurityGroupID)
}
