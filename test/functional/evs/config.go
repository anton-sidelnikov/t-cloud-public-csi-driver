//go:build functional

package evsfunctional

import (
	"os"
	"path/filepath"
	"runtime"
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
	deployPath    string
	insecureTLS   bool
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
		deployPath:    mustRepoPath(t, "deploy", "kubernetes"),
		insecureTLS:   strings.EqualFold(strings.TrimSpace(os.Getenv("CSI_TEST_INSECURE_SKIP_TLS_VERIFY")), "true"),
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

func mustRepoPath(t *testing.T, elems ...string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repo path: runtime caller unavailable")
	}

	// config.go lives in test/functional/evs, so repo root is three levels up.
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	path := filepath.Join(append([]string{repoRoot}, elems...)...)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("resolve repo path %q: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("resolve repo path %q: not a directory", path)
	}

	return path
}
