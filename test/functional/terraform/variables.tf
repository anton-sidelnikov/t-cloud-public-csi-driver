variable "name_prefix" {
  description = "Prefix used for all ephemeral functional-test resources."
  type        = string
  default     = "tcloud-csi-e2e"
}

variable "auth_url" {
  description = "T Cloud Public IAM auth URL. Usually passed from OS_AUTH_URL."
  type        = string
  sensitive   = true
}

variable "region" {
  description = "T Cloud Public region. Usually passed from OS_REGION."
  type        = string
}

variable "availability_zone" {
  description = "Availability zone for the subnet and worker nodes. Usually passed from OS_AVAILABILITY_ZONE."
  type        = string
}

variable "domain_name" {
  description = "IAM domain name. Usually passed from OS_DOMAIN_NAME."
  type        = string
  sensitive   = true
}

variable "user_name" {
  description = "IAM username. Usually passed from OS_USERNAME."
  type        = string
  sensitive   = true
}

variable "password" {
  description = "IAM password. Usually passed from OS_PASSWORD."
  type        = string
  sensitive   = true
}

variable "project_id" {
  description = "IAM project ID. Usually passed from OS_PROJECT_ID. Either project_id or project_name must be set."
  type        = string
  default     = ""
  sensitive   = true
}

variable "project_name" {
  description = "IAM project name. Usually passed from OS_PROJECT_NAME. Either project_id or project_name must be set."
  type        = string
  default     = ""
  sensitive   = true
}

variable "vpc_cidr" {
  description = "CIDR block for the ephemeral VPC."
  type        = string
  default     = "10.42.0.0/16"
}

variable "subnet_cidr" {
  description = "CIDR block for the ephemeral subnet."
  type        = string
  default     = "10.42.1.0/24"
}

variable "subnet_gateway_ip" {
  description = "Gateway IP inside subnet_cidr."
  type        = string
  default     = "10.42.1.1"
}

variable "cluster_flavor_id" {
  description = "CCE cluster flavor."
  type        = string
  default     = "cce.s1.small"
}

variable "cluster_version" {
  description = "CCE Kubernetes version. Empty lets CCE choose the current default."
  type        = string
  default     = ""
}

variable "cluster_api_access_trustlist" {
  description = "CIDR allowlist for CCE API access. Empty uses provider/service defaults."
  type        = list(string)
  default     = []
}

variable "container_network_cidr" {
  description = "Pod network CIDR used by the CCE overlay network."
  type        = string
  default     = "172.16.0.0/16"
}

variable "kubernetes_svc_ip_range" {
  description = "Kubernetes service network CIDR."
  type        = string
  default     = "10.247.0.0/16"
}

variable "node_count" {
  description = "Number of worker nodes."
  type        = number
  default     = 1
}

variable "node_flavor_id" {
  description = "ECS flavor for CCE worker nodes."
  type        = string
  default     = "s2.large.2"
}

variable "node_os" {
  description = "CCE worker node operating system."
  type        = string
  default     = "EulerOS 2.9"
}

variable "node_agency_name" {
  description = "Optional IAM agency attached to worker nodes."
  type        = string
  default     = ""
}

variable "node_bandwidth_size" {
  description = "Worker-node EIP bandwidth size in Mbit/s. Set to null to omit."
  type        = number
  default     = 100
}

variable "node_root_volume_type" {
  description = "EVS volume type for worker root disks."
  type        = string
  default     = "SAS"
}

variable "node_root_volume_size" {
  description = "Worker root disk size in GiB."
  type        = number
  default     = 40
}

variable "node_data_volume_type" {
  description = "EVS volume type for worker data disks."
  type        = string
  default     = "SAS"
}

variable "node_data_volume_size" {
  description = "Worker data disk size in GiB."
  type        = number
  default     = 100
}

variable "kubeconfig_server" {
  description = "Cluster endpoint to put into generated kubeconfig. Supported values: external_otc, external, internal."
  type        = string
  default     = "external_otc"

  validation {
    condition     = contains(["external_otc", "external", "internal"], var.kubeconfig_server)
    error_message = "kubeconfig_server must be one of: external_otc, external, internal."
  }
}
