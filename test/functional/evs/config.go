//go:build functional

package evsfunctional

import (
	"os"
	"strings"
	"testing"
)

const (
	systemNamespace = "tcloud-public-csi-system"
	cloudSecretName = "tcloud-public-csi-cloud-config"
)

type testConfig struct {
	kubeconfig    string
	driverImage   string
	authURL       string
	region        string
	zone          string
	domainName    string
	username      string
	password      string
	projectID     string
	projectName   string
	keepResources bool
}

func loadTestConfig(t *testing.T) testConfig {
	t.Helper()

	cfg := testConfig{
		kubeconfig:    requireEnv(t, "KUBECONFIG"),
		driverImage:   requireEnv(t, "CSI_TEST_IMAGE"),
		authURL:       requireEnv(t, "OS_AUTH_URL"),
		region:        requireEnv(t, "OS_REGION"),
		zone:          requireEnv(t, "OS_AVAILABILITY_ZONE"),
		username:      requireEnv(t, "OS_USERNAME"),
		password:      requireEnv(t, "OS_PASSWORD"),
		projectID:     strings.TrimSpace(os.Getenv("OS_PROJECT_ID")),
		projectName:   strings.TrimSpace(os.Getenv("OS_PROJECT_NAME")),
		domainName:    strings.TrimSpace(os.Getenv("OS_DOMAIN_NAME")),
		keepResources: strings.EqualFold(strings.TrimSpace(os.Getenv("CSI_TEST_KEEP_RESOURCES")), "true"),
	}

	if cfg.projectID == "" && cfg.projectName == "" {
		t.Skip("either OS_PROJECT_ID or OS_PROJECT_NAME is required for EVS functional tests")
	}

	return cfg
}

func requireEnv(t *testing.T, key string) string {
	t.Helper()

	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Skipf("%s is required for EVS functional tests", key)
	}

	return value
}
