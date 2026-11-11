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

type fakeHost struct {
	sys, dev, byID string
}

func newFakeHost(t *testing.T) fakeHost {
	t.Helper()
	root := t.TempDir()
	h := fakeHost{sys: filepath.Join(root, "sys"), dev: filepath.Join(root, "dev"), byID: filepath.Join(root, "by-id")}
	for _, d := range []string{h.sys, h.dev, h.byID} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func (h fakeHost) addDisk(t *testing.T, name, serial string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(h.sys, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if serial != "" {
		if err := os.WriteFile(filepath.Join(h.sys, name, "serial"), []byte(serial+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(h.dev, name)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func (h fakeHost) resolver() *osDevicePathResolver {
	return &osDevicePathResolver{timeout: 10 * time.Millisecond, sysBlockDir: h.sys, devDir: h.dev, byIDDir: h.byID}
}

func TestResolverPrefersSerialOverMismatchedReportedPath(t *testing.T) {
	h := newFakeHost(t)
	h.addDisk(t, "vdb", "93e12c02-60a5-412e-9")
	reported := h.addDisk(t, "vdd", "541f94fb-1111-2222-3")
	want := h.addDisk(t, "vde", "8d54fc61-aaaa-bbbb-c")

	got, err := h.resolver().ResolveDevicePath(context.Background(), "8d54fc61-aaaa-bbbb-cccc-dddddddddddd", reported)
	if err != nil {
		t.Fatalf("ResolveDevicePath: %v", err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolverRejectsReportedPathOwnedByAnotherVolume(t *testing.T) {
	h := newFakeHost(t)
	reported := h.addDisk(t, "vdb", "93e12c02-60a5-412e-9")

	_, err := h.resolver().ResolveDevicePath(context.Background(), "8d54fc61-aaaa-bbbb-cccc-dddddddddddd", reported)
	if err == nil {
		t.Fatal("expected timeout instead of using another volume's device")
	}
}

func TestResolverUsesByIDLink(t *testing.T) {
	h := newFakeHost(t)
	target := h.addDisk(t, "vdc", "")
	if err := os.Symlink(target, filepath.Join(h.byID, "virtio-874a8f3e-0000-1111-2")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(h.byID, "virtio-874a8f3e-0000-1111-2-part1")); err != nil {
		t.Fatal(err)
	}

	got, err := h.resolver().ResolveDevicePath(context.Background(), "874a8f3e-0000-1111-2222-333333333333", "")
	if err != nil {
		t.Fatalf("ResolveDevicePath: %v", err)
	}
	want, _ := filepath.EvalSymlinks(target)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolverFallsBackToReportedPathWithoutSerial(t *testing.T) {
	h := newFakeHost(t)
	reported := h.addDisk(t, "vdc", "")

	got, err := h.resolver().ResolveDevicePath(context.Background(), "874a8f3e-0000-1111-2222-333333333333", reported)
	if err != nil {
		t.Fatalf("ResolveDevicePath: %v", err)
	}
	want, _ := filepath.EvalSymlinks(reported)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSerialMatchesVolume(t *testing.T) {
	id := "93e12c02-60a5-412e-9abc-def012345678"
	cases := map[string]bool{
		"93e12c02-60a5-412e-9": true,
		"93E12C0260A5412E9ABC": true,
		"93e12c02":             true,
		"93e1":                 false,
		"541f94fb-1111-2222-3": false,
		"":                     false,
	}
	for serial, want := range cases {
		if got := serialMatchesVolume(serial, id); got != want {
			t.Errorf("serialMatchesVolume(%q) = %v, want %v", serial, got, want)
		}
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
