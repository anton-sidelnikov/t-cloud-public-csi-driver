package driver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// volumeStats is the filesystem usage of a mounted volume.
type volumeStats struct {
	TotalBytes, AvailableBytes, UsedBytes    int64
	TotalInodes, AvailableInodes, UsedInodes int64
}

type volumeStatter interface {
	// Stats returns the usage of the filesystem at path; the error wraps fs.ErrNotExist
	// if the path does not exist.
	Stats(path string) (volumeStats, error)
}

type osVolumeStatter struct{}

func (osVolumeStatter) Stats(path string) (volumeStats, error) {
	info, err := os.Stat(path)
	if err != nil {
		return volumeStats{}, err
	}
	if !info.IsDir() {
		return volumeStats{}, fmt.Errorf("%s is not a directory", path)
	}
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return volumeStats{}, fmt.Errorf("statfs %s: %w", path, err)
	}
	blockSize := int64(st.Bsize)
	return volumeStats{
		TotalBytes:      int64(st.Blocks) * blockSize,
		AvailableBytes:  int64(st.Bavail) * blockSize,
		UsedBytes:       int64(st.Blocks-st.Bfree) * blockSize,
		TotalInodes:     int64(st.Files),
		AvailableInodes: int64(st.Ffree),
		UsedInodes:      int64(st.Files - st.Ffree),
	}, nil
}

func (s *nodeServer) NodeGetVolumeStats(_ context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}
	if req.GetVolumePath() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_path is required")
	}

	statter := s.statter
	if statter == nil {
		statter = osVolumeStatter{}
	}
	stats, err := statter.Stats(req.GetVolumePath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, status.Errorf(codes.NotFound, "volume path %s does not exist", req.GetVolumePath())
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get volume stats: %v", err)
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{Unit: csi.VolumeUsage_BYTES, Total: stats.TotalBytes, Available: stats.AvailableBytes, Used: stats.UsedBytes},
			{Unit: csi.VolumeUsage_INODES, Total: stats.TotalInodes, Available: stats.AvailableInodes, Used: stats.UsedInodes},
		},
	}, nil
}
