//go:build functional

package evsfunctional

import "testing"

func TestDriverBootstrap(t *testing.T) {
	cfg := loadTestConfig(t)
	k := newKubectl(cfg)

	t.Cleanup(func() {
		if t.Failed() {
			k.collectDriverDebug(t)
		}
		if !cfg.keepResources {
			k.deleteKustomize(t, "deploy/kubernetes")
		}
	})

	k.applyKustomize(t, "deploy/kubernetes")
	k.ensureNamespaceExists(t)
	k.createOrUpdateCloudSecret(t, cfg)
	k.setDriverImage(t, cfg.driverImage)
	k.waitForDriverReady(t)
	k.assertCSIDriverRegistered(t)
}
