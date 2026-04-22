# EVS Snapshot Manual Workflow

This folder contains the ordered EVS snapshot validation flow.

Prerequisites:

- the CSI controller and node manifests from `deploy/kubernetes` are already running
- the driver image in those manifests points to a real published image
- the Kubernetes snapshot CRDs are installed in the cluster
- the bundled `snapshot-controller` from `deploy/kubernetes` is running
- the common manual namespace and storage class exist, or you apply them as part of the steps below

Objects:

- `PersistentVolumeClaim/evs-snap-src-pvc`
- `Pod/evs-snap-src-pod`
- `VolumeSnapshot/evs-snap-src`
- `PersistentVolumeClaim/evs-snap-restore-pvc`
- `Pod/evs-snap-restore-pod`

Apply in order:

```bash
kubectl apply -f deploy/manual/evs/common/namespace.yaml
kubectl apply -f deploy/manual/evs/common/storageclass-delete.yaml
kubectl -n tcloud-public-csi-manual apply -f deploy/manual/evs/snapshot/source.yaml
kubectl -n tcloud-public-csi-manual wait --for=jsonpath='{.status.phase}'=Bound pvc/evs-snap-src-pvc --timeout=10m
kubectl -n tcloud-public-csi-manual wait --for=condition=Ready pod/evs-snap-src-pod --timeout=10m
kubectl -n tcloud-public-csi-manual exec evs-snap-src-pod -- cat /data/app/snapshot.txt

kubectl -n tcloud-public-csi-manual apply -f deploy/manual/evs/snapshot/volume.yaml
kubectl -n tcloud-public-csi-manual wait --for=jsonpath='{.status.readyToUse}'=true volumesnapshot/evs-snap-src --timeout=10m
kubectl -n tcloud-public-csi-manual get volumesnapshot evs-snap-src -o yaml

kubectl -n tcloud-public-csi-manual apply -f deploy/manual/evs/snapshot/restore.yaml
kubectl -n tcloud-public-csi-manual wait --for=jsonpath='{.status.phase}'=Bound pvc/evs-snap-restore-pvc --timeout=10m
kubectl -n tcloud-public-csi-manual wait --for=condition=Ready pod/evs-snap-restore-pod --timeout=10m
kubectl -n tcloud-public-csi-manual exec evs-snap-restore-pod -- cat /data/app/snapshot.txt
```

Useful snapshot-specific checks:

```bash
kubectl -n tcloud-public-csi-manual get volumesnapshot,volumesnapshotcontent
kubectl -n tcloud-public-csi-system logs deployment/tcloud-public-snapshot-controller --tail=200
kubectl -n tcloud-public-csi-system logs deployment/tcloud-public-csi-controller -c csi-snapshotter --tail=200
kubectl -n tcloud-public-csi-system logs deployment/tcloud-public-csi-controller -c tcloud-public-csi-driver --tail=200
```

Cleanup:

```bash
kubectl -n tcloud-public-csi-manual delete -f deploy/manual/evs/snapshot/restore.yaml --ignore-not-found=true
kubectl -n tcloud-public-csi-manual delete -f deploy/manual/evs/snapshot/volume.yaml --ignore-not-found=true
kubectl -n tcloud-public-csi-manual delete -f deploy/manual/evs/snapshot/source.yaml --ignore-not-found=true
```
