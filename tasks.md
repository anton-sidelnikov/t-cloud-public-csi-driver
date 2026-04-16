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
- Added structured controller and EVS cloud-service logs for create, delete, attach, detach, and expand flows.
- Hardened EVS detach so controller unpublish waits until the volume attachment is removed from the cloud volume state.
- Validated and documented supported EVS `StorageClass` parameters, including explicit rejection of unknown keys.
- Added release metadata to the binary and container image, including version, commit, build date, startup logs, and OCI image labels.
- Hardened the EVS node path based on manual cluster testing, including block-volume publish idempotency coverage and device discovery checks.
- Documented EVS assumptions, limitations, and operational checks from the manual test runs.
- Expanded unit coverage around EVS error mapping, device discovery, resize behavior, and idempotent node/controller operations.
- Added an OpenTelekomCloud Terraform scaffold for ephemeral functional-test infrastructure: VPC, subnet, CCE cluster, worker nodes, generated kubeconfig, Makefile targets, and functional test package stub.
- Added functional-test image build and push targets so tests can deploy a driver image built from the current checkout.
- Added the first real functional test: bootstrap the CSI driver into an ephemeral CCE cluster, create the cloud secret from `OS_*`, patch the test image, wait for rollout, and verify `CSIDriver` registration.
- Added the first EVS lifecycle functional test: filesystem PVC provisioning, pod mount, write/read verification, and namespace-scoped cleanup in the ephemeral CCE environment.
- Changed node identity resolution to rely on Kubernetes `Node.status.nodeInfo.systemUUID` as the canonical ECS instance UUID, avoiding incorrect `spec.providerID` values returned by CCE.
- Added a separate manual GitHub Actions workflow for functional tests: build/push image, provision ephemeral infrastructure, run tests, collect diagnostics, and destroy infrastructure.
- Added a PR-comment-triggered functional workflow for same-repository PR branches. Maintainers can comment `run functional`, and the workflow posts start and result comments back to the PR.
- Improved the functional workflows so they upload raw `make test-functional` output and include the failed phase in PR result comments.
- Added a branch-runnable `workflow_dispatch` companion to the PR-comment functional workflow so workflow changes can be tested from the current branch and optionally report back to a PR.
- Updated the Makefile so golangci-lint uses a repo-local cache instead of the user cache directory.
- Adjusted the functional-test kubeconfig to prefer the direct public CCE endpoint and disabled schema validation for bootstrap `kubectl apply -k` and `kubectl delete -k`.
- Resolved Go module dependencies and verified the scaffold builds with `go test ./...`.
- Added unit tests for config parsing, controller request handling, node volume flows, node info exposure, and EVS helper logic.

## In Progress

- No active EVS implementation tasks.

## Planned

- Extend EVS lifecycle functional tests with raw block, online expansion, and reclaim-policy scenarios against the ephemeral test environment.
- Add Helm chart or production Kustomize overlays for installable EVS deployments.
- Document required IAM/API permissions for EVS, ECS attach/detach, and Kubernetes node metadata access.
- Add snapshot support if the target EVS API surface exposes stable snapshot semantics through the SDK-backed clients.
