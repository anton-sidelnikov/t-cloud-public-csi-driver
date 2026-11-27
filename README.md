# T Cloud Public CSI Driver

Kubernetes [CSI](https://kubernetes-csi.github.io/docs/) driver for T Cloud Public (Open Telekom Cloud) storage.

| Storage                      | Driver name                     | Access                                 | Docs                                 | Examples                               |
|------------------------------|---------------------------------|----------------------------------------|--------------------------------------|----------------------------------------|
| EVS (block disks)            | `csi.evs.tcloudpublic.com`      | ReadWriteOnce, filesystem or raw block | [docs/evs.md](docs/evs.md)           | [examples/evs](examples/evs)           |
| SFS Turbo (NFS file systems) | `csi.sfsturbo.tcloudpublic.com` | ReadWriteMany                          | [docs/sfsturbo.md](docs/sfsturbo.md) | [examples/sfsturbo](examples/sfsturbo) |
| SFS General Purpose          | `csi.gpsfs.tcloudpublic.com`    | ReadWriteMany                          | [docs/gpsfs.md](docs/gpsfs.md)       | [examples/gpsfs](examples/gpsfs)       |

> The driver is in early development. Parameters and manifests may change between versions.

## Features

- Dynamic provisioning and deletion
- Online volume expansion
- EVS: raw block volumes, snapshots and restore, availability zone topology
- KMS encryption for EVS and SFS Turbo
- SFS Turbo: Standard, Performance, Enhanced and HPC file systems
- SFS Turbo: existing file systems and their directories as volumes, volume health monitoring
- SFS General Purpose: shared file systems without size limits, existing file systems and their directories as volumes, volume health monitoring

## Requirements

- Kubernetes nodes are ECS instances in the T Cloud Public project used by the driver.
- An IAM user with permissions for the storage service (EVS, SFS Turbo, SFS General Purpose) and, for encryption, KMS.
- For EVS snapshots: the [snapshot CRDs](https://github.com/kubernetes-csi/external-snapshotter/tree/master/client/config/crd).

## Quick Start

1. Install the snapshot CRDs (skip if they already exist):

   ```bash
   kubectl kustomize "https://github.com/kubernetes-csi/external-snapshotter/client/config/crd?ref=v8.2.0" | kubectl create -f -
   ```

2. Create the cloud credentials. Copy [deploy/kubernetes/secret.example.yaml](deploy/kubernetes/secret.example.yaml) to `secret.yaml`, fill in the values (see [docs/configuration.md](docs/configuration.md)) and apply it:

   ```bash
   kubectl create namespace tcloud-public-csi-system
   kubectl apply -f secret.yaml
   ```

3. Install the EVS driver:

   ```bash
   kubectl apply -k deploy/kubernetes          # any cluster
   kubectl apply -k deploy/kubernetes/cce/evs  # CCE clusters
   ```

4. Create a volume and a pod:

   ```bash
   kubectl apply -f examples/evs/filesystem.yaml
   kubectl get pvc evs-data
   ```

To install SFS Turbo or SFS General Purpose, see [docs/sfsturbo.md](docs/sfsturbo.md#installation) and [docs/gpsfs.md](docs/gpsfs.md#installation).

## Images

Images are published to `ghcr.io/anton-sidelnikov/t-cloud-public-csi-driver`: `latest` (main branch), `v*` (releases), `sha-<commit>`. The manifests use `latest`; pin a version with an `images` entry in your kustomize overlay.

## Documentation

- [EVS](docs/evs.md)
- [SFS Turbo](docs/sfsturbo.md)
- [SFS General Purpose](docs/gpsfs.md)
- [Configuration](docs/configuration.md)
- [Development and testing](docs/development.md)
