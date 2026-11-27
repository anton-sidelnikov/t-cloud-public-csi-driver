//go:build functional

package gpsfsfunctional

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"t-cloud-public-csi-driver/test/functional/framework"
)

const (
	// The provisioner retries a refused deletion with a growing interval of up to five minutes.
	deleteTimeout = 15 * time.Minute

	initialSize  = "1Gi"
	expandedSize = "2Gi"
)

// TestGPSFSSharedVolumeLifecycle provisions one ReadWriteMany file system, shares it between two
// pods, and expands it. Deleting the PVC is refused while the file system has data; after the
// data is removed through a static PV, the retried deletion deletes the file system.
func TestGPSFSSharedVolumeLifecycle(t *testing.T) {
	cfg := framework.LoadConfig(t)
	k := framework.NewKubectl(cfg)
	driver := gpsfsDriver(t)
	client := newClient(t, loadConfig(t))
	namespace := framework.TestNamespace(t)
	pvcName := "gpsfs-pvc"
	writerPod := "gpsfs-writer"
	readerPod := "gpsfs-reader"
	staticPV := "gpsfs-static-" + namespace
	staticPVC := "gpsfs-static"
	cleanerPod := "gpsfs-cleaner"
	var pvName string

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("step: collect debug output from namespace %s", namespace)
			k.CollectNamespaceDebug(t, namespace)
			driver.CollectDebug(t, k)
		}
		if cfg.KeepResources {
			return
		}
		t.Logf("step: delete pods, PVCs, static PV, and namespace %s", namespace)
		for _, pod := range []string{writerPod, readerPod, cleanerPod} {
			k.DeletePod(t, namespace, pod)
		}
		k.DeletePVC(t, namespace, pvcName)
		k.DeletePVC(t, namespace, staticPVC)
		k.Run(t, "delete", "pv", staticPV, "--ignore-not-found=true", "--wait=false")
		if pvName != "" {
			if out, err := k.RunCommand("wait", "--for=delete", "pv/"+pvName, "--timeout="+deleteTimeout.String()); err != nil {
				t.Errorf("PV %s was not deleted: %v\n%s", pvName, err, out)
			}
		}
		k.DeleteNamespace(t, namespace)
	})

	ensureDriverInstalled(t, cfg, k)

	t.Logf("step: create namespace %s", namespace)
	k.CreateNamespace(t, namespace)

	t.Logf("step: create RWX PVC %s/%s (%s) and pods %s, %s", namespace, pvcName, initialSize, writerPod, readerPod)
	k.ApplyManifest(t, pvcManifest(namespace, pvcName, storageClassName, "")+podManifest(namespace, writerPod, pvcName)+podManifest(namespace, readerPod, pvcName))
	k.WaitForPVCBound(t, namespace, pvcName, 5*time.Minute)
	pvName = k.GetNamespacedJSONPath(t, namespace, "pvc/"+pvcName, "{.spec.volumeName}")
	if pvName == "" {
		t.Fatal("expected bound PVC to reference a PV")
	}
	fileSystem := strings.TrimSpace(k.Run(t, "get", "pv", pvName, "-o", "jsonpath={.spec.csi.volumeHandle}"))

	t.Logf("step: verify PV %s volume attributes and file system %s", pvName, fileSystem)
	attrs := k.Run(t, "get", "pv", pvName, "-o", "jsonpath={.spec.csi.volumeAttributes}")
	framework.MustContain(t, "PV volume attributes", attrs, `"exportLocation":"`+fileSystem+".sfs3.")
	if names := listFileSystems(t, client); !slices.Contains(names, fileSystem) {
		t.Fatalf("file system %s is not listed: %v", fileSystem, names)
	}

	// The first mounts may fail until the access rule for the VPC takes effect; kubelet retries them.
	k.WaitForPodReady(t, namespace, writerPod)
	k.WaitForPodReady(t, namespace, readerPod)

	t.Log("step: verify both pods see each other's writes")
	writerValue := "written-by-" + writerPod
	k.ExecInPod(t, namespace, writerPod, "sh", "-c", "echo "+writerValue+" > /data/from-writer.txt")
	waitForFileContent(t, k, namespace, readerPod, "/data/from-writer.txt", writerValue)
	readerValue := "written-by-" + readerPod
	k.ExecInPod(t, namespace, readerPod, "sh", "-c", "echo "+readerValue+" > /data/from-reader.txt")
	waitForFileContent(t, k, namespace, writerPod, "/data/from-reader.txt", readerValue)

	t.Logf("step: verify no VolumeAttachment exists for PV %s", pvName)
	attachments := strings.TrimSpace(k.Run(t, "get", "volumeattachment", "-o", `jsonpath={range .items[?(@.spec.source.persistentVolumeName=="`+pvName+`")]}{.metadata.name}{"\n"}{end}`))
	if attachments != "" {
		t.Fatalf("unexpected VolumeAttachments for PV %s: %s", pvName, attachments)
	}

	t.Logf("step: expand PVC %s/%s to %s", namespace, pvcName, expandedSize)
	k.Run(t, "-n", namespace, "patch", "pvc", pvcName, "--type=merge", "-p", fmt.Sprintf(`{"spec":{"resources":{"requests":{"storage":%q}}}}`, expandedSize))
	var capacity string
	framework.Eventually(t, 5*time.Minute, 5*time.Second, func() bool {
		capacity = k.GetNamespacedJSONPath(t, namespace, "pvc/"+pvcName, "{.status.capacity.storage}")
		return capacity == expandedSize
	}, func() string {
		return fmt.Sprintf("PVC %s/%s capacity is %q, want %s", namespace, pvcName, capacity, expandedSize)
	})

	t.Logf("step: delete pods and PVC while file system %s has data", fileSystem)
	k.DeletePod(t, namespace, writerPod)
	k.DeletePod(t, namespace, readerPod)
	k.WaitForPodDeleted(t, namespace, writerPod)
	k.WaitForPodDeleted(t, namespace, readerPod)
	k.DeletePVC(t, namespace, pvcName)

	t.Logf("step: verify the deletion of PV %s is refused", pvName)
	var events string
	framework.Eventually(t, 5*time.Minute, 10*time.Second, func() bool {
		events, _ = k.RunCommand("get", "events", "-A", "--field-selector", "involvedObject.name="+pvName, "-o", "jsonpath={range .items[*]}{.reason}: {.message}{\"\\n\"}{end}")
		return strings.Contains(events, "VolumeFailedDelete") && strings.Contains(events, "not empty")
	}, func() string {
		return fmt.Sprintf("no refused deletion reported for PV %s:\n%s", pvName, events)
	})
	if names := listFileSystems(t, client); !slices.Contains(names, fileSystem) {
		t.Fatalf("non-empty file system %s was deleted", fileSystem)
	}

	t.Logf("step: mount file system %s through static PV %s and remove its data", fileSystem, staticPV)
	k.ApplyManifest(t, staticPVManifest(staticPV, fileSystem, namespace, staticPVC)+pvcManifest(namespace, staticPVC, "", staticPV)+podManifest(namespace, cleanerPod, staticPVC))
	k.WaitForPodReady(t, namespace, cleanerPod)
	waitForFileContent(t, k, namespace, cleanerPod, "/data/from-writer.txt", writerValue)
	k.ExecInPod(t, namespace, cleanerPod, "sh", "-c", "rm -rf /data/* /data/.[!.]*")
	k.DeletePod(t, namespace, cleanerPod)
	k.WaitForPodDeleted(t, namespace, cleanerPod)
	k.DeletePVC(t, namespace, staticPVC)
	k.Run(t, "delete", "pv", staticPV, "--wait=true", "--timeout=2m")

	t.Logf("step: wait for the retried deletion of PV %s and file system %s", pvName, fileSystem)
	k.WaitForPVDeleted(t, pvName, deleteTimeout)
	if names := listFileSystems(t, client); slices.Contains(names, fileSystem) {
		t.Fatalf("file system %s is still listed after PV %s was deleted", fileSystem, pvName)
	}

	t.Logf("SFS General Purpose lifecycle succeeded with PV %s", pvName)
	pvName = ""
}

// waitForFileContent waits until the pod reads want from path. NFS clients on other
// nodes may cache directory attributes for a short time.
func waitForFileContent(t *testing.T, k *framework.Kubectl, namespace, pod, path, want string) {
	t.Helper()

	var got string
	framework.Eventually(t, time.Minute, 5*time.Second, func() bool {
		out, err := k.RunCommand("-n", namespace, "exec", pod, "--", "cat", path)
		got = strings.TrimSpace(out)
		return err == nil && got == want
	}, func() string {
		return fmt.Sprintf("pod %s/%s reads %q from %s, want %q", namespace, pod, got, path, want)
	})
}

func pvcManifest(namespace, name, storageClass, volumeName string) string {
	manifest := fmt.Sprintf(`---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
spec:
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: %s
  storageClassName: %q
`, name, namespace, initialSize, storageClass)
	if volumeName != "" {
		manifest += "  volumeName: " + volumeName + "\n"
	}
	return manifest
}

func podManifest(namespace, name, pvcName string) string {
	return fmt.Sprintf(`---
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
      command: ["sh", "-c", "tail -f /dev/null"]
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, name, namespace, pvcName)
}

func staticPVManifest(name, fileSystem, claimNamespace, claimName string) string {
	return fmt.Sprintf(`---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: %s
spec:
  accessModes:
    - ReadWriteMany
  capacity:
    storage: %s
  persistentVolumeReclaimPolicy: Retain
  storageClassName: ""
  claimRef:
    namespace: %s
    name: %s
  csi:
    driver: %s
    volumeHandle: %s
`, name, initialSize, claimNamespace, claimName, driverName, fileSystem)
}
