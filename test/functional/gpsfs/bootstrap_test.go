//go:build functional

package gpsfsfunctional

import (
	"strings"
	"testing"

	"t-cloud-public-csi-driver/test/functional/framework"
)

func TestGPSFSDriverBootstrap(t *testing.T) {
	cfg := framework.LoadConfig(t)
	k := framework.NewKubectl(cfg)
	driver := gpsfsDriver(t)

	t.Cleanup(func() {
		if t.Failed() {
			t.Log("step: collect driver debug output")
			driver.CollectDebug(t, k)
		}
	})

	ensureDriverInstalled(t, cfg, k)

	t.Log("step: verify CSIDriver does not require attach")
	if got := strings.TrimSpace(k.Run(t, "get", "csidriver", driver.Name, "-o", "jsonpath={.spec.attachRequired}")); got != "false" {
		t.Fatalf("CSIDriver %s attachRequired=%q, want false", driver.Name, got)
	}
}
