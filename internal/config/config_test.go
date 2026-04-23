package config

import (
	"testing"
	"time"
)

func TestFromEnvUsesDefaultsAndProjectName(t *testing.T) {
	t.Setenv("OS_REGION", "eu-de")
	t.Setenv("OS_AVAILABILITY_ZONE", "eu-de-01")
	t.Setenv("OS_AUTH_URL", "https://iam.example.com/v3")
	t.Setenv("OS_USERNAME", "tester")
	t.Setenv("OS_PASSWORD", "secret")
	t.Setenv("OS_PROJECT_NAME", "demo")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}

	if cfg.DriverName != defaultDriverName {
		t.Fatalf("unexpected driver name: %q", cfg.DriverName)
	}
	if cfg.Backend != defaultBackend {
		t.Fatalf("unexpected backend: %q", cfg.Backend)
	}
	if cfg.Endpoint != defaultEndpoint {
		t.Fatalf("unexpected endpoint: %q", cfg.Endpoint)
	}
	if cfg.ProjectName != "demo" {
		t.Fatalf("unexpected project name: %q", cfg.ProjectName)
	}
	if cfg.MaxVolumesPerNode != 32 {
		t.Fatalf("unexpected max volumes: %d", cfg.MaxVolumesPerNode)
	}
	if cfg.Timeout != 2*time.Minute {
		t.Fatalf("unexpected timeout: %s", cfg.Timeout)
	}
}

func TestFromEnvParsesOverrides(t *testing.T) {
	t.Setenv("CSI_BACKEND", "evs")
	t.Setenv("CSI_DRIVER_NAME", "custom.csi")
	t.Setenv("CSI_ENDPOINT", "unix:///tmp/custom.sock")
	t.Setenv("CSI_NODE_ID", "node-1")
	t.Setenv("CSI_MAX_VOLUMES_PER_NODE", "128")
	t.Setenv("CSI_REQUEST_TIMEOUT", "45s")
	t.Setenv("OS_REGION", "eu-nl")
	t.Setenv("OS_AVAILABILITY_ZONE", "eu-nl-01")
	t.Setenv("OS_AUTH_URL", "https://iam.example.com/v3")
	t.Setenv("OS_DOMAIN_NAME", "Default")
	t.Setenv("OS_USERNAME", "tester")
	t.Setenv("OS_PASSWORD", "secret")
	t.Setenv("OS_PROJECT_ID", "project-id")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv returned error: %v", err)
	}

	if cfg.DriverName != "custom.csi" {
		t.Fatalf("unexpected driver name: %q", cfg.DriverName)
	}
	if cfg.Backend != "evs" {
		t.Fatalf("unexpected backend: %q", cfg.Backend)
	}
	if cfg.Endpoint != "unix:///tmp/custom.sock" {
		t.Fatalf("unexpected endpoint: %q", cfg.Endpoint)
	}
	if cfg.NodeID != "node-1" {
		t.Fatalf("unexpected node id: %q", cfg.NodeID)
	}
	if cfg.MaxVolumesPerNode != 128 {
		t.Fatalf("unexpected max volumes: %d", cfg.MaxVolumesPerNode)
	}
	if cfg.Timeout != 45*time.Second {
		t.Fatalf("unexpected timeout: %s", cfg.Timeout)
	}
}

func TestFromEnvRequiresMandatoryValues(t *testing.T) {
	t.Setenv("OS_REGION", "eu-de")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error for missing auth settings")
	}
}

func TestFromEnvRejectsInvalidTypedValues(t *testing.T) {
	t.Setenv("OS_REGION", "eu-de")
	t.Setenv("OS_AUTH_URL", "https://iam.example.com/v3")
	t.Setenv("OS_USERNAME", "tester")
	t.Setenv("OS_PASSWORD", "secret")
	t.Setenv("OS_PROJECT_ID", "project-id")
	t.Setenv("CSI_MAX_VOLUMES_PER_NODE", "invalid")
	t.Setenv("CSI_REQUEST_TIMEOUT", "invalid")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error for invalid typed values")
	}
}
