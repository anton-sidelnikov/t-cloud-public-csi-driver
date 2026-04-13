# EVS Manual Test Manifests

This folder contains manual validation manifests for the EVS backend.

Apply all test assets:

```bash
kubectl apply -k deploy/manual/evs
```

Prerequisites:

- the CSI controller and node manifests from `deploy/kubernetes` are already running
- the driver image in those manifests points to a real published image
- cloud credentials are valid and the target project has EVS quota available

## Covered Scenarios

- filesystem volume provisioning and mount
- raw block volume provisioning and attach
- online filesystem expansion
- reclaim policy validation with `Delete` and `Retain` storage classes

## Filesystem Test

Objects:

- `PersistentVolumeClaim/evs-fs-pvc`
- `Pod/evs-fs-pod`

Checks:

```bash
kubectl -n tcloud-public-csi-manual get pvc,pv,pod
kubectl -n tcloud-public-csi-manual exec evs-fs-pod -- sh -c 'df -h /data && ls -R /data && cat /data/app/health.txt'
```

## Raw Block Test

Objects:

- `PersistentVolumeClaim/evs-block-pvc`
- `Pod/evs-block-pod`

Checks:

```bash
kubectl -n tcloud-public-csi-manual exec evs-block-pod -- sh -c 'ls -l /dev/xvdc'
kubectl -n tcloud-public-csi-manual exec evs-block-pod -- sh -c 'dd if=/dev/zero of=/dev/xvdc bs=1M count=4 conv=fsync'
kubectl -n tcloud-public-csi-manual exec evs-block-pod -- sh -c 'dd if=/dev/xvdc bs=512 count=8 | hexdump -C'
```

## Expansion Test

Objects:

- `PersistentVolumeClaim/evs-expand-pvc`
- `Pod/evs-expand-pod`

Initial check:

```bash
kubectl -n tcloud-public-csi-manual exec evs-expand-pod -- df -h /data
```

Expand the claim:

```bash
kubectl -n tcloud-public-csi-manual patch pvc evs-expand-pvc --type merge -p '{"spec":{"resources":{"requests":{"storage":"12Gi"}}}}'
```

Verify:

```bash
kubectl -n tcloud-public-csi-manual get pvc evs-expand-pvc
kubectl -n tcloud-public-csi-manual exec evs-expand-pod -- df -h /data
```

## Reclaim Policy Checks

Delete reclaim:

- `tcloud-public-evs-manual` should remove the cloud volume when the PVC and PV are deleted.

Retain reclaim:

- `PersistentVolumeClaim/evs-retain-pvc`
- `Pod/evs-retain-pod`
- verify the retained pod can write data:

```bash
kubectl -n tcloud-public-csi-manual exec evs-retain-pod -- sh -c 'df -h /data && cat /data/retain/marker.txt'
```

- delete the pod and PVC:

```bash
kubectl -n tcloud-public-csi-manual delete pod evs-retain-pod
kubectl -n tcloud-public-csi-manual delete pvc evs-retain-pvc
```

- verify the PV moved to `Released` and the EVS disk was kept:

```bash
kubectl get pv | grep evs-retain-pvc
```

## Cleanup

```bash
kubectl delete -k deploy/manual/evs
```

Retained volumes created with `tcloud-public-evs-retain` are intentionally left behind in the cloud and must be cleaned up manually after validation.
