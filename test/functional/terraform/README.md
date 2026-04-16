# Functional Test Infrastructure

This Terraform module provisions ephemeral T Cloud Public infrastructure for EVS functional tests:

- VPC
- subnet
- public EIP for the CCE API endpoint
- ECS key pair
- CCE cluster
- CCE worker nodes
- generated kubeconfig output

The kubeconfig output is fetched from the OpenTelekomCloud provider's CCE kubeconfig data source instead of being assembled manually, and then its API server is rewritten to the preferred endpoint selected by `kubeconfig_server`:

- `external` first
- `external_otc` when requested and available
- `internal` as fallback

By default the module also allocates and attaches a public EIP to the CCE API endpoint so local or GitHub-hosted runners can reach the cluster. You can disable that with `TF_VAR_cluster_public_access=false` if the tests run from inside the VPC.

For local `kubectl` and functional-test traffic, the direct public CCE endpoint (`external`) is the default because it behaves like a normal Kubernetes API server. `external_otc` is still available, but it is not the preferred default for bootstrap and schema-discovery-heavy workflows.

The Makefile maps the standard `OS_*` environment variables into Terraform provider variables:

```bash
export OS_AUTH_URL=https://iam.example.com/v3
export OS_REGION=eu-de
export OS_AVAILABILITY_ZONE=eu-de-01
export OS_DOMAIN_NAME=Default
export OS_USERNAME=replace-me
export OS_PASSWORD=replace-me
export OS_PROJECT_ID=replace-me
```

Create the cluster:

```bash
make functional-infra-init
make functional-infra-up
make functional-kubeconfig
```

You can override any Terraform variable explicitly with `TF_VAR_*`, for example:

```bash
TF_VAR_node_count=3 TF_VAR_node_flavor_id=s3.large.2 make functional-infra-up
```

Run functional tests once they are implemented:

```bash
FUNCTIONAL_IMAGE=ghcr.io/<owner>/t-cloud-public-csi-driver:e2e-$(git rev-parse --short=12 HEAD) make functional-image
FUNCTIONAL_IMAGE=ghcr.io/<owner>/t-cloud-public-csi-driver:e2e-$(git rev-parse --short=12 HEAD) make functional-image-push
KUBECONFIG=.cache/functional/kubeconfig \
FUNCTIONAL_IMAGE=ghcr.io/<owner>/t-cloud-public-csi-driver:e2e-$(git rev-parse --short=12 HEAD) \
make test-functional
```

Destroy the infrastructure:

```bash
make functional-infra-down
```

Operational notes:

- CCE must be authorized in the target project before cluster creation.
- The generated Terraform state contains sensitive data, including kubeconfig material and generated keypair data. Keep it local and do not commit it.
- In CI, run destroy in an `always()`/finally step.
- Restrict `cluster_api_access_trustlist` to CI runner or developer IP ranges when possible.
