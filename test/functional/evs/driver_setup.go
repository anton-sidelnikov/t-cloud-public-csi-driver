//go:build functional

package evsfunctional

import "testing"

func installDriver(t *testing.T, cfg testConfig, k kubectl) {
	t.Helper()

	t.Logf("step: apply CSI manifests from %s", cfg.deployPath)
	k.applyKustomize(t, cfg.deployPath)
	t.Logf("step: verify namespace %s exists", systemNamespace)
	k.ensureNamespaceExists(t)
	t.Logf("step: create or update cloud secret %s/%s", systemNamespace, cloudSecretName)
	k.createOrUpdateCloudSecret(t, cfg)
	t.Logf("step: patch controller and node images to %s", cfg.driverImage)
	k.setDriverImage(t, cfg.driverImage)
	t.Log("step: wait for controller and node rollout readiness")
	k.waitForDriverReady(t)
	t.Log("step: verify CSIDriver registration")
	k.assertCSIDriverRegistered(t)
}
