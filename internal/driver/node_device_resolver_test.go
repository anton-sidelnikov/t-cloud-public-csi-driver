package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeDeviceResolver struct {
	volumeID     string
	reportedPath string
	resolvedPath string
	err          error
}

func (r *fakeDeviceResolver) ResolveDevicePath(_ context.Context, volumeID, reportedPath string) (string, error) {
	r.volumeID = volumeID
	r.reportedPath = reportedPath
	return r.resolvedPath, r.err
}

func TestDevicePathCandidatesStartWithReportedPath(t *testing.T) {
	volumeID := "123e4567-e89b-12d3-a456-426614174000"
	candidates := devicePathCandidates(volumeID, "/dev/vdc")
	if len(candidates) == 0 || candidates[0] != "/dev/vdc" {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}
}

func TestResolveExistingDevicePathUsesSymlinkTarget(t *testing.T) {
	dir := t.TempDir()

	devicePath := filepath.Join(dir, "vdc")
	targetPath := filepath.Join(dir, "disk")
	if err := os.WriteFile(targetPath, []byte("device"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(targetPath, devicePath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, matched, ok, err := resolveExistingDevicePath("vol-1", devicePath)
	if err != nil {
		t.Fatalf("resolveExistingDevicePath returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected path to resolve")
	}
	want, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatalf("eval target symlinks: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected resolved path: got %q want %q", got, want)
	}
	if matched != devicePath {
		t.Fatalf("unexpected matched path: got %q want %q", matched, devicePath)
	}
}

func TestOSDevicePathResolverTimesOut(t *testing.T) {
	resolver := &osDevicePathResolver{timeout: 10 * time.Millisecond}

	_, err := resolver.ResolveDevicePath(context.Background(), "vol-1", "/dev/does-not-exist")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestOSDevicePathResolverHonorsContextCancellation(t *testing.T) {
	resolver := &osDevicePathResolver{timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resolver.ResolveDevicePath(ctx, "vol-1", "/dev/does-not-exist")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}
