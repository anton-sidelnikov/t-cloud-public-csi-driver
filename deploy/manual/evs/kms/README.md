# EVS KMS Encryption Manual Workflow

This scenario checks encrypted EVS volumes created with the StorageClass
parameter `kmsKeyId`, their snapshots and restores. It reuses the manifests of
the [snapshot](../snapshot/README.md) scenario with the
`tcloud-public-evs-manual-kms` StorageClass.

Prerequisites:

- the prerequisites of the snapshot scenario
- an enabled KMS key in the same region and project; `evs/default` or a custom key
- KMS access granted to EVS in the project (the one-time agency created by the
  first encrypted disk in the console; it exists if `evs/default` exists)

## Encrypted volume and snapshot

```bash
KMS_KEY_ID=<key ID>
N=tcloud-public-csi-manual
kubectl apply -f deploy/manual/evs/common/namespace.yaml
kubectl apply -f deploy/manual/evs/common/storageclass-delete.yaml
sed "s/KMS_KEY_ID/${KMS_KEY_ID}/" deploy/manual/evs/kms/storageclass.yaml | kubectl apply -f -
sed "s/tcloud-public-evs-manual$/tcloud-public-evs-manual-kms/" deploy/manual/evs/snapshot/source.yaml | kubectl -n $N apply -f -
kubectl -n $N wait --for=condition=Ready pod/evs-snap-src-pod --timeout=10m
kubectl -n $N exec evs-snap-src-pod -- cat /data/app/snapshot.txt
PV=$(kubectl -n $N get pvc evs-snap-src-pvc -o jsonpath='{.spec.volumeName}')
kubectl get pv "$PV" -o jsonpath='{.spec.csi.volumeAttributes.kmsKeyId}'   # the key ID
```

In the console the disk must be encrypted with the key (metadata
`__system__encrypted=1` and `__system__cmkid=<key ID>`).

```bash
kubectl -n $N apply -f deploy/manual/evs/snapshot/volume.yaml
kubectl -n $N wait --for=jsonpath='{.status.readyToUse}'=true volumesnapshot/evs-snap-src --timeout=10m
```

## Restore

A volume restored from a snapshot inherits the encryption of the snapshot's
source volume. Restore into the KMS StorageClass; the driver checks that the
inherited key matches `kmsKeyId`:

```bash
sed "s/tcloud-public-evs-manual$/tcloud-public-evs-manual-kms/" deploy/manual/evs/snapshot/restore.yaml | kubectl -n $N apply -f -
kubectl -n $N wait --for=condition=Ready pod/evs-snap-restore-pod --timeout=10m
kubectl -n $N logs evs-snap-restore-pod   # the line written to the source
PV=$(kubectl -n $N get pvc evs-snap-restore-pvc -o jsonpath='{.spec.volumeName}')
kubectl get pv "$PV" -o jsonpath='{.spec.csi.volumeAttributes.kmsKeyId}'   # the key ID
kubectl -n $N delete -f deploy/manual/evs/snapshot/restore.yaml
```

## Rejected requests

A malformed key ID is rejected before any cloud call:

```bash
sed "s/KMS_KEY_ID/not-a-key/; s/manual-kms/manual-kms-bad/" deploy/manual/evs/kms/storageclass.yaml | kubectl apply -f -
sed "s/tcloud-public-evs-manual$/tcloud-public-evs-manual-kms-bad/" deploy/manual/evs/snapshot/restore.yaml | kubectl -n $N apply -f -
kubectl -n $N describe pvc evs-snap-restore-pvc | grep "must be a KMS key ID"
kubectl -n $N delete -f deploy/manual/evs/snapshot/restore.yaml
kubectl delete storageclass tcloud-public-evs-manual-kms-bad
```

A restore that would not be encrypted with `kmsKeyId` is deleted and fails.
Snapshot an unencrypted volume and restore it into the KMS StorageClass:

```bash
sed "s/evs-snap-src/evs-snap-plain/g" deploy/manual/evs/snapshot/source.yaml | kubectl -n $N apply -f -
kubectl -n $N wait --for=condition=Ready pod/evs-snap-plain-pod --timeout=10m
sed "s/evs-snap-src/evs-snap-plain/g" deploy/manual/evs/snapshot/volume.yaml | kubectl -n $N apply -f -
kubectl -n $N wait --for=jsonpath='{.status.readyToUse}'=true volumesnapshot/evs-snap-plain --timeout=10m
sed "s/evs-snap-src/evs-snap-plain/; s/tcloud-public-evs-manual$/tcloud-public-evs-manual-kms/" deploy/manual/evs/snapshot/restore.yaml | kubectl -n $N apply -f -
kubectl -n $N describe pvc evs-snap-restore-pvc | grep "instead of"
kubectl -n tcloud-public-csi-system logs deployment/tcloud-public-csi-controller -c tcloud-public-csi-driver \
  | grep "deleting EVS volume without the requested encryption"
```

The console must not keep the deleted restore volumes.

## Cleanup

```bash
kubectl -n $N delete -f deploy/manual/evs/snapshot/restore.yaml --ignore-not-found
sed "s/evs-snap-src/evs-snap-plain/g" deploy/manual/evs/snapshot/volume.yaml | kubectl -n $N delete -f - --ignore-not-found
sed "s/evs-snap-src/evs-snap-plain/g" deploy/manual/evs/snapshot/source.yaml | kubectl -n $N delete -f - --ignore-not-found
kubectl -n $N delete -f deploy/manual/evs/snapshot/volume.yaml --ignore-not-found
kubectl -n $N delete -f deploy/manual/evs/snapshot/source.yaml --ignore-not-found
kubectl delete storageclass tcloud-public-evs-manual-kms --ignore-not-found
```
