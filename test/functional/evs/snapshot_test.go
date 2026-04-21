//go:build functional

package evsfunctional

import (
	"fmt"
	"strings"
	"testing"
)

func TestEVSSnapshotVolumeLifecycle(t *testing.T) {
	cfg := loadTestConfig(t)
	k := newKubectl(cfg)
	if !k.hasVolumeSnapshotCRDs(t) {
		t.Skip("cluster does not expose Kubernetes snapshot CRDs")
	}

	namespace := testNamespace(t)
	sourcePVCName := "evs-src-pvc"
	sourcePodName := "evs-src-pod"
	snapshotName := "evs-src-snap"
	restorePVCName := "evs-restore-pvc"
	restorePodName := "evs-restore-pod"
	storageClassName := "tcloud-public-evs"
	snapshotClassName := "tcloud-public-evs"
	testFile := "/data/app/snapshot.txt"
	testValue := "functional-evs-snapshot-ok"

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("step: collect debug output from namespace %s", namespace)
			k.collectNamespaceDebug(t, namespace)
			k.collectDriverDebug(t)
		}
		if !cfg.keepResources {
			t.Logf("step: delete pod %s/%s", namespace, restorePodName)
			k.deletePod(t, namespace, restorePodName)
			t.Logf("step: delete PVC %s/%s", namespace, restorePVCName)
			k.deletePvc(t, namespace, restorePVCName)
			t.Logf("step: delete VolumeSnapshot %s/%s", namespace, snapshotName)
			k.deleteVolumeSnapshot(t, namespace, snapshotName)
			t.Logf("step: delete pod %s/%s", namespace, sourcePodName)
			k.deletePod(t, namespace, sourcePodName)
			t.Logf("step: delete PVC %s/%s", namespace, sourcePVCName)
			k.deletePvc(t, namespace, sourcePVCName)
			t.Logf("step: delete namespace %s", namespace)
			k.deleteNamespace(t, namespace)
			t.Log("step: delete deployed CSI manifests")
			k.deleteKustomize(t, cfg.deployPath)
		}
	})

	installDriver(t, cfg, k)

	t.Logf("step: create namespace %s", namespace)
	k.createNamespace(t, namespace)

	sourceManifest := fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: %s
---
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
    - name: app
      image: busybox:1.36
      command:
        - sh
        - -ceu
        - |
          mkdir -p /data/app
          echo %s > %s
          sync
          tail -f /dev/null
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, sourcePVCName, namespace, storageClassName, sourcePodName, namespace, testValue, testFile, sourcePVCName)

	t.Logf("step: create source PVC %s/%s and pod %s/%s", namespace, sourcePVCName, namespace, sourcePodName)
	k.applyManifest(t, sourceManifest)

	t.Logf("step: wait for source PVC %s/%s to bind", namespace, sourcePVCName)
	k.waitForPVCBound(t, namespace, sourcePVCName)
	t.Logf("step: wait for source pod %s/%s to become ready", namespace, sourcePodName)
	k.waitForPodReady(t, namespace, sourcePodName)

	t.Logf("step: verify source pod %s/%s can read %s", namespace, sourcePodName, testFile)
	sourceOutput := strings.TrimSpace(k.execInPod(t, namespace, sourcePodName, "cat", testFile))
	if sourceOutput != testValue {
		t.Fatalf("unexpected source file contents in %s: got %q want %q", testFile, sourceOutput, testValue)
	}

	snapshotManifest := fmt.Sprintf(`apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: %s
  namespace: %s
spec:
  volumeSnapshotClassName: %s
  source:
    persistentVolumeClaimName: %s
`, snapshotName, namespace, snapshotClassName, sourcePVCName)

	t.Logf("step: create VolumeSnapshot %s/%s from PVC %s/%s", namespace, snapshotName, namespace, sourcePVCName)
	k.applyManifest(t, snapshotManifest)

	t.Logf("step: wait for VolumeSnapshot %s/%s to become ready", namespace, snapshotName)
	k.waitForVolumeSnapshotReady(t, namespace, snapshotName)

	restoreManifest := fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: %s
  dataSource:
    name: %s
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
---
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
    - name: app
      image: busybox:1.36
      command:
        - sh
        - -ceu
        - |
          cat %s
          tail -f /dev/null
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, restorePVCName, namespace, storageClassName, snapshotName, restorePodName, namespace, testFile, restorePVCName)

	t.Logf("step: create restored PVC %s/%s and pod %s/%s", namespace, restorePVCName, namespace, restorePodName)
	k.applyManifest(t, restoreManifest)

	t.Logf("step: wait for restored PVC %s/%s to bind", namespace, restorePVCName)
	k.waitForPVCBound(t, namespace, restorePVCName)
	t.Logf("step: wait for restored pod %s/%s to become ready", namespace, restorePodName)
	k.waitForPodReady(t, namespace, restorePodName)

	t.Logf("step: verify restored pod %s/%s can read %s", namespace, restorePodName, testFile)
	restoreOutput := strings.TrimSpace(k.execInPod(t, namespace, restorePodName, "cat", testFile))
	if restoreOutput != testValue {
		t.Fatalf("unexpected restored file contents in %s: got %q want %q", testFile, restoreOutput, testValue)
	}

	t.Logf("step: verify VolumeSnapshot %s/%s is bound to snapshot content", namespace, snapshotName)
	contentName := k.getNamespacedJSONPath(t, namespace, "volumesnapshot/"+snapshotName, "{.status.boundVolumeSnapshotContentName}")
	if contentName == "" {
		t.Fatal("expected bound VolumeSnapshotContent name")
	}

	t.Logf("snapshot lifecycle succeeded with VolumeSnapshotContent %s", contentName)
}
