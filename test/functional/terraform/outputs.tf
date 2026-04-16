data "opentelekomcloud_cce_cluster_kubeconfig_v3" "e2e" {
  cluster_id = opentelekomcloud_cce_cluster_v3.e2e.id
}

locals {
  kubeconfig_raw = yamldecode(data.opentelekomcloud_cce_cluster_kubeconfig_v3.e2e.kubeconfig)

  preferred_server = (
    var.kubeconfig_server == "external" && opentelekomcloud_cce_cluster_v3.e2e.external != "" ? opentelekomcloud_cce_cluster_v3.e2e.external :
    var.kubeconfig_server == "external_otc" && opentelekomcloud_cce_cluster_v3.e2e.external_otc != "" ? opentelekomcloud_cce_cluster_v3.e2e.external_otc :
    opentelekomcloud_cce_cluster_v3.e2e.external != "" ? opentelekomcloud_cce_cluster_v3.e2e.external :
    opentelekomcloud_cce_cluster_v3.e2e.external_otc != "" ? opentelekomcloud_cce_cluster_v3.e2e.external_otc :
    opentelekomcloud_cce_cluster_v3.e2e.internal
  )

  kubeconfig = merge(
    local.kubeconfig_raw,
    {
      clusters = [
        for cluster in local.kubeconfig_raw.clusters : merge(
          cluster,
          {
            cluster = merge(
              cluster.cluster,
              {
                server = local.preferred_server
              }
            )
          }
        )
      ]
    }
  )
}

output "cluster_id" {
  description = "Ephemeral CCE cluster ID."
  value       = opentelekomcloud_cce_cluster_v3.e2e.id
}

output "cluster_name" {
  description = "Ephemeral CCE cluster name."
  value       = opentelekomcloud_cce_cluster_v3.e2e.name
}

output "vpc_id" {
  description = "Ephemeral VPC ID."
  value       = opentelekomcloud_vpc_v1.e2e.id
}

output "cluster_api_public_ip" {
  description = "CCE API public IP address, if public access is enabled."
  value       = var.cluster_public_access ? opentelekomcloud_vpc_eip_v1.cluster_api[0].publicip[0].ip_address : ""
}

output "subnet_id" {
  description = "Ephemeral VPC subnet ID."
  value       = opentelekomcloud_vpc_subnet_v1.e2e.subnet_id
}

output "network_id" {
  description = "Ephemeral subnet network ID used by CCE."
  value       = opentelekomcloud_vpc_subnet_v1.e2e.network_id
}

output "node_ids" {
  description = "Ephemeral CCE worker node IDs."
  value       = opentelekomcloud_cce_node_v3.e2e[*].id
}

output "kubeconfig" {
  description = "Generated kubeconfig for the ephemeral CCE cluster."
  value       = yamlencode(local.kubeconfig)
  sensitive   = true
}
