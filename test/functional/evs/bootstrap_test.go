//go:build functional

package evsfunctional

import (
	"testing"

	"t-cloud-public-csi-driver/test/functional/framework"
)

func TestDriverBootstrap(t *testing.T) {
	cfg := loadTestConfig(t)
	k := framework.NewKubectl(cfg)

	t.Cleanup(func() {
		if t.Failed() {
			t.Log("step: collect driver debug output")
			collectDriverDebug(t, k)
		}
	})

	ensureDriverInstalled(t, cfg, k)
}
