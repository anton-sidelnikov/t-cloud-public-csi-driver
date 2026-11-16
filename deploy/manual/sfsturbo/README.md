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

## Cleanup

```bash
kubectl delete -k deploy/manual/sfsturbo
```

The StorageClass uses `reclaimPolicy: Delete`, so the share is deleted with the
PVC. Check the console that no share remains.
