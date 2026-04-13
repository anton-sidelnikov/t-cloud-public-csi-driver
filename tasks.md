# Tasks

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
- Resolved Go module dependencies and verified the scaffold builds with `go test ./...`.
- Added unit tests for config parsing, controller request handling, node volume flows, node info exposure, and EVS helper logic.

## In Progress

- Documenting assumptions, limitations, and the next iteration toward a deployable controller/node stack.
- Designing the next test layer so functional coverage can be added once node mounting and deployment assets exist.
- Shaping the next backend addition so a second storage driver can plug into the shared CSI framework instead of duplicating EVS-specific logic.

## Planned

- Add functional tests for end-to-end provisioning, attach/detach, mount, and expansion flows against a real or ephemeral test environment.
- Add snapshot support if the target EVS API surface exposes stable snapshot semantics through the SDK-backed clients.
