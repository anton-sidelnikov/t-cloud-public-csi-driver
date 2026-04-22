//go:build functional

package evsfunctional

import (
	"fmt"
	"strings"
	"testing"
)

func TestEVSFilesystemVolumeLifecycle(t *testing.T) {
	cfg := loadTestConfig(t)
	k := newKubectl(cfg)
	namespace := testNamespace(t)
	pvcName := "evs-fs-pvc"
	podName := "evs-fs-pod"
	storageClassName := "tcloud-public-evs"
	testFile := "/data/app/health.txt"
	testValue := "functional-evs-ok"

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("step: collect debug output from namespace %s", namespace)
			k.collectNamespaceDebug(t, namespace)
			k.collectDriverDebug(t)
		}
		if !cfg.keepResources {
			t.Logf("step: delete pvc %s/%s", namespace, pvcName)
			k.deletePvc(t, namespace, pvcName)
			t.Logf("step: delete pod %s/%s", namespace, podName)
			k.deletePod(t, namespace, podName)
			t.Logf("step: delete namespace %s", namespace)
			k.deleteNamespace(t, namespace)
		}
	})

	ensureDriverInstalled(t, cfg, k)

	t.Logf("step: create namespace %s", namespace)
	k.createNamespace(t, namespace)

	manifest := fmt.Sprintf(`apiVersion: v1
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
          tail -f /dev/null
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, pvcName, namespace, storageClassName, podName, namespace, testValue, testFile, pvcName)

	t.Logf("step: create PVC %s/%s and pod %s/%s", namespace, pvcName, namespace, podName)
	k.applyManifest(t, manifest)

	t.Logf("step: wait for PVC %s/%s to bind", namespace, pvcName)
	k.waitForPVCBound(t, namespace, pvcName)
	t.Logf("step: wait for pod %s/%s to become ready", namespace, podName)
	k.waitForPodReady(t, namespace, podName)

	t.Logf("step: verify pod %s/%s can read %s", namespace, podName, testFile)
	output := strings.TrimSpace(k.execInPod(t, namespace, podName, "cat", testFile))
	if output != testValue {
		t.Fatalf("unexpected file contents in %s: got %q want %q", testFile, output, testValue)
	}

	t.Logf("step: verify PVC %s/%s is backed by a PV", namespace, pvcName)
	pvName := k.getNamespacedJSONPath(t, namespace, "pvc/"+pvcName, "{.spec.volumeName}")
	if pvName == "" {
		t.Fatal("expected bound PVC to reference a PV")
	}
	t.Logf("filesystem lifecycle succeeded with PV %s", pvName)
}
