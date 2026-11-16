//go:build functional

package evsfunctional

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"t-cloud-public-csi-driver/test/functional/framework"
)

const zoneTopologyKey = "topology.evs.tcloudpublic.com/zone"

// TestEVSAvailabilityZoneAutodetection runs the driver without OS_AVAILABILITY_ZONE and verifies
// that nodes detect their zone from the ECS metadata service and the controller provisions
// volumes in the zone requested through CSI topology.
func TestEVSAvailabilityZoneAutodetection(t *testing.T) {
	cfg := loadTestConfig(t)
	k := framework.NewKubectl(cfg)
	namespace := framework.TestNamespace(t)
	pvcName := "evs-zone-pvc"
	podName := "evs-zone-pod"

	ensureDriverInstalled(t, cfg, k)

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("step: collect debug output from namespace %s", namespace)
			k.CollectNamespaceDebug(t, namespace)
			collectDriverDebug(t, k)
		}
		if !cfg.KeepResources {
			t.Logf("step: delete pod %s/%s", namespace, podName)
			k.DeletePod(t, namespace, podName)
			t.Logf("step: delete pvc %s/%s", namespace, pvcName)
			k.DeletePVC(t, namespace, pvcName)
			t.Logf("step: delete namespace %s", namespace)
			k.DeleteNamespace(t, namespace)
		}
		t.Log("step: restore cloud secret with OS_AVAILABILITY_ZONE and restart the driver")
		evsDriver(t).ApplyCloudSecret(t, k, cfg, cfg.Zone)
		evsDriver(t).Restart(t, k)
	})

	t.Log("step: remove OS_AVAILABILITY_ZONE from the cloud secret and restart the driver")
	evsDriver(t).ApplyCloudSecret(t, k, cfg, "")
	evsDriver(t).Restart(t, k)

	nodeNames := evsDriver(t).NodeNames(t, k)
	if len(nodeNames) == 0 {
		t.Fatal("no CSI node plugin pods found")
	}

	t.Log("step: verify every node plugin resolved the zone from the metadata service")
	waitForNodeZoneFromMetadata(t, k, cfg.Zone, len(nodeNames))
	for _, node := range nodeNames {
		label := strings.TrimSpace(k.Run(t, "get", "node", node, "-o", fmt.Sprintf("jsonpath={.metadata.labels.%s}", strings.ReplaceAll(zoneTopologyKey, ".", `\.`))))
		if label != cfg.Zone {
			t.Fatalf("node %s has %s=%q, want %q", node, zoneTopologyKey, label, cfg.Zone)
		}
	}

	t.Logf("step: create namespace %s", namespace)
	k.CreateNamespace(t, namespace)

	manifest := fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: tcloud-public-evs
---
apiVersion: v1
kind: Pod
metadata:
  name: %[3]s
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
        claimName: %[1]s
`, pvcName, namespace, podName)

	t.Logf("step: create PVC %s/%s and pod %s/%s without a configured zone", namespace, pvcName, namespace, podName)
	k.ApplyManifest(t, manifest)
	k.WaitForPVCBound(t, namespace, pvcName, 5*time.Minute)
	k.WaitForPodReady(t, namespace, podName)

	pvName := k.GetNamespacedJSONPath(t, namespace, "pvc/"+pvcName, "{.spec.volumeName}")
	if pvName == "" {
		t.Fatal("expected bound PVC to reference a PV")
	}

	t.Logf("step: verify PV %s was provisioned in zone %s", pvName, cfg.Zone)
	volumeZone := strings.TrimSpace(k.Run(t, "get", "pv", pvName, "-o", "jsonpath={.spec.csi.volumeAttributes.availabilityZone}"))
	if volumeZone != cfg.Zone {
		t.Fatalf("PV %s availabilityZone=%q, want %q", pvName, volumeZone, cfg.Zone)
	}
	affinity := k.Run(t, "get", "pv", pvName, "-o", "jsonpath={.spec.nodeAffinity.required.nodeSelectorTerms[*].matchExpressions[*]}")
	framework.MustContain(t, "PV node affinity key", affinity, zoneTopologyKey)
	framework.MustContain(t, "PV node affinity zone", affinity, cfg.Zone)
}

func waitForNodeZoneFromMetadata(t *testing.T, k *framework.Kubectl, zone string, nodes int) {
	t.Helper()

	want := fmt.Sprintf(`"availability_zone":%q`, zone)
	deadline := time.Now().Add(3 * time.Minute)
	for {
		logs, err := evsDriver(t).NodeLogs(k)
		if err == nil {
			matches := 0
			for _, line := range strings.Split(logs, "\n") {
				if strings.Contains(line, "resolved node availability zone") && strings.Contains(line, `"source":"metadata"`) && strings.Contains(line, want) {
					matches++
				}
			}
			if matches >= nodes {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d node plugins to report zone %s from the metadata service; last logs:\n%s", nodes, zone, logs)
		}
		time.Sleep(5 * time.Second)
	}
}
