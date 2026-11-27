# SFS General Purpose

Each volume is a separate SFS General Purpose file system, mounted over NFS. Volumes can be shared by pods on many nodes. Existing file systems and their directories can be used as volumes too.

Examples: [examples/gpsfs](../examples/gpsfs).

## Installation

The driver runs in its own namespace `tcloud-public-csi-gpsfs` and can be installed next to EVS and SFS Turbo.

1. Create a [VPC endpoint](#vpc-endpoint) in the VPC of the nodes.

2. Create the cloud secret in the driver namespace, see [configuration.md](configuration.md). Set `OS_VPC_ID` to the VPC of the nodes, or set `vpcId` in every StorageClass. Access keys can replace the password, see [authentication](configuration.md#sfs-general-purpose-authentication).

   ```bash
   kubectl create namespace tcloud-public-csi-gpsfs
   kubectl -n tcloud-public-csi-gpsfs apply -f secret.yaml
   ```

3. Install the driver:

   ```bash
   kubectl apply -k deploy/kubernetes/gpsfs      # any cluster
   kubectl apply -k deploy/kubernetes/cce/gpsfs  # CCE clusters
   ```

The bundle also adds the StorageClass `tcloud-public-gpsfs`.

## Requirements

- The region offers SFS General Purpose (`eu-de`).
- The VPC of the nodes has a VPC endpoint for SFS General Purpose.
- The IAM user has the `Tenant Administrator` role in the project; OTC has no narrower system role for SFS General Purpose.

## VPC Endpoint

Nodes reach file systems only through a VPC endpoint in their VPC. Create it once per VPC: **VPC Endpoint** > **VPC Endpoints** > **Create VPC Endpoint**, **Find a service by name** `com.t-systems.otc.eu-de.lz02.obsv2.ipv4`, the VPC and subnet of the nodes. The endpoint is free of charge and is accepted at once.

## Capabilities

| Feature               | Support                                                               |
|-----------------------|-----------------------------------------------------------------------|
| Access modes          | all, including `ReadWriteMany`                                        |
| Volume modes          | `Filesystem`                                                          |
| Size                  | not enforced: the requested size is only shown, usage is not limited  |
| Expansion             | yes, changes only the shown size                                      |
| Snapshots             | no                                                                    |
| Encryption            | no                                                                    |
| Volume metrics        | yes (`kubelet_volume_stats_*`)                                        |
| Existing file systems | whole file system or a directory, see [below](#existing-file-systems) |
| Volume health         | with the health monitor, see [below](#volume-health)                  |

A new volume can be mounted after about 30 seconds, when the access rules take effect; pods start once the mount succeeds.

## StorageClass Parameters

Unknown parameters are rejected.

| Parameter                   | Description                                                   |
|-----------------------------|---------------------------------------------------------------|
| `vpcId`                     | VPC that may access the file system. Default: `OS_VPC_ID`.    |
| `csi.storage.k8s.io/fstype` | Only `nfs`.                                                   |

Example: [shared.yaml](../examples/gpsfs/shared.yaml).

## Deleting Volumes

SFS General Purpose deletes only empty file systems. Delete the data before deleting the claim; otherwise the driver refuses deletion with a `VolumeFailedDelete` event and retries until the file system is empty.

Other limits of the service:

- File system names are unique across all accounts. The name of a volume is the name of its PersistentVolume, for example `pvc-<uid>`.
- A deleted name can be used again only after 12 hours.
- A project can have up to 100 file systems.

## Existing File Systems

To use a file system that was created outside Kubernetes, create a PersistentVolume with the file system name as `volumeHandle`. The file system must allow access from the VPC of the nodes.

```yaml
  persistentVolumeReclaimPolicy: Retain
  csi:
    driver: csi.gpsfs.tcloudpublic.com
    volumeHandle: <file system name>            # whole file system
    # volumeHandle: <file system name>:team-a   # only the directory team-a
```

- To mount a directory, append `:` and the path inside the file system. The directory must already exist.
- Use `persistentVolumeReclaimPolicy: Retain`. With `Delete`, deleting the claim deletes the file system if it is empty. For a directory volume the driver refuses deletion; set the PV to `Retain` and delete it.

Example: [static.yaml](../examples/gpsfs/static.yaml).

## Volume Health

The optional [health monitor](https://kubernetes-csi.github.io/docs/volume-health-monitor.html) checks every SFS General Purpose volume once a minute. If a file system was deleted outside Kubernetes, it adds a `VolumeConditionAbnormal` warning event to the claim.

Install the driver with the `health-monitor` component instead of step 3 of [Installation](#installation). From the repository root:

```bash
mkdir -p overlays/gpsfs
cat > overlays/gpsfs/kustomization.yaml <<EOF
resources:
  - ../../deploy/kubernetes/gpsfs   # CCE: ../../deploy/kubernetes/cce/gpsfs
components:
  - ../../deploy/kubernetes/components/health-monitor
EOF
kubectl apply -k overlays/gpsfs
```

Abnormal volumes:

```bash
kubectl get events -A --field-selector reason=VolumeConditionAbnormal
```

## Troubleshooting

```bash
kubectl describe pvc <name>
kubectl -n tcloud-public-csi-gpsfs logs deployment/tcloud-public-csi-controller-gpsfs -c tcloud-public-csi-driver
kubectl -n tcloud-public-csi-gpsfs logs daemonset/tcloud-public-csi-node-gpsfs -c tcloud-public-csi-driver
```

A mount that times out usually means a missing VPC endpoint or a VPC that is not allowed in the file system access rules.
