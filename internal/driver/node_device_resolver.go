package driver

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"t-cloud-public-csi-driver/internal/config"
)

const (
	defaultSysBlockDir = "/sys/block"
	defaultDevDir      = "/dev"
	defaultByIDDir     = "/dev/disk/by-id"

	// Shorter serials are not considered a reliable volume identity.
	minSerialMatchLength = 8
)

type devicePathResolver interface {
	ResolveDevicePath(ctx context.Context, volumeID, reportedPath string) (string, error)
}

type osDevicePathResolver struct {
	timeout     time.Duration
	logger      *slog.Logger
	sysBlockDir string
	devDir      string
	byIDDir     string
}

func newDevicePathResolver(cfg config.Config) devicePathResolver {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	return &osDevicePathResolver{
		timeout:     timeout,
		logger:      slog.Default().With("component", "node-device-resolver"),
		sysBlockDir: defaultSysBlockDir,
		devDir:      defaultDevDir,
		byIDDir:     defaultByIDDir,
	}
}

func (r *osDevicePathResolver) ResolveDevicePath(ctx context.Context, volumeID, reportedPath string) (string, error) {
	deadline := time.Now().Add(r.timeout)
	logger := r.loggerOrDefault().With("volume_id", volumeID, "reported_path", reportedPath)
	logger.Info("waiting for node device path")

	for {
		resolvedPath, source, ok, err := r.resolveOnce(volumeID, reportedPath)
		if err != nil {
			return "", err
		}
		if ok {
			logger.Info("resolved node device path", "match_source", source, "resolved_path", resolvedPath)
			return resolvedPath, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for device path for volume %s, last reported path %s", volumeID, reportedPath)
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (r *osDevicePathResolver) loggerOrDefault() *slog.Logger {
	if r.logger != nil {
		return r.logger
	}
	return slog.Default().With("component", "node-device-resolver")
}

// resolveOnce identifies the device by its serial first, because the guest kernel
// can name disks in a different order than the cloud API reports them.
func (r *osDevicePathResolver) resolveOnce(volumeID, reportedPath string) (string, string, bool, error) {
	if path, ok := r.findBySerial(volumeID); ok {
		return path, "serial", true, nil
	}

	if path, ok, err := r.findByID(volumeID); err != nil || ok {
		return path, "by-id", ok, err
	}

	if reportedPath == "" {
		return "", "", false, nil
	}
	resolved, err := filepath.EvalSymlinks(reportedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	// The reported path is trusted only when the device has no readable serial;
	// a serial belonging to another volume means the cloud-reported name is stale.
	if serial := r.readSerial(filepath.Base(resolved)); serial != "" {
		r.loggerOrDefault().Warn("reported device path belongs to another volume", "volume_id", volumeID, "reported_path", reportedPath, "device_serial", serial)
		return "", "", false, nil
	}
	return resolved, "reported-path", true, nil
}

func (r *osDevicePathResolver) findBySerial(volumeID string) (string, bool) {
	entries, err := os.ReadDir(r.sysBlockDir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if serialMatchesVolume(r.readSerial(entry.Name()), volumeID) {
			path := filepath.Join(r.devDir, entry.Name())
			if _, err := os.Stat(path); err == nil {
				return path, true
			}
		}
	}
	return "", false
}

func (r *osDevicePathResolver) findByID(volumeID string) (string, bool, error) {
	entries, err := os.ReadDir(r.byIDDir)
	if err != nil {
		return "", false, nil
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, "-part") || !byIDNameMatchesVolume(name, volumeID) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(r.byIDDir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", false, err
		}
		return resolved, true, nil
	}
	return "", false, nil
}

func (r *osDevicePathResolver) readSerial(deviceName string) string {
	data, err := os.ReadFile(filepath.Join(r.sysBlockDir, deviceName, "serial"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// serialMatchesVolume handles virtio serials, which are the volume ID truncated to 20 characters.
func serialMatchesVolume(serial, volumeID string) bool {
	serial = strings.ToLower(strings.TrimSpace(serial))
	if len(serial) < minSerialMatchLength || volumeID == "" {
		return false
	}
	id := strings.ToLower(volumeID)
	return strings.HasPrefix(id, serial) || strings.HasPrefix(strings.ReplaceAll(id, "-", ""), serial)
}

func byIDNameMatchesVolume(name, volumeID string) bool {
	name = strings.ToLower(name)
	for _, prefix := range []string{"virtio-", "scsi-0qemu_qemu_harddisk_", "scsi-", "wwn-"} {
		if strings.HasPrefix(name, prefix) {
			return serialMatchesVolume(strings.TrimPrefix(name, prefix), volumeID)
		}
	}
	return false
}
