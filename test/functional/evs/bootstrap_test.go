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
	})

	ensureDriverInstalled(t, cfg, k)
}
