# SFS Turbo

Each volume is a separate SFS Turbo file system, mounted over NFS. Volumes can be shared by pods on many nodes.

Examples: [examples/sfsturbo](../examples/sfsturbo).

## Installation

The driver runs in its own namespace `tcloud-public-csi-sfsturbo` and can be installed next to EVS.

1. Create the cloud secret in that namespace, see [configuration.md](configuration.md). Set `OS_VPC_ID`, `OS_SUBNET_ID` and `OS_SECURITY_GROUP_ID`, or set them in every StorageClass.

   ```bash
   kubectl create namespace tcloud-public-csi-sfsturbo
   kubectl -n tcloud-public-csi-sfsturbo apply -f secret.yaml
   ```

2. Install the driver:

   ```bash
   kubectl apply -k deploy/kubernetes/sfsturbo      # any cluster
   kubectl apply -k deploy/kubernetes/cce/sfsturbo  # CCE clusters
   ```

The bundle also adds the StorageClass `tcloud-public-sfsturbo` (Standard).

## Requirements

- Nodes are in the VPC of the file system.
- The security group allows NFS from the nodes (ports 111, 2049, 2051, 2052, 20048). SFS Turbo opens them when it creates a file system.
- The IAM user has SFS Turbo permissions, for example `SFS Turbo FullAccess`.

## Capabilities

| Feature        | Support                        |
|----------------|--------------------------------|
| Access modes   | all, including `ReadWriteMany` |
| Volume modes   | `Filesystem`                   |
| Expansion      | online                         |
| Snapshots      | no                             |
| Encryption     | KMS (`kmsKeyId`)               |
| Volume metrics | yes (`kubelet_volume_stats_*`) |

Creating a file system takes several minutes.

## File System Types

| Type                                     | Parameters                                             | Size, GiB                                                           | Expansion step       |
|------------------------------------------|--------------------------------------------------------|---------------------------------------------------------------------|----------------------|
| Standard                                 | `shareType: STANDARD`                                  | 500–32768                                                           | at least 100 GiB     |
| Performance                              | `shareType: PERFORMANCE`                               | 500–32768                                                           | at least 100 GiB     |
| Standard-Enhanced / Performance-Enhanced | `shareType`, `expandType: bandwidth`                   | 10240–327680                                                        | at least 100 GiB     |
| HPC                                      | `hpcBw: 20M`, `40M`, `125M`, `250M`, `500M` or `1000M` | multiples of 1228 (1.2 TiB), up to 1048576; at least 3686 for `20M` | to the next multiple |

A smaller request is raised to the minimum size and rounded up to the step. A larger request than the maximum fails.

## StorageClass Parameters

Unknown parameters are rejected.

| Parameter                   | Description                                                                |
|-----------------------------|----------------------------------------------------------------------------|
| `shareType`                 | `STANDARD` (default) or `PERFORMANCE`.                                     |
| `expandType`                | `bandwidth` (Enhanced) or `hpc`. `hpcBw` alone implies `hpc`.              |
| `hpcBw`                     | HPC bandwidth per TiB.                                                     |
| `availabilityZone`          | Default: `OS_AVAILABILITY_ZONE`.                                           |
| `vpcId`                     | Default: `OS_VPC_ID`.                                                      |
| `subnetId`                  | Network ID of the subnet. Default: `OS_SUBNET_ID`.                         |
| `securityGroupId`           | Default: `OS_SECURITY_GROUP_ID`.                                           |
| `kmsKeyId`                  | ID of a KMS key to encrypt the file system, see [Encryption](#encryption). |
| `description`               | File system description.                                                   |
| `csi.storage.k8s.io/fstype` | Only `nfs`.                                                                |

The zone, VPC, subnet and security group are required, in the StorageClass or in the secret.

Examples: [standard.yaml](../examples/sfsturbo/standard.yaml), [enhanced.yaml](../examples/sfsturbo/enhanced.yaml), [hpc.yaml](../examples/sfsturbo/hpc.yaml).

## Encryption

Set `kmsKeyId` to the ID of an enabled custom KMS key in the same region and project. SFS Turbo has no default key.

- The IAM user needs the KMS Administrator role.
- Encryption cannot be changed after the file system is created.
- If the key is disabled or deleted, the data becomes inaccessible.

Example: [examples/sfsturbo/encrypted.yaml](../examples/sfsturbo/encrypted.yaml).

## Troubleshooting

```bash
kubectl describe pvc <name>
kubectl -n tcloud-public-csi-sfsturbo logs deployment/tcloud-public-csi-controller-sfsturbo -c tcloud-public-csi-driver
kubectl -n tcloud-public-csi-sfsturbo logs daemonset/tcloud-public-csi-node-sfsturbo -c tcloud-public-csi-driver
```
