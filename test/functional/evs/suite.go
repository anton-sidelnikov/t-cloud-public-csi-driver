//go:build functional

package evsfunctional

import (
	"sync"
	"testing"
)

var (
	driverBootstrapMu  sync.Mutex
	driverBootstrapped bool
)

func ensureDriverInstalled(t *testing.T, cfg testConfig, k kubectl) {
	t.Helper()

	driverBootstrapMu.Lock()
	defer driverBootstrapMu.Unlock()

	if driverBootstrapped {
		t.Log("step: reuse existing deployed CSI driver")
		return
	}

	installDriver(t, cfg, k)
	driverBootstrapped = true
}
