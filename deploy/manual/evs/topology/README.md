# EVS Availability Zone Manual Workflow

This folder validates availability zone autodetection and topology-aware provisioning.

Prerequisites:

- the CSI controller and node manifests from `deploy/kubernetes` are already running
- the driver image in those manifests contains availability zone autodetection
- the node plugin can reach the ECS metadata service `http://169.254.169.254` (the DaemonSet uses `hostNetwork: true`)
- the common manual namespace and storage class exist, or you apply them as part of the steps below

Objects:

- `PersistentVolumeClaim/evs-zone-pvc`
- `Pod/evs-zone-pod`
- `StorageClass/tcloud-public-evs-zone-mismatch`
- `PersistentVolumeClaim/evs-zone-mismatch-pvc`
- `Pod/evs-zone-mismatch-pod`

## 1. Run the driver without `OS_AVAILABILITY_ZONE`

Remove the key from the in-cluster secret only. Keep your local `deploy/kubernetes/secret.yaml` unchanged.

```bash
kubectl -n tcloud-public-csi-system patch secret tcloud-public-csi-cloud-config --type json \
  -p '[{"op":"remove","path":"/data/OS_AVAILABILITY_ZONE"}]'
kubectl -n tcloud-public-csi-system rollout restart deployment/tcloud-public-csi-controller daemonset/tcloud-public-csi-node
kubectl -n tcloud-public-csi-system rollout status deployment/tcloud-public-csi-controller --timeout=10m
kubectl -n tcloud-public-csi-system rollout status daemonset/tcloud-public-csi-node --timeout=10m
```

If the patch fails because the key does not exist, the secret already has no zone; continue.

## 2. Verify node zone detection

Every node plugin should log `"source":"metadata"` with its instance zone:

```bash
kubectl -n tcloud-public-csi-system logs -l app=tcloud-public-csi-node -c tcloud-public-csi-driver --tail=-1 \
  | grep 'resolved node availability zone'
kubectl get nodes -L topology.evs.tcloudpublic.com/zone
```

Expected: the `topology.evs.tcloudpublic.com/zone` label of each node matches the zone in the log for that node. A warning `OS_AVAILABILITY_ZONE differs from the instance availability zone` means the secret's zone was wrong.

## 3. Provision a volume from the node topology

```bash
kubectl apply -f deploy/manual/evs/common/namespace.yaml
kubectl apply -f deploy/manual/evs/common/storageclass-delete.yaml
kubectl -n tcloud-public-csi-manual apply -f deploy/manual/evs/topology/volume.yaml
kubectl -n tcloud-public-csi-manual wait --for=jsonpath='{.status.phase}'=Bound pvc/evs-zone-pvc --timeout=10m
kubectl -n tcloud-public-csi-manual wait --for=condition=Ready pod/evs-zone-pod --timeout=10m
```

Compare the node zone of the pod with the zone of the volume:

```bash
NODE=$(kubectl -n tcloud-public-csi-manual get pod evs-zone-pod -o jsonpath='{.spec.nodeName}')
PV=$(kubectl -n tcloud-public-csi-manual get pvc evs-zone-pvc -o jsonpath='{.spec.volumeName}')
kubectl get node "$NODE" -o jsonpath='{.metadata.labels.topology\.evs\.tcloudpublic\.com/zone}{"\n"}'
kubectl get pv "$PV" -o jsonpath='{.spec.csi.volumeAttributes.availabilityZone}{"\n"}{.spec.nodeAffinity.required}{"\n"}'
kubectl -n tcloud-public-csi-manual exec evs-zone-pod -- cat /data/app/zone.txt
```

Expected: all three zone values are identical, and the pod can read the file.

## 4. Verify that an explicit zone outside the topology is rejected

```bash
kubectl apply -f deploy/manual/evs/topology/mismatch.yaml
sleep 30
kubectl -n tcloud-public-csi-manual get pvc evs-zone-mismatch-pvc
kubectl -n tcloud-public-csi-manual get events --field-selector involvedObject.name=evs-zone-mismatch-pvc
```

Expected: the PVC stays `Pending`, and a `ProvisioningFailed` event contains `is not in the accessible topology`. No EVS volume is created.

## 5. Restore the secret

```bash
kubectl apply -f deploy/kubernetes/secret.yaml
kubectl -n tcloud-public-csi-system rollout restart deployment/tcloud-public-csi-controller daemonset/tcloud-public-csi-node
kubectl -n tcloud-public-csi-system rollout status daemonset/tcloud-public-csi-node --timeout=10m
```

If a node's reported zone ever changes, remove the stale label first, otherwise the registrar fails with `detected topology value collision`:

```bash
kubectl label node <node> topology.evs.tcloudpublic.com/zone-
```

## Cleanup

```bash
kubectl delete -f deploy/manual/evs/topology/mismatch.yaml --ignore-not-found=true
kubectl -n tcloud-public-csi-manual delete -f deploy/manual/evs/topology/volume.yaml --ignore-not-found=true
```
