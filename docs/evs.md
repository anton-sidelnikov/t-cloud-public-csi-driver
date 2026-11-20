# EVS

EVS disks are attached to one node at a time and used as a filesystem or a raw block device.

Examples: [examples/evs](../examples/evs).

## Installation

See the [Quick Start](../README.md#quick-start). The bundle installs the driver into `tcloud-public-csi-system` together with:

- the `snapshot-controller`;
- the StorageClass `tcloud-public-evs` (SSD);
- the VolumeSnapshotClass `tcloud-public-evs`.

On CCE use `deploy/kubernetes/cce/evs`, because CCE runs kubelet with a different root directory. In your own overlay add the component `deploy/kubernetes/components/cce` instead.

## Capabilities

| Feature               | Support                                                                              |
|-----------------------|--------------------------------------------------------------------------------------|
| Access modes          | `ReadWriteOnce`, `ReadWriteOncePod`                                                  |
| Volume modes          | `Filesystem` (ext4 by default, xfs), `Block` ([example](../examples/evs/block.yaml)) |
| Expansion             | online                                                                               |
| Snapshots and restore | yes                                                                                  |
| Topology              | availability zone (`topology.evs.tcloudpublic.com/zone`)                             |
| Encryption            | KMS (`kmsKeyId`)                                                                     |
| Volumes per node      | 32 by default (`CSI_MAX_VOLUMES_PER_NODE`)                                           |

## StorageClass Parameters

Unknown parameters are rejected.

| Parameter                   | Description                                                                                                                |
|-----------------------------|----------------------------------------------------------------------------------------------------------------------------|
| `volumeType`                | EVS disk type, for example `SSD`, `SAS`, `GPSSD`.                                                                          |
| `availabilityZone`          | Zone of the disk. By default the zone of the node selected by the scheduler. Prefer `allowedTopologies` to restrict zones. |
| `kmsKeyId`                  | ID of a KMS key to encrypt the disk, see [Encryption](#encryption).                                                        |
| `description`               | Disk description.                                                                                                          |
| `metadata.<key>`            | Disk metadata `<key>`.                                                                                                     |
| `csi.storage.k8s.io/fstype` | `ext4` (default) or `xfs`.                                                                                                 |

Use `volumeBindingMode: WaitForFirstConsumer`, so that the disk is created in the zone of the pod's node.

Example: [examples/evs/filesystem.yaml](../examples/evs/filesystem.yaml).

## Snapshots

Create a `VolumeSnapshot` with the class `tcloud-public-evs`, then a PVC with the snapshot as `dataSource`. The restored PVC must be at least the size of the snapshot. Snapshots contain only data already written to the disk; flush it (`sync`) or stop the application first.

Example: [examples/evs/snapshot.yaml](../examples/evs/snapshot.yaml).

## Encryption

Set `kmsKeyId` to the ID of an enabled KMS key in the same region and project. You can use the default key `evs/default` or a custom key.

- EVS needs access to KMS in the project. It is granted once, when the first encrypted disk is created in the console; this requires the Security Administrator or KMS Administrator role.
- Encryption cannot be changed after the disk is created.
- A disk restored from a snapshot is encrypted with the key of the original disk. If that key differs from `kmsKeyId`, provisioning fails.
- If the key is disabled or deleted, the disk cannot be attached again.

Example: [examples/evs/encrypted.yaml](../examples/evs/encrypted.yaml).

## Troubleshooting

```bash
kubectl describe pvc <name>
kubectl -n tcloud-public-csi-system logs deployment/tcloud-public-csi-controller -c tcloud-public-csi-driver
kubectl -n tcloud-public-csi-system logs daemonset/tcloud-public-csi-node -c tcloud-public-csi-driver
```

The node's `status.nodeInfo.systemUUID` must be the ECS instance ID:

```bash
kubectl get node <name> -o jsonpath='{.status.nodeInfo.systemUUID}'
```
