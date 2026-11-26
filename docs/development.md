# Development and Testing

## Build

```bash
make build    # binary
make test     # unit tests
make lint     # golangci-lint
make check    # fmt, vet, test
make image IMAGE=ghcr.io/example/tcloud-public-csi-driver:dev
```

## CI

[.github/workflows](../.github/workflows):

- `ci.yaml`: vet, unit tests, lint and image build; on `main` and `v*` tags the image is pushed to GHCR.
- `functional-pr-dispatch.yaml`: functional tests on a temporary CCE cluster for every pull request from this repository, or started manually. Pull requests from forks do not get functional runs.

## Functional Tests

The tests create a CCE cluster with Terraform ([test/functional/terraform](../test/functional/terraform)), install the driver and run the EVS, SFS Turbo and SFS General Purpose scenarios. The credentials are the `OS_*` variables from [configuration.md](configuration.md).

```bash
export OS_AUTH_URL=... OS_REGION=eu-de OS_DOMAIN_NAME=... OS_USERNAME=... OS_PASSWORD=... OS_PROJECT_ID=...

make functional-infra-init
make functional-infra-up
make functional-kubeconfig
make functional-image-push   # build and push an image reachable from the cluster
make test-functional
make functional-infra-down
```

`FUNCTIONAL_PACKAGES` selects the test packages (default `./test/functional/evs ./test/functional/sfsturbo ./test/functional/gpsfs`). The SFS Turbo tests are billed and add about 20–30 minutes. Set `CSI_TEST_INSECURE_SKIP_TLS_VERIFY=true` if the cluster certificate is not trusted. Set `OS_ACCESS_KEY` and `OS_SECRET_KEY` to test permanent access keys; otherwise temporary keys are used.

The SFS General Purpose authentication and file system API tests do not need a cluster, only the `OS_*` variables and `CSI_TEST_VPC_ID`:

```bash
CSI_TEST_VPC_ID=... go test -tags=functional ./test/functional/gpsfs -v -run 'Authentication|TestFileSystemLifecycle'
```

`make functional-cleanup-shares` deletes SFS Turbo shares and empty SFS General Purpose file systems that failed tests left in the functional VPC.

## Manual Tests

Step-by-step scenarios for a running cluster:

- [deploy/manual/evs](../deploy/manual/evs): filesystem, raw block, expansion, reclaim policy, snapshots, topology, encryption
- [deploy/manual/sfsturbo](../deploy/manual/sfsturbo): shared volume, expansion, reclaim policy, HPC and encryption
- [deploy/manual/gpsfs](../deploy/manual/gpsfs): shared volume, static volumes, deletion of non-empty file systems
