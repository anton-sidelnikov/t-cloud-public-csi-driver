//go:build functional

package gpsfsfunctional

import (
	"sync"
	"testing"

	"t-cloud-public-csi-driver/test/functional/framework"
)

const (
	driverName       = "csi.gpsfs.tcloudpublic.com"
	driverNamespace  = "tcloud-public-csi-gpsfs"
	storageClassName = "tcloud-public-gpsfs"
)

var (
	driverBootstrapMu  sync.Mutex
	driverBootstrapped bool
)

// gpsfsDriver allows access to new file systems from the cluster VPC, which needs a VPC
// endpoint for SFS General Purpose.
func gpsfsDriver(t *testing.T) framework.Driver {
	t.Helper()

	return framework.Driver{
		Name:       driverName,
		DeployPath: framework.RepoPath(t, "deploy", "kubernetes", "cce", "gpsfs"),
		Namespace:  driverNamespace,
		Controller: "tcloud-public-csi-controller-gpsfs",
		Node:       "tcloud-public-csi-node-gpsfs",
		ExtraSecret: map[string]string{
			"OS_VPC_ID": framework.RequireEnv(t, "CSI_TEST_VPC_ID"),
		},
	}
}

func ensureDriverInstalled(t *testing.T, cfg framework.Config, k *framework.Kubectl) {
	t.Helper()

	driverBootstrapMu.Lock()
	defer driverBootstrapMu.Unlock()

	if driverBootstrapped {
		t.Log("step: reuse existing deployed SFS General Purpose CSI driver")
		return
	}

	gpsfsDriver(t).Install(t, cfg, k)
	driverBootstrapped = true
}
