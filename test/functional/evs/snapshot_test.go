//go:build functional

package evsfunctional

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"t-cloud-public-csi-driver/test/functional/framework"
)

func TestEVSSnapshotVolumeLifecycle(t *testing.T) {
	cfg := loadTestConfig(t)
	k := framework.NewKubectl(cfg)
	if !k.HasVolumeSnapshotCRDs() {
		t.Skip("cluster does not expose Kubernetes snapshot CRDs")
	}

	namespace := framework.TestNamespace(t)
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
			k.CollectNamespaceDebug(t, namespace)
			collectDriverDebug(t, k)
		}
		if !cfg.KeepResources {
			t.Logf("step: delete pod %s/%s", namespace, restorePodName)
			k.DeletePod(t, namespace, restorePodName)
			t.Logf("step: delete PVC %s/%s", namespace, restorePVCName)
			k.DeletePVC(t, namespace, restorePVCName)
			t.Logf("step: delete VolumeSnapshot %s/%s", namespace, snapshotName)
			k.DeleteVolumeSnapshot(t, namespace, snapshotName)
			t.Logf("step: delete pod %s/%s", namespace, sourcePodName)
			k.DeletePod(t, namespace, sourcePodName)
			t.Logf("step: delete PVC %s/%s", namespace, sourcePVCName)
			k.DeletePVC(t, namespace, sourcePVCName)
			t.Logf("step: delete namespace %s", namespace)
			k.DeleteNamespace(t, namespace)
		}
	})

	ensureDriverInstalled(t, cfg, k)

	t.Logf("step: create namespace %s", namespace)
	k.CreateNamespace(t, namespace)

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
	k.ApplyManifest(t, sourceManifest)

	t.Logf("step: wait for source PVC %s/%s to bind", namespace, sourcePVCName)
	k.WaitForPVCBound(t, namespace, sourcePVCName, 5*time.Minute)
	sourcePVName := k.GetNamespacedJSONPath(t, namespace, "pvc/"+sourcePVCName, "{.spec.volumeName}")
	if sourcePVName == "" {
		t.Fatal("expected source PVC to reference a PV")
	}
	t.Logf("step: wait for source pod %s/%s to become ready", namespace, sourcePodName)
	k.WaitForPodReady(t, namespace, sourcePodName)

	t.Logf("step: verify source pod %s/%s can read %s", namespace, sourcePodName, testFile)
	sourceOutput := strings.TrimSpace(k.ExecInPod(t, namespace, sourcePodName, "cat", testFile))
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
	k.ApplyManifest(t, snapshotManifest)

	t.Logf("step: wait for VolumeSnapshot %s/%s to become ready", namespace, snapshotName)
	k.WaitForVolumeSnapshotReady(t, namespace, snapshotName)

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
          mkdir -p /data/app
          echo restore-smoke > /data/app/restore-smoke.txt
          tail -f /dev/null
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, restorePVCName, namespace, storageClassName, snapshotName, restorePodName, namespace, restorePVCName)

	t.Logf("step: create restored PVC %s/%s and smoke pod %s/%s from VolumeSnapshot %s/%s", namespace, restorePVCName, namespace, restorePodName, namespace, snapshotName)
	k.ApplyManifest(t, restoreManifest)

	t.Logf("step: wait for restored PVC %s/%s to bind", namespace, restorePVCName)
	k.WaitForPVCBound(t, namespace, restorePVCName, 5*time.Minute)
	t.Logf("step: wait for restored pod %s/%s to become ready", namespace, restorePodName)
	k.WaitForPodReady(t, namespace, restorePodName)
	restorePVName := k.GetNamespacedJSONPath(t, namespace, "pvc/"+restorePVCName, "{.spec.volumeName}")
	if restorePVName == "" {
		t.Fatal("expected restored PVC to reference a PV")
	}

	t.Logf("step: verify VolumeSnapshot %s/%s is bound to snapshot content", namespace, snapshotName)
	contentName := k.GetNamespacedJSONPath(t, namespace, "volumesnapshot/"+snapshotName, "{.status.boundVolumeSnapshotContentName}")
	if contentName == "" {
		t.Fatal("expected bound VolumeSnapshotContent name")
	}

	t.Logf("snapshot smoke lifecycle succeeded with source PV %s, restore PV %s, restored pod %s, and VolumeSnapshotContent %s", sourcePVName, restorePVName, restorePodName, contentName)
}
