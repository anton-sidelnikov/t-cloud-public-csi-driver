//go:build functional

package evsfunctional

import (
	"sync"
	"testing"

	"t-cloud-public-csi-driver/test/functional/framework"
)

const systemNamespace = "tcloud-public-csi-system"

var (
	driverBootstrapMu  sync.Mutex
	driverBootstrapped bool
)

func loadTestConfig(t *testing.T) framework.Config {
	t.Helper()
	return framework.LoadConfig(t)
}

func evsDriver(t *testing.T) framework.Driver {
	t.Helper()

	return framework.Driver{
		Name:       "csi.evs.tcloudpublic.com",
		DeployPath: framework.RepoPath(t, "deploy", "kubernetes", "cce", "evs"),
		Namespace:  systemNamespace,
		Controller: "tcloud-public-csi-controller",
		Node:       "tcloud-public-csi-node",
	}
}

func ensureDriverInstalled(t *testing.T, cfg framework.Config, k *framework.Kubectl) {
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

func collectDriverDebug(t *testing.T, k *framework.Kubectl) {
	t.Helper()

	k.LogCommands(t,
		[]string{"get", "pods", "-A", "-o", "wide"},
		[]string{"-n", "kube-system", "get", "deployment", "everest-csi-controller", "-o", "wide"},
		[]string{"-n", systemNamespace, "get", "deployment", "tcloud-public-snapshot-controller", "-o", "wide"},
		[]string{"-n", systemNamespace, "logs", "deployment/tcloud-public-snapshot-controller", "--tail=200"},
	)
	evsDriver(t).CollectDebug(t, k)
}
