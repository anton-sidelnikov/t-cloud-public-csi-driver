# Functional Test Infrastructure

This Terraform module provisions ephemeral T Cloud Public infrastructure for EVS functional tests:

- VPC
- subnet
- ECS key pair
- CCE cluster
- CCE worker nodes
- generated kubeconfig output

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
KUBECONFIG=.cache/functional/kubeconfig \
CSI_TEST_IMAGE=ghcr.io/<owner>/t-cloud-public-csi-driver:<tag> \
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
