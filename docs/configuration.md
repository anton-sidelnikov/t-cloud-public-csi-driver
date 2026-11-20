# Configuration

## Cloud Secret

The controller and node plugins read the credentials from the secret `tcloud-public-csi-cloud-config` in the driver namespace: `tcloud-public-csi-system` (EVS) and `tcloud-public-csi-sfsturbo` (SFS Turbo). Template: [deploy/kubernetes/secret.example.yaml](../deploy/kubernetes/secret.example.yaml).

| Key                                                 | Required  | Description                                                                          |
|-----------------------------------------------------|-----------|--------------------------------------------------------------------------------------|
| `OS_AUTH_URL`                                       | yes       | IAM endpoint, for example `https://iam.eu-de.otc.t-systems.com/v3`.                  |
| `OS_REGION`                                         | yes       | Region, for example `eu-de`.                                                         |
| `OS_DOMAIN_NAME`                                    | yes       | IAM domain (account) name.                                                           |
| `OS_USERNAME`, `OS_PASSWORD`                        | yes       | IAM user.                                                                            |
| `OS_PROJECT_ID` or `OS_PROJECT_NAME`                | yes       | Project.                                                                             |
| `OS_AVAILABILITY_ZONE`                              | no        | Default zone. Nodes detect their zone themselves; this value is a fallback.          |
| `OS_VPC_ID`, `OS_SUBNET_ID`, `OS_SECURITY_GROUP_ID` | SFS Turbo | Default network of new file systems. `OS_SUBNET_ID` is the network ID of the subnet. |

## Driver Settings

Set in the controller and node manifests; the defaults fit most clusters.

| Variable                   | Default     | Description                                                     |
|----------------------------|-------------|-----------------------------------------------------------------|
| `CSI_BACKEND`              | `evs`       | `evs` or `sfsturbo`.                                            |
| `CSI_DRIVER_NAME`          | per backend | CSI driver name.                                                |
| `CSI_MAX_VOLUMES_PER_NODE` | `32`        | EVS disks per node.                                             |
| `CSI_REQUEST_TIMEOUT`      | `2m`        | Timeout of cloud operations (`10m` in the SFS Turbo manifests). |
