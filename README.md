# T Cloud Public CSI Driver

This repository hosts an EVS-first Container Storage Interface driver for T Cloud Public.

The initial slice in this repository focuses on:

- CSI identity service wiring
- CSI controller methods for EVS volume create, delete, attach, detach, and expand
- CSI node methods for stage, publish, unpublish, unstage, and filesystem expansion
- a multi-driver backend abstraction so additional T Cloud Public storage drivers can be added later
- T Cloud Public authentication and service client initialization through `github.com/opentelekomcloud/gophertelekomcloud`
- A task ledger in `tasks.md` so future work starts from the current state

## Current Status

This is an initial scaffold, not a production-ready driver yet.

The repository is now structured as a multi-driver CSI codebase.
Today only the `evs` backend is implemented, but the common controller/node plumbing no longer assumes EVS must be the only backend forever.

Implemented:

- `CreateVolume`
- `DeleteVolume`
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

- Snapshot APIs
- Functional tests

## Test Status

Current unit coverage includes:

- environment-based config parsing and defaulting
- controller request validation and service interaction shaping
- node staging, publishing, unpublishing, expansion, and topology exposure
- EVS helper behavior such as GiB rounding and response mapping

Run:

```bash
go test ./...
```

## Development

Common local tasks are exposed through the [Makefile](/Users/antonsidelnikov/GolandProjects/t-cloud-public-csi-driver/Makefile).

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

GitHub Actions workflow definitions live in [.github/workflows](/Users/antonsidelnikov/GolandProjects/t-cloud-public-csi-driver/.github/workflows).

The current CI pipeline runs:

- `go vet`
- `go test ./...`
- `golangci-lint`
- Docker image build on pull requests
- Docker image build and push to GHCR on pushes to `main` and version tags

## Functional Tests

Functional EVS tests are designed to run against an ephemeral T Cloud Public CCE cluster provisioned with Terraform and the OpenTelekomCloud provider.

The infrastructure scaffold lives in [test/functional/terraform](./test/functional/terraform). It creates a VPC, subnet, CCE cluster, worker nodes, and a generated kubeconfig output. Authentication is read from the same `OS_*` environment variables used by the CSI driver:

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

Run the functional test scaffold:

```bash
CSI_TEST_IMAGE=ghcr.io/<owner>/t-cloud-public-csi-driver:<tag> make test-functional
```

Destroy infrastructure:

```bash
make functional-infra-down
```

Terraform variables can still be overridden explicitly with `TF_VAR_*` when needed, for example `TF_VAR_node_count=3 make functional-infra-up`.

The current Go functional package is only a scaffold. The next step is to add tests that install the CSI manifests into the generated cluster, create the cloud secret from `OS_*`, and run filesystem, raw block, expansion, and reclaim-policy scenarios.

## Kubernetes Manifests

Baseline manifests live in [deploy/kubernetes](/Users/antonsidelnikov/GolandProjects/t-cloud-public-csi-driver/deploy/kubernetes).

Included:

- namespace and example cloud credential secret
- controller `Deployment` with `csi-provisioner`, `csi-attacher`, and `csi-resizer`
- node `DaemonSet` with `node-driver-registrar`
- RBAC for controller and node components
- `CSIDriver` object
- example EVS `StorageClass`

Apply the bundle with:

```bash
kubectl apply -k deploy/kubernetes
```

Before applying, replace the placeholder image `ghcr.io/example/tcloud-public-csi-driver:dev` and create a real secret from `deploy/kubernetes/secret.example.yaml`.

Current manifest assumptions:

- controller and node components both consume cloud credentials from the same Kubernetes `Secret`
- the node plugin runs privileged and mounts `/dev`, `/sys`, and the full host `/var/lib/kubelet` with bidirectional mount propagation
- snapshot sidecars are not included yet because snapshot APIs are not implemented in the driver

## EVS Operational Assumptions

The EVS backend currently assumes:

- Kubernetes node identity can be resolved to the ECS instance UUID. The node plugin first uses `Node.spec.providerID` when it contains a UUID and falls back to `Node.status.nodeInfo.systemUUID`.
- `CSI_NODE_ID` is set from `spec.nodeName` in the node DaemonSet and is used only as the Kubernetes `Node` lookup key unless it is already a UUID.
- EVS volumes are single-node block devices. The driver accepts CSI single-node access modes and rejects multi-node writer modes.
- EVS volumes must be provisioned in an availability zone compatible with the node selected by Kubernetes. The example StorageClasses use `WaitForFirstConsumer`.
- Raw block volumes require the node plugin to see host kubelet block-device publish paths under `/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices`.
- Online filesystem expansion requires controller expansion followed by node filesystem expansion. The controller treats EVS states `available` and `in-use` as valid completed expansion states when the size is updated.

Useful checks:

```bash
kubectl get node <node-name> -o jsonpath='{.spec.providerID}{"\n"}'
kubectl get node <node-name> -o jsonpath='{.status.nodeInfo.systemUUID}{"\n"}'
kubectl -n tcloud-public-csi-system logs -l app=tcloud-public-csi-node -c tcloud-public-csi-driver --tail=200
kubectl -n tcloud-public-csi-system logs -l app=tcloud-public-csi-controller -c tcloud-public-csi-driver --tail=200
```

Manual EVS validation manifests live in [deploy/manual/evs](/Users/antonsidelnikov/GolandProjects/t-cloud-public-csi-driver/deploy/manual/evs).

They cover:

- filesystem PVC + pod validation
- raw block PVC + pod validation
- online expansion checks
- reclaim policy checks for `Delete` and `Retain`

## EVS StorageClass Parameters

The EVS backend validates `StorageClass.parameters` explicitly. Unsupported keys are rejected during `CreateVolume` so typos fail early instead of being silently ignored.

Supported parameters:

- `volumeType`: EVS volume type, for example `SSD`.
- `availabilityZone`: EVS availability zone override. If omitted, the driver uses `OS_AVAILABILITY_ZONE`.
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
  availabilityZone: eu-de-01
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

Use either `OS_PROJECT_ID` or `OS_PROJECT_NAME`.

`CSI_BACKEND` currently supports:

- `evs`

If `CSI_BACKEND` is omitted, the driver defaults to `evs`.

## References

- Huawei Cloud CSI Driver: https://github.com/huaweicloud/huaweicloud-csi-driver
- OpenStack Cinder CSI references: https://github.com/kubernetes/cloud-provider-openstack
- T Cloud Public Go SDK: https://github.com/opentelekomcloud/gophertelekomcloud
