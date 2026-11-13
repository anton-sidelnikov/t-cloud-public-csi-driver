# T Cloud Public CSI Driver

This repository hosts a Container Storage Interface driver for T Cloud Public block (EVS) and file (SFS Turbo) storage.

The initial slice in this repository focuses on:

- CSI identity service wiring
- CSI controller methods for EVS volume create, delete, attach, detach, and expand
- CSI node methods for stage, publish, unpublish, unstage, and filesystem expansion
- a multi-driver backend abstraction so additional T Cloud Public storage drivers can be added later
- T Cloud Public authentication and service client initialization through `github.com/opentelekomcloud/gophertelekomcloud`

## Current Status

This is an initial scaffold, not a production-ready driver yet.

The repository is now structured as a multi-driver CSI codebase.
The `evs` and `sfsturbo` backends are implemented, and the common controller/node plumbing no longer assumes EVS must be the only backend forever.

Implemented:

- `CreateVolume`
- `DeleteVolume`
- `CreateSnapshot`
- `DeleteSnapshot`
- `ListSnapshots`
- `ControllerPublishVolume`
- `ControllerUnpublishVolume`
- `ControllerExpandVolume`
- `ValidateVolumeCapabilities`
- `GetPluginInfo`, `GetPluginCapabilities`, `Probe`
- `NodeStageVolume`
- `NodeUnstageVolume`
- `NodePublishVolume`
- `NodeUnpublishVolume`
- `NodeExpandVolume`
- `NodeGetInfo`, `NodeGetCapabilities`

Not implemented yet:

- Full EVS lifecycle functional tests

## Test Status

Current unit coverage includes:

- environment-based config parsing and defaulting
- controller request validation and service interaction shaping
- node staging, publishing, unpublishing, expansion, and topology exposure
- EVS helper behavior such as GiB rounding and response mapping

Current functional coverage includes:

- bootstrap of the CSI controller/node stack into an ephemeral CCE cluster
- cloud secret creation from `OS_*` variables
- rollout readiness verification for the controller `Deployment` and node `DaemonSet`
- `CSIDriver` registration check
- filesystem PVC lifecycle: provision, attach, mount, write/read validation, and cleanup
- snapshot smoke lifecycle on CCE: source PVC, `VolumeSnapshot` readiness, and restore PVC binding

Current snapshot coverage includes:

- unit tests for create/delete/list snapshot controller flows
- unit tests for restore-from-snapshot request mapping
- functional CCE smoke coverage for snapshot and restore control-plane flow

Run:

```bash
go test ./...
```

## Development

Common local tasks are exposed through the [Makefile](Makefile).

Examples:

```bash
make build
make test
make lint
make check
make manifests
make image
```

Build metadata can be overridden for local builds and images:

```bash
make build VERSION=v0.1.0 COMMIT=$(git rev-parse --short=12 HEAD)
make image IMAGE=ghcr.io/example/tcloud-public-csi-driver:v0.1.0 VERSION=v0.1.0
```

The binary logs `version`, `commit`, and `build_date` on startup. Container images also include matching OCI labels.

## CI

GitHub Actions workflow definitions live in [.github/workflows](.github/workflows).

The current CI pipeline runs:

- `go vet`
- `go test ./...`
- `golangci-lint`
- Docker image build on pull requests
- Docker image build and push to GHCR on pushes to `main` and version tags

A separate workflow is available in [.github/workflows/functional-pr-dispatch.yaml](.github/workflows/functional-pr-dispatch.yaml). It runs EVS functional tests automatically on every same-repository PR commit and can also be started manually with `workflow_dispatch`.

Use the functional workflow when you want automatic branch-local functional runs on PR updates, or start it manually from the Actions tab when needed:

- open the Actions tab
- select `functional-pr-dispatch`
- choose the branch that contains the workflow change when using `workflow_dispatch`

Security boundary:

- the PR functional workflow runs only for same-repository PR branches
- fork PRs do not receive cloud-backed functional runs
- functional runs upload the raw `make test-functional` output so failures can be diagnosed from artifacts

## Functional Tests

Functional tests are designed to run against an ephemeral T Cloud Public CCE cluster provisioned with Terraform and the OpenTelekomCloud provider.

The infrastructure scaffold lives in [test/functional/terraform](test/functional/terraform). It creates a VPC, subnet, CCE cluster, worker nodes, and a generated kubeconfig output. Authentication is read from the same `OS_*` environment variables used by the CSI driver:

```bash
export OS_AUTH_URL=https://iam.example.com/v3
export OS_REGION=eu-de
export OS_AVAILABILITY_ZONE=eu-de-01
export OS_DOMAIN_NAME=Default
export OS_USERNAME=replace-me
export OS_PASSWORD=replace-me
export OS_PROJECT_ID=replace-me
```

Provision infrastructure. The Makefile maps `OS_*` into Terraform `TF_VAR_*` values automatically:

```bash
make functional-infra-init
make functional-infra-up
make functional-kubeconfig
```

Build and push a functional-test image from the current checkout. The image must be reachable by CCE worker nodes:

```bash
make functional-image
make functional-image-push
```

Run the functional test scaffold. If `CSI_TEST_IMAGE` is omitted, `make test-functional` uses `FUNCTIONAL_IMAGE`:

```bash
make test-functional
```

Destroy infrastructure:

```bash
make functional-infra-down
```

Terraform variables can still be overridden explicitly with `TF_VAR_*` when needed, for example `TF_VAR_node_count=3 make functional-infra-up`.

For private registries, the functional test implementation must create an image pull secret before deploying the driver. Public GHCR images do not require that extra setup.

Shared helpers (kubectl wrapper, config, driver install) live in `test/functional/framework`. `make test-functional` runs the packages in `FUNCTIONAL_PACKAGES` (default `./test/functional/evs ./test/functional/sfsturbo`).

The `evs` package includes:

- a bootstrap test that installs the CSI manifests into the generated cluster, creates the cloud secret from `OS_*`, patches the driver image to `CSI_TEST_IMAGE`, and verifies the driver rolls out successfully
- a filesystem lifecycle test that provisions an EVS-backed PVC, mounts it in a pod, writes and reads test data, and then performs best-effort namespace cleanup

- a snapshot test and an availability zone autodetection test

The `sfsturbo` package deploys `deploy/kubernetes/sfsturbo` into its own namespace and includes:

- a bootstrap test that also checks `attachRequired: false`
- a lifecycle test: a 500 GiB `ReadWriteMany` PVC shared by two pods that read each other's writes, no `VolumeAttachment`, online expansion to 600 GiB visible in the running pod, and deletion of the PV and share after the PVC is deleted

The SFS Turbo tests need `CSI_TEST_VPC_ID`, `CSI_TEST_SUBNET_ID` (subnet network ID), and `CSI_TEST_SECURITY_GROUP_ID`; `make test-functional` reads them from the Terraform outputs `vpc_id`, `network_id`, and `sfs_turbo_security_group_id` (the project `default` security group), and the tests are skipped when they are missing. SFS Turbo is billed and each step takes minutes, so the package adds roughly 20–30 minutes to the run.

The next functional-test steps are raw block, expansion, and reclaim-policy scenarios for EVS.

The Terraform functional scaffold now defaults kubeconfig generation to the direct public CCE endpoint (`external`) instead of `external_otc`, because that behaves more reliably for normal `kubectl` operations from outside the cluster VPC.

If the generated kubeconfig points to a CCE endpoint whose certificate chain is not trusted by the machine running the tests, the functional runner automatically retries `kubectl` with `--insecure-skip-tls-verify=true`. The bootstrap test also disables OpenAPI schema validation for `kubectl apply -k`, which avoids false failures on clusters whose public endpoint does not expose schema discovery cleanly. You can still force insecure TLS mode up front with:

```bash
CSI_TEST_INSECURE_SKIP_TLS_VERIFY=true make test-functional
```

## Kubernetes Manifests

Baseline manifests live in [deploy/kubernetes](deploy/kubernetes) and are organized as Kustomize components:

- `namespace/`: the `tcloud-public-csi-system` namespace used by EVS
- `base/`: RBAC, controller `Deployment`, and node `DaemonSet` (EVS values)
- `snapshot-controller/`: cluster-level `snapshot-controller` and its RBAC
- `evs/`: EVS overlay that combines the components above with the EVS `CSIDriver`, the example `StorageClass`, and `VolumeSnapshotClass`
- `sfsturbo/`: SFS Turbo overlay, see [SFS Turbo](#sfs-turbo)

The root `kustomization.yaml` deploys the `evs` overlay.

Included:

- namespace and example cloud credential secret
- controller `Deployment` with `csi-provisioner`, `csi-attacher`, `csi-resizer`, and `csi-snapshotter`
- cluster-level `snapshot-controller` `Deployment`
- node `DaemonSet` with `node-driver-registrar`
- RBAC for controller, snapshot, and node components
- `CSIDriver` object
- example EVS `StorageClass`
- example EVS `VolumeSnapshotClass`

Apply the bundle with:

```bash
kubectl apply -k deploy/kubernetes
```

Before applying, replace the placeholder image `ghcr.io/example/tcloud-public-csi-driver:dev` and create a real secret from `deploy/kubernetes/secret.example.yaml`.

Current manifest assumptions:

- controller and node components both consume cloud credentials from the same Kubernetes `Secret`
- the node plugin runs privileged and mounts `/dev`, `/sys`, and the full host `/var/lib/kubelet` with bidirectional mount propagation
- the driver socket is always served at `/csi/csi.sock` inside the pods; only the host plugin directory and the registrar `--kubelet-registration-path` depend on the driver name
- the image ships the NFS client (`nfs-common`) for file-share backends
- snapshot support still depends on the cluster having the upstream snapshot CRDs installed (`VolumeSnapshot`, `VolumeSnapshotContent`, `VolumeSnapshotClass`), but the bundle now includes the `snapshot-controller` deployment itself

## SFS Turbo

The `sfsturbo` backend (`csi.sfsturbo.tcloudpublic.com`) provisions one SFS Turbo file system per volume and mounts it over NFSv3 (`vers=3,timeo=600,noresvport,nolock,tcp`). It runs next to EVS from the same image:

```bash
kubectl apply -k deploy/kubernetes/sfsturbo
```

The overlay deploys into its own namespace `tcloud-public-csi-sfsturbo`, so it can be installed and removed independently of EVS. Create the cloud secret `tcloud-public-csi-cloud-config` in that namespace as well (the same keys as for EVS plus the SFS Turbo network defaults below). The overlay reuses `base/` with the `-sfsturbo` name suffix (cluster-scoped RBAC objects must not clash with EVS) and its own `app` labels, removes `csi-attacher` and `csi-snapshotter`, registers `CSIDriver` with `attachRequired: false`, raises the provisioner and resizer `--timeout` to 5m, and adds an example `tcloud-public-sfsturbo` StorageClass.

Behavior and limits:

- Volumes support only filesystem access (`fsType` empty or `nfs`) and all access modes, including `ReadWriteMany`. Raw block, snapshots, and cloning are not supported.
- Sizes are rounded up to whole GiB. `STANDARD` and `PERFORMANCE` shares accept 500 to 32768 GiB; smaller requests are raised to 500 GiB.
- Expansion is online. Each expansion adds at least 100 GiB, so a smaller increase is rounded up to current size + 100 GiB.
- Creation takes several minutes. `CreateVolume` is idempotent by share name, so provisioner retries pick up the share that is still being created. A share that ends in a failed state is deleted.
- Deletion waits until the share is gone.
- Volumes have no topology. The share is created in `availabilityZone` and must be reachable from all nodes that mount it.

Network prerequisites: nodes must be in the share VPC. SFS Turbo opens the NFS ports (111, 2049, 2051, 2052, 20048) in the configured security group; keep them open if you manage the group yourself. The IAM user needs SFS Turbo permissions (for example the `SFS Turbo FullAccess` role).

StorageClass parameters (unknown keys are rejected):

- `shareType`: `STANDARD` (default) or `PERFORMANCE`.
- `availabilityZone`: defaults to `OS_AVAILABILITY_ZONE`.
- `vpcId`, `subnetId`, `securityGroupId`: default to `OS_VPC_ID`, `OS_SUBNET_ID`, `OS_SECURITY_GROUP_ID`. `subnetId` is the network ID of the VPC subnet.
- `kmsKeyId`: optional KMS key for encryption.
- `description`: optional share description.
- `csi.storage.k8s.io/fstype`: only `nfs`.

The zone, VPC, subnet, and security group are required; `CreateVolume` fails if neither the StorageClass nor the secret provides them.

Manual validation manifests live in [deploy/manual/sfsturbo](deploy/manual/sfsturbo).

## EVS Operational Assumptions

The EVS backend currently assumes:

- Kubernetes node identity must expose the ECS instance UUID in `Node.status.nodeInfo.systemUUID`. The node plugin uses `systemUUID` as the canonical instance identifier and does not trust `spec.providerID` for EVS attach/detach.
- `CSI_NODE_ID` is set from `spec.nodeName` in the node DaemonSet and is used only as the Kubernetes `Node` lookup key unless it is already a UUID.
- EVS volumes are single-node block devices. The driver accepts CSI single-node access modes and rejects multi-node writer modes.
- EVS volumes must be provisioned in an availability zone compatible with the node selected by Kubernetes. The example StorageClasses use `WaitForFirstConsumer`.
- Raw block volumes require the node plugin to see host kubelet block-device publish paths under `/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices`.
- Online filesystem expansion requires controller expansion followed by node filesystem expansion. The controller treats EVS states `available` and `in-use` as valid completed expansion states when the size is updated.

Useful checks:

```bash
kubectl get node <node-name> -o jsonpath='{.status.nodeInfo.systemUUID}{"\n"}'
kubectl -n tcloud-public-csi-system logs -l app=tcloud-public-csi-node -c tcloud-public-csi-driver --tail=200
kubectl -n tcloud-public-csi-system logs -l app=tcloud-public-csi-controller -c tcloud-public-csi-driver --tail=200
```

Manual EVS validation manifests live in [deploy/manual/evs](deploy/manual/evs).

They cover:

- filesystem PVC + pod validation
- raw block PVC + pod validation
- online expansion checks
- reclaim policy checks for `Delete` and `Retain`

## Snapshot Support

The EVS backend now supports the core CSI snapshot flow:

- create snapshot from a source volume
- delete snapshot
- list snapshots
- create a new volume from a snapshot source

Driver behavior:

- `CreateSnapshot` is idempotent by snapshot name and source volume
- restored volumes use the snapshot size when the request does not specify capacity
- restored volumes reject a requested size smaller than the snapshot size

Deployment notes:

- the controller manifest includes `csi-snapshotter`
- the deployment bundle includes a cluster-level `snapshot-controller`
- [deploy/kubernetes/evs/volumesnapshotclass.yaml](deploy/kubernetes/evs/volumesnapshotclass.yaml) provides a baseline `VolumeSnapshotClass`
- your cluster must already have the Kubernetes snapshot CRDs installed

## EVS StorageClass Parameters

The EVS backend validates `StorageClass.parameters` explicitly. Unsupported keys are rejected during `CreateVolume` so typos fail early instead of being silently ignored.

Supported parameters:

- `volumeType`: EVS volume type, for example `SSD`.
- `availabilityZone`: EVS availability zone override. If omitted, the driver uses the zone requested by the scheduler (topology `topology.evs.tcloudpublic.com/zone`; with `WaitForFirstConsumer` this is the selected node's zone), and only then `OS_AVAILABILITY_ZONE`. An override outside the requested topology is rejected. To restrict zones, prefer `allowedTopologies` over this parameter.
- `description`: optional EVS volume description.
- `csi.storage.k8s.io/fstype`: accepted for Kubernetes CSI compatibility. Filesystem formatting is still driven by the CSI volume capability on the node side.
- `metadata.<key>`: optional EVS metadata. The `metadata.` prefix is stripped before sending metadata to EVS.

Example:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: tcloud-public-evs-ssd
provisioner: csi.evs.tcloudpublic.com
allowVolumeExpansion: true
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
parameters:
  volumeType: SSD
  description: Kubernetes EVS volume
  metadata.environment: dev
```

## Configuration

The binary reads its configuration from environment variables:

- `CSI_BACKEND`
- `CSI_DRIVER_NAME`
- `CSI_ENDPOINT`
- `CSI_NODE_ID`
- `CSI_MAX_VOLUMES_PER_NODE`
- `CSI_REQUEST_TIMEOUT`
- `OS_REGION`
- `OS_AVAILABILITY_ZONE`
- `OS_AUTH_URL`
- `OS_DOMAIN_NAME`
- `OS_USERNAME`
- `OS_PASSWORD`
- `OS_PROJECT_ID`
- `OS_PROJECT_NAME`
- `OS_VPC_ID`, `OS_SUBNET_ID`, `OS_SECURITY_GROUP_ID` (SFS Turbo defaults, optional)

Use either `OS_PROJECT_ID` or `OS_PROJECT_NAME`.

`OS_AVAILABILITY_ZONE` is optional. Each node reads its availability zone from the ECS metadata service (`http://169.254.169.254/openstack/latest/meta_data.json`) and reports it as the `topology.evs.tcloudpublic.com/zone` topology. `OS_AVAILABILITY_ZONE` is used only when the metadata service is unreachable, and as the controller default when a request has no topology. Node registration fails if neither source provides a zone. If a node's reported zone changes, remove the stale `topology.evs.tcloudpublic.com/zone` node label before restarting the node plugin, otherwise registration fails with a topology collision.

`CSI_BACKEND` selects the backend. Each backend is a separate CSI driver with its own name, socket, `CSIDriver` object, and controller/node workloads, so several backends can run side by side in one cluster from the same image.

| `CSI_BACKEND` | Default `CSI_DRIVER_NAME` | Status |
| --- | --- | --- |
| `evs` | `csi.evs.tcloudpublic.com` | implemented |
| `sfsturbo` | `csi.sfsturbo.tcloudpublic.com` | implemented |
| `gpsfs` | `csi.gpsfs.tcloudpublic.com` | planned |

If `CSI_BACKEND` is omitted, the driver defaults to `evs`. Unknown values fail at startup. If `CSI_DRIVER_NAME` is omitted, it defaults to the backend driver name, and `CSI_ENDPOINT` defaults to `unix:///var/lib/kubelet/plugins/<driver name>/csi.sock`.

`CSI_MAX_VOLUMES_PER_NODE` applies to block backends only. File-share backends report no per-node volume limit and no topology.

## References

- OpenStack Cinder CSI references: https://github.com/kubernetes/cloud-provider-openstack
- T Cloud Public Go SDK: https://github.com/opentelekomcloud/gophertelekomcloud
