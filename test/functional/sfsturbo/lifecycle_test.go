//go:build functional

package sfsturbofunctional

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"t-cloud-public-csi-driver/test/functional/framework"
)

const (
	// SFS Turbo creation, expansion, and deletion take several minutes each.
	provisionTimeout = 20 * time.Minute
	expandTimeout    = 20 * time.Minute
	deleteTimeout    = 20 * time.Minute

	initialSize  = "500Gi"
	expandedSize = "600Gi"
	initialKiB   = 500 * 1024 * 1024
)

// TestSFSTurboSharedVolumeLifecycle provisions one ReadWriteMany SFS Turbo volume, shares it between
// two pods, expands it online, and verifies that deleting the PVC deletes the share.
func TestSFSTurboSharedVolumeLifecycle(t *testing.T) {
	cfg := framework.LoadConfig(t)
	k := framework.NewKubectl(cfg)
	driver := sfsTurboDriver(t)
	namespace := framework.TestNamespace(t)
	pvcName := "sfsturbo-pvc"
	writerPod := "sfsturbo-writer"
	readerPod := "sfsturbo-reader"
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
		t.Logf("step: delete pods, PVC, and namespace %s", namespace)
		k.DeletePod(t, namespace, writerPod)
		k.DeletePod(t, namespace, readerPod)
		k.DeletePVC(t, namespace, pvcName)
		// A leaked share blocks the deletion of the test VPC, so wait for the driver to delete it.
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
	k.ApplyManifest(t, lifecycleManifest(namespace, pvcName, writerPod, readerPod))

	t.Logf("step: wait for PVC %s/%s to bind (SFS Turbo creation takes several minutes)", namespace, pvcName)
	k.WaitForPVCBound(t, namespace, pvcName, provisionTimeout)
	pvName = k.GetNamespacedJSONPath(t, namespace, "pvc/"+pvcName, "{.spec.volumeName}")
	if pvName == "" {
		t.Fatal("expected bound PVC to reference a PV")
	}

	t.Logf("step: verify PV %s volume attributes", pvName)
	attrs := k.Run(t, "get", "pv", pvName, "-o", "jsonpath={.spec.csi.volumeAttributes}")
	framework.MustContain(t, "PV volume attributes", attrs, `"exportLocation"`)
	framework.MustContain(t, "PV volume attributes", attrs, `"availabilityZone":"`+cfg.Zone+`"`)

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
	framework.Eventually(t, expandTimeout, 10*time.Second, func() bool {
		capacity = k.GetNamespacedJSONPath(t, namespace, "pvc/"+pvcName, "{.status.capacity.storage}")
		return capacity == expandedSize
	}, func() string {
		return fmt.Sprintf("PVC %s/%s capacity is %q, want %s", namespace, pvcName, capacity, expandedSize)
	})

	t.Log("step: verify the running pod sees the expanded file system")
	var sizeKiB int64
	framework.Eventually(t, 5*time.Minute, 10*time.Second, func() bool {
		sizeKiB = mountSizeKiB(t, k, namespace, readerPod)
		return sizeKiB > initialKiB
	}, func() string {
		return fmt.Sprintf("pod %s/%s still sees %d KiB on /data after expansion", namespace, readerPod, sizeKiB)
	})

	t.Logf("step: delete pods and PVC, wait for PV %s and its share to be deleted", pvName)
	k.DeletePod(t, namespace, writerPod)
	k.DeletePod(t, namespace, readerPod)
	k.WaitForPodDeleted(t, namespace, writerPod)
	k.WaitForPodDeleted(t, namespace, readerPod)
	k.DeletePVC(t, namespace, pvcName)
	k.WaitForPVDeleted(t, pvName, deleteTimeout)

	t.Logf("SFS Turbo lifecycle succeeded with PV %s", pvName)
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

// mountSizeKiB returns the size of /data in KiB as reported by df inside the pod.
func mountSizeKiB(t *testing.T, k *framework.Kubectl, namespace, pod string) int64 {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(k.ExecInPod(t, namespace, pod, "df", "-kP", "/data")), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 2 {
		t.Fatalf("unexpected df output: %q", lines)
	}
	size, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		t.Fatalf("parse df size %q: %v", fields[1], err)
	}
	return size
}

func lifecycleManifest(namespace, pvcName, writerPod, readerPod string) string {
	pod := func(name string) string {
		return fmt.Sprintf(`---
apiVersion: v1
kind: Pod
metadata:
  name: %[1]s
  namespace: %[2]s
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
        claimName: %[3]s
`, name, namespace, pvcName)
	}

	return fmt.Sprintf(`apiVersion: v1
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
  storageClassName: %s
`, pvcName, namespace, initialSize, storageClassName) + pod(writerPod) + pod(readerPod)
}
