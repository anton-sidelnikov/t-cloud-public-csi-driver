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

## Expansion

SFS Turbo grows in steps of at least 100 GiB. Expansion is online and needs no
pod restart.

```bash
kubectl -n tcloud-public-csi-manual-sfsturbo patch pvc sfsturbo-pvc \
  -p '{"spec":{"resources":{"requests":{"storage":"600Gi"}}}}'
kubectl -n tcloud-public-csi-manual-sfsturbo get pvc sfsturbo-pvc -w
kubectl -n tcloud-public-csi-manual-sfsturbo exec sfsturbo-reader -- df -h /data
```

## Cleanup

```bash
kubectl delete -k deploy/manual/sfsturbo
```

The StorageClass uses `reclaimPolicy: Delete`, so the share is deleted with the
PVC. Check the console that no share remains.
