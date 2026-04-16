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

type devicePathResolver interface {
	ResolveDevicePath(ctx context.Context, volumeID, reportedPath string) (string, error)
}

type osDevicePathResolver struct {
	timeout time.Duration
	logger  *slog.Logger
}

func newDevicePathResolver(cfg config.Config) devicePathResolver {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	return &osDevicePathResolver{
		timeout: timeout,
		logger:  slog.Default().With("component", "node-device-resolver"),
	}
}

func (r *osDevicePathResolver) ResolveDevicePath(ctx context.Context, volumeID, reportedPath string) (string, error) {
	deadline := time.Now().Add(r.timeout)
	logger := r.loggerOrDefault().With("volume_id", volumeID, "reported_path", reportedPath)
	logger.Info("waiting for node device path")

	for {
		resolvedPath, matchedPath, ok, err := resolveExistingDevicePath(volumeID, reportedPath)
		if err != nil {
			return "", err
		}
		if ok {
			logger.Info("resolved node device path", "matched_path", matchedPath, "resolved_path", resolvedPath)
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

func resolveExistingDevicePath(volumeID, reportedPath string) (string, string, bool, error) {
	candidates := devicePathCandidates(volumeID, reportedPath)
	for _, candidate := range candidates {
		_, err := os.Stat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", "", false, err
		}

		resolvedPath, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			return resolvedPath, candidate, true, nil
		}
		if os.IsNotExist(err) {
			continue
		}

		return candidate, candidate, true, nil
	}

	return "", "", false, nil
}

func devicePathCandidates(volumeID, reportedPath string) []string {
	candidates := []string{reportedPath}

	noHyphen := strings.ReplaceAll(strings.ToLower(volumeID), "-", "")
	shortNoHyphen := noHyphen
	if len(shortNoHyphen) > 20 {
		shortNoHyphen = shortNoHyphen[:20]
	}

	patterns := []string{
		filepath.Join("/dev/disk/by-id", "*"+strings.ToLower(volumeID)+"*"),
		filepath.Join("/dev/disk/by-id", "*"+strings.ToUpper(volumeID)+"*"),
		filepath.Join("/dev/disk/by-id", "*"+noHyphen+"*"),
		filepath.Join("/dev/disk/by-id", "*"+shortNoHyphen+"*"),
	}

	seen := map[string]struct{}{reportedPath: {}}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}
			candidates = append(candidates, match)
		}
	}

	return candidates
}
