//go:build functional

package evsfunctional

import "testing"

func TestDriverBootstrap(t *testing.T) {
	cfg := loadTestConfig(t)
	k := newKubectl(cfg)

	t.Cleanup(func() {
		if t.Failed() {
			t.Log("step: collect driver debug output")
			k.collectDriverDebug(t)
		}
		if !cfg.keepResources {
			t.Log("step: delete deployed CSI manifests")
			k.deleteKustomize(t, cfg.deployPath)
		}
	})

	installDriver(t, cfg, k)
}
