package driver

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type nodeMounter interface {
	EnsureDir(path string) error
	EnsureFile(path string) error
	Remove(path string) error
	IsMounted(target string) (bool, error)
	Mount(ctx context.Context, source, target, fsType string, options []string) error
	Unmount(ctx context.Context, target string) error
}

type nodeDeviceManager interface {
	EnsureFormatted(ctx context.Context, devicePath, fsType string) error
	Resize(ctx context.Context, volumePath, fsType string) error
}

type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (r *execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

type osMounter struct{}

func (m *osMounter) EnsureDir(path string) error {
	return os.MkdirAll(path, 0o750)
}

func (m *osMounter) EnsureFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE, 0o640)
	if err != nil {
		return err
	}

	return file.Close()
}

func (m *osMounter) Remove(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (m *osMounter) IsMounted(target string) (bool, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[4] == target {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}

	return false, nil
}

func (m *osMounter) Mount(ctx context.Context, source, target, fsType string, options []string) error {
	args := make([]string, 0, 6)
	if fsType != "" {
		args = append(args, "-t", fsType)
	}
	if len(options) > 0 {
		args = append(args, "-o", strings.Join(options, ","))
	}
	args = append(args, source, target)

	_, err := (&execRunner{}).Run(ctx, "mount", args...)
	return err
}

func (m *osMounter) Unmount(ctx context.Context, target string) error {
	_, err := (&execRunner{}).Run(ctx, "umount", target)
	return err
}

type filesystemManager struct {
	runner commandRunner
}

func newFilesystemManager(runner commandRunner) *filesystemManager {
	return &filesystemManager{runner: runner}
}

func (m *filesystemManager) EnsureFormatted(ctx context.Context, devicePath, fsType string) error {
	existingType, err := m.detectFSType(ctx, devicePath)
	if err == nil && existingType != "" {
		return nil
	}

	cmd, args, err := mkfsCommand(fsType, devicePath)
	if err != nil {
		return err
	}

	_, err = m.runner.Run(ctx, cmd, args...)
	return err
}

func (m *filesystemManager) Resize(ctx context.Context, volumePath, fsType string) error {
	resolvedFSType := fsType
	if resolvedFSType == "" {
		resolvedFSType = "ext4"
	}

	if resolvedFSType == "xfs" {
		_, err := m.runner.Run(ctx, "xfs_growfs", volumePath)
		return err
	}

	devicePath, err := m.findSourceDevice(ctx, volumePath)
	if err != nil {
		return err
	}

	_, err = m.runner.Run(ctx, "resize2fs", devicePath)
	return err
}

func (m *filesystemManager) detectFSType(ctx context.Context, devicePath string) (string, error) {
	output, err := m.runner.Run(ctx, "blkid", "-o", "value", "-s", "TYPE", devicePath)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func (m *filesystemManager) findSourceDevice(ctx context.Context, volumePath string) (string, error) {
	output, err := m.runner.Run(ctx, "findmnt", "-n", "-o", "SOURCE", "--target", volumePath)
	if err != nil {
		return "", err
	}

	devicePath := strings.TrimSpace(string(output))
	if devicePath == "" {
		return "", fmt.Errorf("could not resolve source device for %s", volumePath)
	}

	return devicePath, nil
}

func mkfsCommand(fsType, devicePath string) (string, []string, error) {
	switch defaultFSType(fsType) {
	case "ext3":
		return "mkfs.ext3", []string{"-F", devicePath}, nil
	case "ext4":
		return "mkfs.ext4", []string{"-F", devicePath}, nil
	case "xfs":
		return "mkfs.xfs", []string{"-f", devicePath}, nil
	default:
		return "", nil, fmt.Errorf("unsupported filesystem type %q", fsType)
	}
}

func mountOptions(readonly bool, flags []string, extra ...string) []string {
	options := make([]string, 0, len(flags)+len(extra)+1)
	options = append(options, extra...)
	if readonly {
		options = append(options, "ro")
	}
	options = append(options, flags...)
	return options
}

func defaultFSType(fsType string) string {
	if fsType == "" {
		return "ext4"
	}
	return fsType
}
