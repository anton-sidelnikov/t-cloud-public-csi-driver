# T Cloud Public CSI Driver

This repository hosts an EVS-first Container Storage Interface driver for T Cloud Public.

The initial slice in this repository focuses on:

- CSI identity service wiring
- CSI controller methods for EVS volume create, delete, attach, detach, and expand
- CSI node methods for stage, publish, unpublish, unstage, and filesystem expansion
- a multi-driver backend abstraction so additional T Cloud Public storage drivers can be added later
- T Cloud Public authentication and service client initialization through `github.com/opentelekomcloud/gophertelekomcloud`

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

Functional EVS tests are designed to run against an ephemeral T Cloud Public CCE cluster provisioned with Terraform and the OpenTelekomCloud provider.

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

The current Go functional package includes:

- a bootstrap test that installs the CSI manifests into the generated cluster, creates the cloud secret from `OS_*`, patches the driver image to `CSI_TEST_IMAGE`, and verifies the driver rolls out successfully
- a filesystem lifecycle test that provisions an EVS-backed PVC, mounts it in a pod, writes and reads test data, and then performs best-effort namespace cleanup

The next functional-test steps are raw block, expansion, and reclaim-policy scenarios.

The Terraform functional scaffold now defaults kubeconfig generation to the direct public CCE endpoint (`external`) instead of `external_otc`, because that behaves more reliably for normal `kubectl` operations from outside the cluster VPC.

If the generated kubeconfig points to a CCE endpoint whose certificate chain is not trusted by the machine running the tests, the functional runner automatically retries `kubectl` with `--insecure-skip-tls-verify=true`. The bootstrap test also disables OpenAPI schema validation for `kubectl apply -k`, which avoids false failures on clusters whose public endpoint does not expose schema discovery cleanly. You can still force insecure TLS mode up front with:

```bash
CSI_TEST_INSECURE_SKIP_TLS_VERIFY=true make test-functional
```

## Kubernetes Manifests

Baseline manifests live in [deploy/kubernetes](deploy/kubernetes).

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

- OpenStack Cinder CSI references: https://github.com/kubernetes/cloud-provider-openstack
- T Cloud Public Go SDK: https://github.com/opentelekomcloud/gophertelekomcloud
