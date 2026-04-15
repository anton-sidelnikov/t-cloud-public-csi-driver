# Tasks
## Info
- Do not touch deploy/kubernetes/secret.yaml, never
## Done

- Scanned Huawei EVS CSI and OpenStack Cinder CSI projects to align the initial driver shape with established controller and sidecar patterns.
- Scanned OpenStack Cinder CSI operator patterns to keep room for a future operator-driven deployment model.
- Chose `gophertelekomcloud` as the only cloud API integration layer and standardized project naming on `T Cloud Public`.
- Bootstrapped the repository with an EVS-first CSI driver scaffold, configuration model, and cloud service abstraction.
- Implemented the first controller flow for EVS volumes: create, delete, attach, detach, inspect, and expand.
- Implemented node-side staging, publishing, unpublishing, unstaging, and filesystem expansion flows with mount and filesystem abstractions.
- Added a buildable main binary, CSI identity/controller/node servers, and baseline repository documentation.
- Added baseline Kubernetes deployment manifests, RBAC, sidecar wiring, a `CSIDriver` object, and an example EVS `StorageClass`.
- Refactored the codebase toward a multi-driver architecture with a backend abstraction layer and EVS as the first concrete backend.
- Added repository hygiene files: `.gitignore` and a `Makefile` for format, test, build, image, and manifest workflows.
- Added a multi-stage `Dockerfile` and `.dockerignore` so the driver can be built into a runnable container image.
- Added GitHub Actions CI for vet, test, lint, and Docker image build/push to GHCR.
- Added EVS manual test manifests for filesystem, raw block, expansion, and reclaim-policy validation.
- Added dedicated EVS retain-policy manual manifests and documented the retain lifecycle.
- Added node identity resolution from Kubernetes node metadata so attach uses the ECS instance UUID instead of the Kubernetes node name.
- Fixed online EVS expansion completion handling so attached volumes can finish while reporting `in-use`.
- Added node-side device path resolution for EVS publish paths, including retry and `/dev/disk/by-id` lookup.
- Updated the node DaemonSet to mount the host kubelet root so raw block publish targets under `/var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices` are visible to the node plugin.
- Added structured JSON startup and node-path logs for CSI server lifecycle, node identity resolution, and EVS device path resolution.
- Updated the Makefile so golangci-lint uses a repo-local cache instead of the user cache directory.
- Resolved Go module dependencies and verified the scaffold builds with `go test ./...`.
- Added unit tests for config parsing, controller request handling, node volume flows, node info exposure, and EVS helper logic.

## In Progress

- Hardening the EVS node path based on manual cluster testing, especially block-volume publish idempotency and device discovery.
- Documenting EVS assumptions, limitations, and operational checks from the manual test runs.
- Expanding unit coverage around EVS error mapping, device discovery, resize behavior, and idempotent node/controller operations.

## Planned

- Add functional tests for end-to-end provisioning, attach/detach, mount, and expansion flows against a real or ephemeral test environment.
- Add structured logs around controller CSI calls and cloud API operations.
- Add release metadata to the binary and container image, including version, commit, and build date.
- Add Helm chart or production Kustomize overlays for installable EVS deployments.
- Document required IAM/API permissions for EVS, ECS attach/detach, and Kubernetes node metadata access.
- Harden detach handling so the controller waits until EVS confirms attachment removal.
- Validate and document all supported EVS `StorageClass` parameters.
- Add snapshot support if the target EVS API surface exposes stable snapshot semantics through the SDK-backed clients.
