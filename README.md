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

## CI

GitHub Actions workflow definitions live in [.github/workflows](/Users/antonsidelnikov/GolandProjects/t-cloud-public-csi-driver/.github/workflows).

The current CI pipeline runs:

- `go vet`
- `go test ./...`
- `golangci-lint`
- Docker image build on pull requests
- Docker image build and push to GHCR on pushes to `main` and version tags

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
- the node plugin runs privileged and mounts `/dev`, `/sys`, and kubelet plugin paths
- snapshot sidecars are not included yet because snapshot APIs are not implemented in the driver

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
