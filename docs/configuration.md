# Configuration

## Cloud Secret

The controller and node plugins read the credentials from the secret `tcloud-public-csi-cloud-config` in the driver namespace: `tcloud-public-csi-system` (EVS), `tcloud-public-csi-sfsturbo` (SFS Turbo) and `tcloud-public-csi-gpsfs` (SFS General Purpose). Template: [deploy/kubernetes/secret.example.yaml](../deploy/kubernetes/secret.example.yaml).

| Key                                                 | Required  | Description                                                                          |
|-----------------------------------------------------|-----------|--------------------------------------------------------------------------------------|
| `OS_AUTH_URL`                                       | yes       | IAM endpoint, for example `https://iam.eu-de.otc.t-systems.com/v3`.                  |
| `OS_REGION`                                         | yes       | Region, for example `eu-de`.                                                         |
| `OS_DOMAIN_NAME`                                    | yes       | IAM domain (account) name.                                                           |
| `OS_USERNAME`, `OS_PASSWORD`                        | yes       | IAM user.                                                                            |
| `OS_PROJECT_ID` or `OS_PROJECT_NAME`                | yes       | Project.                                                                             |
| `OS_ACCESS_KEY`, `OS_SECRET_KEY`                    | no        | SFS General Purpose only: IAM access keys used instead of the password.              |
| `OS_SECURITY_TOKEN`                                 | no        | SFS General Purpose only: security token of temporary access keys.                   |
| `OS_AVAILABILITY_ZONE`                              | no        | Default zone. Nodes detect their zone themselves; this value is a fallback.          |
| `OS_VPC_ID`, `OS_SUBNET_ID`, `OS_SECURITY_GROUP_ID` | SFS Turbo | Default network of new file systems. `OS_SUBNET_ID` is the network ID of the subnet. |
| `OS_VPC_ID`                                         | SFS GP    | Default VPC allowed to access new file systems.                                      |

## Driver Settings

Set in the controller and node manifests; the defaults fit most clusters.

| Variable                   | Default     | Description                                                                       |
|----------------------------|-------------|-----------------------------------------------------------------------------------|
| `CSI_BACKEND`              | `evs`       | `evs`, `sfsturbo` or `gpsfs`.                                                     |
| `CSI_DRIVER_NAME`          | per backend | CSI driver name.                                                                  |
| `CSI_MODE`                 | `all`       | `node` in the SFS General Purpose node plugin, which then needs only `OS_REGION`. |
| `CSI_MAX_VOLUMES_PER_NODE` | `32`        | EVS disks per node.                                                               |
| `CSI_REQUEST_TIMEOUT`      | `2m`        | Timeout of cloud operations (`10m` in the SFS Turbo manifests).                   |

## SFS General Purpose Authentication

SFS General Purpose accepts IAM access keys (AK/SK). Either:

- set `OS_REGION`, `OS_ACCESS_KEY` and `OS_SECRET_KEY` of an IAM user; the other `OS_*` keys can then be omitted. Add `OS_SECURITY_TOKEN` for temporary keys, which must be replaced in the secret before they expire; or
- set the password credentials only; the driver then obtains temporary access keys itself and renews them.

Only the controller uses the credentials; the node plugin gets only `OS_REGION` and `OS_AVAILABILITY_ZONE` from the secret.
