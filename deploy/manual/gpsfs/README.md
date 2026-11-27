# SFS General Purpose Manual Test Manifests

Validates the `csi.gpsfs.tcloudpublic.com` driver with one shared
`ReadWriteMany` volume used by two pods.

## Prerequisites

- The cluster VPC has a VPC endpoint for SFS General Purpose, see
  [docs/gpsfs.md](../../../docs/gpsfs.md#vpc-endpoint).
- The driver is deployed: `kubectl apply -k deploy/kubernetes/gpsfs` (on CCE:
  `deploy/kubernetes/cce/gpsfs`). It runs in its own namespace
  `tcloud-public-csi-gpsfs`; the test workloads use
  `tcloud-public-csi-manual-gpsfs`.
- The cloud secret `tcloud-public-csi-cloud-config` exists in
  `tcloud-public-csi-gpsfs` and contains `OS_VPC_ID` (the cluster VPC).

## Run

```bash
kubectl apply -k deploy/manual/gpsfs
kubectl -n tcloud-public-csi-manual-gpsfs get pvc gpsfs-pvc -w
kubectl -n tcloud-public-csi-manual-gpsfs wait --for=condition=Ready pod --all --timeout=5m
kubectl -n tcloud-public-csi-manual-gpsfs logs gpsfs-reader
```

The reader log must contain the line written by `gpsfs-writer`. The first mount
may fail for up to a minute after the claim is bound; the kubelet retries it.

The file system is named after the PV and must be listed in the console with
the cluster VPC in its access rules:

```bash
PV=$(kubectl -n tcloud-public-csi-manual-gpsfs get pvc gpsfs-pvc -o jsonpath='{.spec.volumeName}')
kubectl get pv "$PV" -o jsonpath='{.spec.csi.volumeHandle}{"\n"}{.spec.csi.volumeAttributes}{"\n"}'
```

## Expansion

The file system has no size limit; the claim gets the requested size and the
pods need no restart.

```bash
kubectl -n tcloud-public-csi-manual-gpsfs patch pvc gpsfs-pvc \
  -p '{"spec":{"resources":{"requests":{"storage":"2Gi"}}}}'
kubectl -n tcloud-public-csi-manual-gpsfs get pvc gpsfs-pvc -w
```

## Volume stats

```bash
NODE=$(kubectl -n tcloud-public-csi-manual-gpsfs get pod gpsfs-reader -o jsonpath='{.spec.nodeName}')
kubectl get --raw "/api/v1/nodes/${NODE}/proxy/stats/summary" \
  | grep -A12 '"name": "gpsfs-pvc"'
```

`capacityBytes` (250 TB, the service limit) and `inodes` must be set; the
service reports no usage, so `usedBytes` stays 0.

## Existing file system and directory

Mounts the file system of `gpsfs-pvc` through static PVs, the whole file system
and its directory `app`:

```bash
kubectl -n tcloud-public-csi-manual-gpsfs exec gpsfs-reader -- \
  sh -c 'mkdir -p /data/app && echo app > /data/app/app.txt'
PV=$(kubectl -n tcloud-public-csi-manual-gpsfs get pvc gpsfs-pvc -o jsonpath='{.spec.volumeName}')
FS_NAME=$(kubectl get pv "$PV" -o jsonpath='{.spec.csi.volumeHandle}')
sed "s/FS_NAME/${FS_NAME}/" deploy/manual/gpsfs/static/static.yaml | kubectl apply -f -
kubectl -n tcloud-public-csi-manual-gpsfs wait --for=condition=Ready pod/gpsfs-static --timeout=5m
kubectl -n tcloud-public-csi-manual-gpsfs logs gpsfs-static
```

The pod log must list `shared.txt` and `app` under `/root-fs` and `app.txt`
under `/app-dir`.

The directory PV has `persistentVolumeReclaimPolicy: Delete`. Deleting its claim
must not delete the file system; the PV stays `Released` with a
`VolumeFailedDelete` event:

```bash
kubectl -n tcloud-public-csi-manual-gpsfs delete pod gpsfs-static
kubectl -n tcloud-public-csi-manual-gpsfs delete pvc gpsfs-static-app gpsfs-static-root
kubectl get pv gpsfs-manual-static-app gpsfs-manual-static-root   # STATUS Released
kubectl describe pv gpsfs-manual-static-app | grep "is not deleted"
kubectl patch pv gpsfs-manual-static-app -p '{"spec":{"persistentVolumeReclaimPolicy":"Retain"}}'
kubectl delete pv gpsfs-manual-static-app gpsfs-manual-static-root
kubectl -n tcloud-public-csi-manual-gpsfs exec gpsfs-reader -- cat /data/app/app.txt   # app
```

## Volume health

Install the driver with the health monitor and check that it reports a static
volume of a missing file system as `NotFound`, and nothing for `gpsfs-pvc`:

```bash
kubectl apply -k deploy/manual/gpsfs-health
kubectl -n tcloud-public-csi-gpsfs rollout status deployment/tcloud-public-csi-controller-gpsfs
kubectl apply -f deploy/manual/gpsfs/static/missing.yaml
sleep 120
kubectl -n tcloud-public-csi-gpsfs logs deployment/tcloud-public-csi-controller-gpsfs \
  -c csi-external-health-monitor-controller | grep -i "error\|notfound"
kubectl get events -A --field-selector reason=VolumeConditionAbnormal
kubectl delete -f deploy/manual/gpsfs/static/missing.yaml
```

## Deleting a file system with data

A file system can be deleted only when it is empty. Delete the claim with the
data still in place:

```bash
PV=$(kubectl -n tcloud-public-csi-manual-gpsfs get pvc gpsfs-pvc -o jsonpath='{.spec.volumeName}')
FS_NAME=$(kubectl get pv "$PV" -o jsonpath='{.spec.csi.volumeHandle}')
kubectl -n tcloud-public-csi-manual-gpsfs delete pod gpsfs-writer gpsfs-reader
kubectl -n tcloud-public-csi-manual-gpsfs delete pvc gpsfs-pvc
kubectl get pv "$PV"   # STATUS Released
kubectl describe pv "$PV" | grep "not empty"
```

The file system must still exist. Remove its data through a static PV; the
retried deletion then deletes the PV and the file system:

```bash
sed "s/FS_NAME/${FS_NAME}/" deploy/manual/gpsfs/static/static.yaml | kubectl apply -f -
kubectl -n tcloud-public-csi-manual-gpsfs wait --for=condition=Ready pod/gpsfs-static --timeout=5m
kubectl -n tcloud-public-csi-manual-gpsfs exec gpsfs-static -- sh -c 'rm -rf /root-fs/* /root-fs/.[!.]*'
kubectl -n tcloud-public-csi-manual-gpsfs delete pod gpsfs-static
kubectl -n tcloud-public-csi-manual-gpsfs delete pvc gpsfs-static-app gpsfs-static-root
kubectl patch pv gpsfs-manual-static-app -p '{"spec":{"persistentVolumeReclaimPolicy":"Retain"}}'
kubectl delete pv gpsfs-manual-static-app gpsfs-manual-static-root
kubectl wait --for=delete pv/"$PV" --timeout=15m
```

The file system must no longer be listed in the console.

## Cleanup

```bash
kubectl delete -k deploy/manual/gpsfs --ignore-not-found
```

Only empty file systems are deleted with their claim; remove the data first as
shown above.
