//go:build functional

// Package framework holds helpers shared by the backend functional test suites.
package framework

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Config holds the environment shared by all functional suites.
type Config struct {
	Kubeconfig    string
	DriverImage   string
	InsecureTLS   bool
	AuthURL       string
	Region        string
	Zone          string
	DomainName    string
	Username      string
	Password      string
	ProjectID     string
	ProjectName   string
	KeepResources bool
}

// LoadConfig reads the common environment and skips the test when a required value is missing.
func LoadConfig(t *testing.T) Config {
	t.Helper()

	cfg := Config{
		Kubeconfig:    RequireEnv(t, "KUBECONFIG"),
		DriverImage:   RequireEnv(t, "CSI_TEST_IMAGE"),
		InsecureTLS:   envBool("CSI_TEST_INSECURE_SKIP_TLS_VERIFY"),
		AuthURL:       RequireEnv(t, "OS_AUTH_URL"),
		Region:        RequireEnv(t, "OS_REGION"),
		Zone:          RequireEnv(t, "OS_AVAILABILITY_ZONE"),
		Username:      RequireEnv(t, "OS_USERNAME"),
		Password:      RequireEnv(t, "OS_PASSWORD"),
		ProjectID:     strings.TrimSpace(os.Getenv("OS_PROJECT_ID")),
		ProjectName:   strings.TrimSpace(os.Getenv("OS_PROJECT_NAME")),
		DomainName:    strings.TrimSpace(os.Getenv("OS_DOMAIN_NAME")),
		KeepResources: envBool("CSI_TEST_KEEP_RESOURCES"),
	}

	if cfg.ProjectID == "" && cfg.ProjectName == "" {
		t.Skip("either OS_PROJECT_ID or OS_PROJECT_NAME is required for functional tests")
	}

	return cfg
}

// RequireEnv returns a trimmed environment value and skips the test when it is empty.
func RequireEnv(t *testing.T, key string) string {
	t.Helper()

	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Skipf("%s is required for functional tests", key)
	}

	return value
}

func envBool(key string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "true")
}

// RepoPath resolves a directory relative to the repository root.
func RepoPath(t *testing.T, elems ...string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repo path: runtime caller unavailable")
	}

	// config.go lives in test/functional/framework, so the repo root is three levels up.
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
