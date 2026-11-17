package sfsturbo

import (
	"fmt"
	"slices"
	"strings"
)

// Values of metadata.expand_type from the OTC SFS Turbo API reference.
const (
	ExpandTypeBandwidth = "bandwidth"
	ExpandTypeHPC       = "hpc"
)

// hpcBandwidths are the metadata.hpc_bw values of 20 to 1,000 MB/s/TiB file systems.
var hpcBandwidths = []string{"20M", "40M", "125M", "250M", "500M", "1000M"}

const (
	// Standard and Performance file systems.
	MinSizeGiB int64 = 500
	MaxSizeGiB int64 = 32768
	// Standard-Enhanced and Performance-Enhanced file systems (expand_type=bandwidth).
	minEnhancedSizeGiB int64 = 10240
	maxEnhancedSizeGiB int64 = 327680
	// HPC file systems (expand_type=hpc): multiples of 1.2 TiB rounded down to whole GiB.
	minHPCSizeGiB    int64 = 1228
	minHPC20MSizeGiB int64 = 3686
	maxHPCSizeGiB    int64 = 1048576

	minExpandStepGiB int64 = 100
	// hpcStepTenthsGiB is 1.2 TiB in tenths of GiB (1228.8 GiB).
	hpcStepTenthsGiB int64 = 12288
)

// CapacityRules holds the size limits of one SFS Turbo file system type.
type CapacityRules struct {
	kind   string
	minGiB int64
	maxGiB int64
	hpc    bool
}

// CapacityRulesFor returns the size limits for the expand_type and hpc_bw of a share.
func CapacityRulesFor(expandType, hpcBandwidth string) CapacityRules {
	switch strings.ToLower(expandType) {
	case ExpandTypeBandwidth:
		return CapacityRules{kind: "Enhanced", minGiB: minEnhancedSizeGiB, maxGiB: maxEnhancedSizeGiB}
	case ExpandTypeHPC:
		rules := CapacityRules{kind: "HPC " + hpcBandwidth, minGiB: minHPCSizeGiB, maxGiB: maxHPCSizeGiB, hpc: true}
		if strings.EqualFold(hpcBandwidth, "20M") {
			rules.minGiB = minHPC20MSizeGiB
		}
		return rules
	default:
		return CapacityRules{kind: "Standard/Performance", minGiB: MinSizeGiB, maxGiB: MaxSizeGiB}
	}
}

// CreateSizeGiB rounds a requested size up to the minimum and, for HPC, to a multiple of 1.2 TiB.
func (r CapacityRules) CreateSizeGiB(requestedGiB int64) (int64, error) {
	return r.validSize(max(requestedGiB, r.minGiB))
}

// ExpandSizeGiB returns the size to expand a share of currentGiB to, so that it holds at
// least requestedGiB and grows by at least the minimum increment.
func (r CapacityRules) ExpandSizeGiB(currentGiB, requestedGiB int64) (int64, error) {
	if r.hpc {
		// Valid HPC sizes are 1.2 TiB apart, so the next one above currentGiB is a full step.
		return r.validSize(max(requestedGiB, currentGiB+1))
	}
	return r.validSize(max(requestedGiB, currentGiB+minExpandStepGiB))
}

func (r CapacityRules) validSize(sizeGiB int64) (int64, error) {
	if r.hpc {
		steps := (sizeGiB*10 + hpcStepTenthsGiB - 1) / hpcStepTenthsGiB
		sizeGiB = steps * hpcStepTenthsGiB / 10
	}
	if sizeGiB > r.maxGiB {
		return 0, fmt.Errorf("%d GiB exceeds the maximum of %d GiB for %s SFS Turbo file systems", sizeGiB, r.maxGiB, r.kind)
	}
	return sizeGiB, nil
}

// normalizeExpandParams validates expandType and hpcBw. hpcBw alone implies expandType=hpc.
func normalizeExpandParams(expandType, hpcBandwidth string) (string, string, error) {
	expandType = strings.ToLower(expandType)
	hpcBandwidth = strings.ToUpper(hpcBandwidth)
	if expandType == "" && hpcBandwidth != "" {
		expandType = ExpandTypeHPC
	}
	switch expandType {
	case "":
	case ExpandTypeBandwidth:
		if hpcBandwidth != "" {
			return "", "", fmt.Errorf("StorageClass parameter %s is only valid with %s=%s", paramHPCBandwidth, paramExpandType, ExpandTypeHPC)
		}
	case ExpandTypeHPC:
		if !slices.Contains(hpcBandwidths, hpcBandwidth) {
			return "", "", fmt.Errorf("StorageClass parameter %s=%q must be one of %s for %s=%s", paramHPCBandwidth, hpcBandwidth, strings.Join(hpcBandwidths, ", "), paramExpandType, ExpandTypeHPC)
		}
	default:
		return "", "", fmt.Errorf("StorageClass parameter %s=%q must be %s or %s", paramExpandType, expandType, ExpandTypeBandwidth, ExpandTypeHPC)
	}
	return expandType, hpcBandwidth, nil
}
