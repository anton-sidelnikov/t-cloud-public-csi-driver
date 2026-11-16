//go:build functional

package sfsturbofunctional

import (
	"sync"
	"testing"

	"t-cloud-public-csi-driver/test/functional/framework"
)

const (
	driverNamespace  = "tcloud-public-csi-sfsturbo"
	storageClassName = "tcloud-public-sfsturbo"
)

var (
	driverBootstrapMu  sync.Mutex
	driverBootstrapped bool
)

// sfsTurboDriver needs the network of the test cluster: SFS Turbo shares are created in the
// cluster VPC and subnet so that nodes can mount them.
func sfsTurboDriver(t *testing.T) framework.Driver {
	t.Helper()

	return framework.Driver{
		Name:       "csi.sfsturbo.tcloudpublic.com",
		DeployPath: framework.RepoPath(t, "deploy", "kubernetes", "cce", "sfsturbo"),
		Namespace:  driverNamespace,
		Controller: "tcloud-public-csi-controller-sfsturbo",
		Node:       "tcloud-public-csi-node-sfsturbo",
		ExtraSecret: map[string]string{
			"OS_VPC_ID":            framework.RequireEnv(t, "CSI_TEST_VPC_ID"),
			"OS_SUBNET_ID":         framework.RequireEnv(t, "CSI_TEST_SUBNET_ID"),
			"OS_SECURITY_GROUP_ID": framework.RequireEnv(t, "CSI_TEST_SECURITY_GROUP_ID"),
		},
	}
}

func ensureDriverInstalled(t *testing.T, cfg framework.Config, k *framework.Kubectl) {
	t.Helper()

	driverBootstrapMu.Lock()
	defer driverBootstrapMu.Unlock()

	if driverBootstrapped {
		t.Log("step: reuse existing deployed SFS Turbo CSI driver")
		return
	}

	sfsTurboDriver(t).Install(t, cfg, k)
	driverBootstrapped = true
}
