# SFS Turbo Manual Test Manifests

Validates the `csi.sfsturbo.tcloudpublic.com` driver with one shared
`ReadWriteMany` volume used by two pods.

> SFS Turbo is billed by provisioned capacity and the minimum share size is
> 500 GiB. Clean up as soon as the check is done.

## Prerequisites

- The SFS Turbo driver is deployed: `kubectl apply -k deploy/kubernetes/sfsturbo` (on CCE: `deploy/kubernetes/cce/sfsturbo`).
  It runs in its own namespace `tcloud-public-csi-sfsturbo`, separate from EVS
  (`tcloud-public-csi-system`); the test workloads use a third namespace,
  `tcloud-public-csi-manual-sfsturbo`.
- The cloud secret `tcloud-public-csi-cloud-config` exists in
  `tcloud-public-csi-sfsturbo` and contains `OS_AVAILABILITY_ZONE`, `OS_VPC_ID`, `OS_SUBNET_ID`
  and `OS_SECURITY_GROUP_ID` (or set the matching StorageClass parameters).
- The nodes are in the same VPC as the share. SFS Turbo opens the NFS ports
  (111, 2049, 2051, 2052, 20048) in the configured security group itself;
  keep them open if you manage the group manually.

## Run

```bash
kubectl apply -k deploy/manual/sfsturbo
# Creation takes several minutes.
kubectl -n tcloud-public-csi-manual-sfsturbo get pvc sfsturbo-pvc -w
kubectl -n tcloud-public-csi-manual-sfsturbo wait --for=condition=Ready pod --all --timeout=10m
kubectl -n tcloud-public-csi-manual-sfsturbo logs sfsturbo-reader
```

The reader log must contain the line written by `sfsturbo-writer`.

## Controller restart during creation

Run this right after `kubectl apply`, while the PVC is still `Pending`. The
restarted controller must find the share by name and wait for it instead of
creating a second share or rejecting it with `AlreadyExists`:

```bash
kubectl -n tcloud-public-csi-sfsturbo logs deployment/tcloud-public-csi-controller-sfsturbo \
  -c tcloud-public-csi-driver | grep "creating SFS Turbo share"
kubectl -n tcloud-public-csi-sfsturbo delete pod -l app=tcloud-public-csi-controller-sfsturbo
kubectl -n tcloud-public-csi-sfsturbo rollout status deployment/tcloud-public-csi-controller-sfsturbo
kubectl -n tcloud-public-csi-manual-sfsturbo get pvc sfsturbo-pvc -w
kubectl -n tcloud-public-csi-sfsturbo logs deployment/tcloud-public-csi-controller-sfsturbo \
  -c tcloud-public-csi-driver | grep -E "reusing|does not match|creating SFS Turbo share"
```

The last command must print only a `reusing ... SFS Turbo share` line, and the
console must show a single share named after the PV.

## Expansion

SFS Turbo grows in steps of at least 100 GiB. Expansion is online and needs no
pod restart.

```bash
kubectl -n tcloud-public-csi-manual-sfsturbo patch pvc sfsturbo-pvc \
  -p '{"spec":{"resources":{"requests":{"storage":"600Gi"}}}}'
kubectl -n tcloud-public-csi-manual-sfsturbo get pvc sfsturbo-pvc -w
kubectl -n tcloud-public-csi-manual-sfsturbo exec sfsturbo-reader -- df -h /data
```

## Volume stats

The node plugin reports usage through `NodeGetVolumeStats`, so the kubelet
summary contains capacity and usage of the share:

```bash
NODE=$(kubectl -n tcloud-public-csi-manual-sfsturbo get pod sfsturbo-reader -o jsonpath='{.spec.nodeName}')
kubectl get --raw "/api/v1/nodes/${NODE}/proxy/stats/summary" \
  | grep -A12 '"name": "sfsturbo-pvc"'
```

`capacityBytes` must match `df -h /data` in the pod, and `usedBytes` and
`inodesUsed` must be set.

## Retain reclaim policy

This check reuses the share created above, so no second share is billed. Switch the PV to
`Retain`, delete the claim, and check that the share and its data survive:

```bash
PV=$(kubectl -n tcloud-public-csi-manual-sfsturbo get pvc sfsturbo-pvc -o jsonpath='{.spec.volumeName}')
kubectl patch pv "$PV" -p '{"spec":{"persistentVolumeReclaimPolicy":"Retain"}}'
kubectl -n tcloud-public-csi-manual-sfsturbo delete pod sfsturbo-writer sfsturbo-reader
kubectl -n tcloud-public-csi-manual-sfsturbo delete pvc sfsturbo-pvc
kubectl get pv "$PV"   # STATUS Released
kubectl -n tcloud-public-csi-sfsturbo logs deployment/tcloud-public-csi-controller-sfsturbo \
  -c tcloud-public-csi-driver | grep -c "deleting SFS Turbo share"   # 0
```

The share must still be `Available` in the console. Bind the retained PV to a
new claim and read the old data:

```bash
kubectl patch pv "$PV" --type json -p '[{"op":"remove","path":"/spec/claimRef"}]'
sed "s/PV_NAME/$PV/" deploy/manual/sfsturbo/retain/pvc.yaml | kubectl apply -f -
kubectl -n tcloud-public-csi-manual-sfsturbo apply -f deploy/manual/sfsturbo/pod-reader.yaml
kubectl -n tcloud-public-csi-manual-sfsturbo wait --for=condition=Ready pod/sfsturbo-reader --timeout=5m
kubectl -n tcloud-public-csi-manual-sfsturbo logs sfsturbo-reader   # the line written before
```

Restore `Delete` before the cleanup so that the share is removed with the claim:

```bash
kubectl patch pv "$PV" -p '{"spec":{"persistentVolumeReclaimPolicy":"Delete"}}'
```

## Existing file system and directory

Run this before the cleanup: it mounts the share of `sfsturbo-pvc` through static
PVs, the whole share and its directory `app`. Install the driver with the health
monitor first, it is checked below:

```bash
kubectl apply -k deploy/manual/sfsturbo-health
kubectl -n tcloud-public-csi-sfsturbo rollout status deployment/tcloud-public-csi-controller-sfsturbo
kubectl -n tcloud-public-csi-manual-sfsturbo exec sfsturbo-reader -- \
  sh -c 'mkdir -p /data/app && echo app > /data/app/app.txt'
PV=$(kubectl -n tcloud-public-csi-manual-sfsturbo get pvc sfsturbo-pvc -o jsonpath='{.spec.volumeName}')
SHARE_ID=$(kubectl get pv "$PV" -o jsonpath='{.spec.csi.volumeHandle}')
sed "s/SHARE_ID/${SHARE_ID}/" deploy/manual/sfsturbo/static/static.yaml | kubectl apply -f -
kubectl -n tcloud-public-csi-manual-sfsturbo wait --for=condition=Ready pod/sfsturbo-static --timeout=5m
kubectl -n tcloud-public-csi-manual-sfsturbo logs sfsturbo-static
kubectl -n tcloud-public-csi-sfsturbo logs daemonset/tcloud-public-csi-node-sfsturbo \
  -c tcloud-public-csi-driver | grep "looked up file share volume context"
```

The pod log must list `shared.txt` and `app` under `/root-share` and `app.txt`
under `/app-dir`. The static PVs have no export location, the node looks it up.

The root PV has the same volume handle as `sfsturbo-pvc` and shares its staging
mount, so its lookup is logged only when no pod of `sfsturbo-pvc` runs on the
node. After the pods are deleted, both staging mounts must be removed from the
node (the node log shows `unmounting volume path` for both `globalmount` paths).

The directory PV has `persistentVolumeReclaimPolicy: Delete`. Deleting its claim
must not delete the share; the PV stays `Released` with a `VolumeFailedDelete`
event (`FailedPrecondition`) and can be deleted after switching it to `Retain`:

```bash
kubectl -n tcloud-public-csi-manual-sfsturbo delete pod sfsturbo-static
kubectl -n tcloud-public-csi-manual-sfsturbo delete pvc sfsturbo-static-app sfsturbo-static-root
kubectl get pv sfsturbo-manual-static-app sfsturbo-manual-static-root   # STATUS Released
kubectl describe pv sfsturbo-manual-static-app | grep "is not deleted"
kubectl patch pv sfsturbo-manual-static-app -p '{"spec":{"persistentVolumeReclaimPolicy":"Retain"}}'
kubectl delete pv sfsturbo-manual-static-app sfsturbo-manual-static-root
kubectl -n tcloud-public-csi-manual-sfsturbo exec sfsturbo-reader -- cat /data/app/app.txt   # app
```

The health monitor checks bound volumes once a minute (with retries on errors).
For a volume whose share does not exist it must log `NotFound`, for the healthy
`sfsturbo-pvc` nothing:

```bash
kubectl apply -f deploy/manual/sfsturbo/static/missing.yaml
sleep 120
kubectl -n tcloud-public-csi-sfsturbo logs deployment/tcloud-public-csi-controller-sfsturbo \
  -c csi-external-health-monitor-controller | grep -i "error\|notfound"
kubectl get events -A --field-selector reason=VolumeConditionAbnormal
kubectl delete -f deploy/manual/sfsturbo/static/missing.yaml
```

A frozen or failed share produces a `VolumeConditionAbnormal` event on its claim.

## Cleanup

```bash
kubectl delete -k deploy/manual/sfsturbo --ignore-not-found
```

The StorageClass uses `reclaimPolicy: Delete`, so the share is deleted with the
PVC. Check the console that no share remains.

## HPC file system

The [sfsturbo-hpc](../sfsturbo-hpc) overlay runs the same claim and pods with a
`tcloud-public-sfsturbo-manual-hpc` StorageClass (`hpcBw: 40M`) and a 1Ti
request. HPC shares start at 1.2 TiB and are billed accordingly, so run this
once and clean up right away. Run it after the cleanup above, because both
overlays use the same namespace and claim name.

```bash
kubectl apply -k deploy/manual/sfsturbo-hpc
kubectl -n tcloud-public-csi-manual-sfsturbo get pvc sfsturbo-pvc -w
kubectl -n tcloud-public-csi-manual-sfsturbo wait --for=condition=Ready pod/sfsturbo-reader --timeout=15m
kubectl -n tcloud-public-csi-manual-sfsturbo logs sfsturbo-reader
```

The PVC capacity must be `1228Gi`, the size of the smallest 40M share. In the
console the share must have type HPC, bandwidth 40 MB/s/TiB, and 1228 GiB.

Expand by 1 GiB; the driver rounds up to the next 1.2 TiB multiple:

```bash
kubectl -n tcloud-public-csi-manual-sfsturbo patch pvc sfsturbo-pvc \
  -p '{"spec":{"resources":{"requests":{"storage":"1229Gi"}}}}'
kubectl -n tcloud-public-csi-manual-sfsturbo get pvc sfsturbo-pvc -w   # 2457Gi
kubectl -n tcloud-public-csi-manual-sfsturbo exec sfsturbo-reader -- df -h /data
```

Clean up:

```bash
kubectl delete -k deploy/manual/sfsturbo-hpc --ignore-not-found
```

## KMS encryption

The [sfsturbo-kms](../sfsturbo-kms) overlay runs the same claim and pods with a
`tcloud-public-sfsturbo-manual-kms` StorageClass that sets `kmsKeyId`. Use the ID
of an enabled custom KMS key in the same region and project (SFS Turbo has no
default key). Run it after the cleanup of the other overlays.

```bash
KMS_KEY_ID=<key ID>
kubectl kustomize deploy/manual/sfsturbo-kms | sed "s/KMS_KEY_ID/${KMS_KEY_ID}/" | kubectl apply -f -
kubectl -n tcloud-public-csi-manual-sfsturbo wait --for=condition=Ready pod/sfsturbo-reader --timeout=20m
kubectl -n tcloud-public-csi-manual-sfsturbo exec sfsturbo-reader -- cat /data/shared.txt
PV=$(kubectl -n tcloud-public-csi-manual-sfsturbo get pvc sfsturbo-pvc -o jsonpath='{.spec.volumeName}')
kubectl get pv "$PV" -o jsonpath='{.spec.csi.volumeAttributes.kmsKeyId}'   # the key ID
```

In the console the share must be encrypted with the key.

A malformed key ID is rejected before any cloud call, and an unknown key is
rejected by the API (`SFS.TURBO.0001 crypt key id (...) cannot be used`). In
both cases the claim stays `Pending` and no share is created:

```bash
kubectl delete -k deploy/manual/sfsturbo-kms --ignore-not-found
kubectl kustomize deploy/manual/sfsturbo-kms | sed "s/KMS_KEY_ID/not-a-key/" | kubectl apply -f -
kubectl -n tcloud-public-csi-manual-sfsturbo describe pvc sfsturbo-pvc | grep "must be a KMS key ID"
kubectl delete -k deploy/manual/sfsturbo-kms --ignore-not-found
kubectl kustomize deploy/manual/sfsturbo-kms | sed "s/KMS_KEY_ID/00000000-0000-0000-0000-000000000000/" | kubectl apply -f -
kubectl -n tcloud-public-csi-sfsturbo logs deployment/tcloud-public-csi-controller-sfsturbo \
  -c tcloud-public-csi-driver | grep "cannot be used"
```

Clean up:

```bash
kubectl delete -k deploy/manual/sfsturbo-kms --ignore-not-found
```
