//go:build functional

package evsfunctional

import (
	"fmt"
	"testing"
)

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
	if k.deploymentExists(t, "kube-system", "everest-csi-controller") {
		t.Log("step: scale down managed everest-csi-controller to avoid snapshot reconciliation conflicts in the ephemeral test cluster")
		k.scaleDeployment(t, "kube-system", "everest-csi-controller", 0)
		t.Log("step: wait for everest-csi-controller to scale down")
		k.waitForDeploymentReplicas(t, "kube-system", "everest-csi-controller", 0)
	}
	if k.hasVolumeSnapshotCRDs(t) {
		t.Log("step: ensure snapshot-controller deployment is present for VolumeSnapshot reconciliation")
		ensureSnapshotControllerInstalled(t, k)
	}
	t.Log("step: verify CSIDriver registration")
	k.assertCSIDriverRegistered(t)
}

func ensureSnapshotControllerInstalled(t *testing.T, k kubectl) {
	t.Helper()

	if k.snapshotControllerExists(t) {
		t.Log("step: reuse existing deployed snapshot-controller")
		k.waitForSnapshotControllerReady(t)
		return
	}

	t.Log("step: snapshot-controller deployment was not created by the manifest bundle, applying fallback functional manifest")
	k.applyManifest(t, snapshotControllerManifest())
	t.Log("step: wait for snapshot-controller rollout readiness")
	k.waitForSnapshotControllerReady(t)
}

func snapshotControllerManifest() string {
	return fmt.Sprintf(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: tcloud-public-snapshot-controller
  namespace: %s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: tcloud-public-snapshot-controller-runner
rules:
  - apiGroups: [""]
    resources: ["persistentvolumes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["persistentvolumeclaims"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["list", "watch", "create", "update", "patch"]
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshotclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshotcontents"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshotcontents/status"]
    verbs: ["update", "patch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshotcontents/finalizers"]
    verbs: ["update", "patch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshots"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshots/status"]
    verbs: ["update", "patch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshots/finalizers"]
    verbs: ["update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: tcloud-public-snapshot-controller-runner
subjects:
  - kind: ServiceAccount
    name: tcloud-public-snapshot-controller
    namespace: %s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: tcloud-public-snapshot-controller-runner
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tcloud-public-snapshot-controller
  namespace: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tcloud-public-snapshot-controller
  template:
    metadata:
      labels:
        app: tcloud-public-snapshot-controller
    spec:
      serviceAccountName: tcloud-public-snapshot-controller
      containers:
        - name: snapshot-controller
          image: registry.k8s.io/sig-storage/snapshot-controller:v8.2.0
          args:
            - --v=2
            - --leader-election=true
            - --leader-election-namespace=%s
`, systemNamespace, systemNamespace, systemNamespace, systemNamespace)
}
