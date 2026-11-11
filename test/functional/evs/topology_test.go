//go:build functional

package evsfunctional

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const zoneTopologyKey = "topology.evs.tcloudpublic.com/zone"

// TestEVSAvailabilityZoneAutodetection runs the driver without OS_AVAILABILITY_ZONE and verifies
// that nodes detect their zone from the ECS metadata service and the controller provisions
// volumes in the zone requested through CSI topology.
func TestEVSAvailabilityZoneAutodetection(t *testing.T) {
	cfg := loadTestConfig(t)
	k := newKubectl(cfg)
	namespace := testNamespace(t)
	pvcName := "evs-zone-pvc"
	podName := "evs-zone-pod"

	ensureDriverInstalled(t, cfg, k)

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("step: collect debug output from namespace %s", namespace)
			k.collectNamespaceDebug(t, namespace)
			k.collectDriverDebug(t)
		}
		if !cfg.keepResources {
			t.Logf("step: delete pod %s/%s", namespace, podName)
			k.deletePod(t, namespace, podName)
			t.Logf("step: delete pvc %s/%s", namespace, pvcName)
			k.deletePvc(t, namespace, pvcName)
			t.Logf("step: delete namespace %s", namespace)
			k.deleteNamespace(t, namespace)
		}
		t.Log("step: restore cloud secret with OS_AVAILABILITY_ZONE and restart the driver")
		k.createOrUpdateCloudSecret(t, cfg)
		k.restartDriver(t)
	})

	t.Log("step: remove OS_AVAILABILITY_ZONE from the cloud secret and restart the driver")
	k.applyCloudSecret(t, cfg, "")
	k.restartDriver(t)

	nodeNames := strings.Fields(k.run(t, "-n", systemNamespace, "get", "pods", "-l", "app=tcloud-public-csi-node", "-o", "jsonpath={.items[*].spec.nodeName}"))
	if len(nodeNames) == 0 {
		t.Fatal("no CSI node plugin pods found")
	}

	t.Log("step: verify every node plugin resolved the zone from the metadata service")
	k.waitForNodeZoneFromMetadata(t, cfg.zone, len(nodeNames))
	for _, node := range nodeNames {
		label := strings.TrimSpace(k.run(t, "get", "node", node, "-o", fmt.Sprintf("jsonpath={.metadata.labels.%s}", strings.ReplaceAll(zoneTopologyKey, ".", `\.`))))
		if label != cfg.zone {
			t.Fatalf("node %s has %s=%q, want %q", node, zoneTopologyKey, label, cfg.zone)
		}
	}

	t.Logf("step: create namespace %s", namespace)
	k.createNamespace(t, namespace)

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
	k.applyManifest(t, manifest)
	k.waitForPVCBound(t, namespace, pvcName)
	k.waitForPodReady(t, namespace, podName)

	pvName := k.getNamespacedJSONPath(t, namespace, "pvc/"+pvcName, "{.spec.volumeName}")
	if pvName == "" {
		t.Fatal("expected bound PVC to reference a PV")
	}

	t.Logf("step: verify PV %s was provisioned in zone %s", pvName, cfg.zone)
	volumeZone := strings.TrimSpace(k.run(t, "get", "pv", pvName, "-o", "jsonpath={.spec.csi.volumeAttributes.availabilityZone}"))
	if volumeZone != cfg.zone {
		t.Fatalf("PV %s availabilityZone=%q, want %q", pvName, volumeZone, cfg.zone)
	}
	affinity := k.run(t, "get", "pv", pvName, "-o", "jsonpath={.spec.nodeAffinity.required.nodeSelectorTerms[*].matchExpressions[*]}")
	k.mustOutputContain(t, "PV node affinity key", affinity, zoneTopologyKey)
	k.mustOutputContain(t, "PV node affinity zone", affinity, cfg.zone)
}

func (k kubectl) waitForNodeZoneFromMetadata(t *testing.T, zone string, nodes int) {
	t.Helper()

	want := fmt.Sprintf(`"availability_zone":%q`, zone)
	deadline := time.Now().Add(3 * time.Minute)
	for {
		logs, err := k.runCommand("-n", systemNamespace, "logs", "-l", "app=tcloud-public-csi-node", "-c", "tcloud-public-csi-driver", "--tail=-1")
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
