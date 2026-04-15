locals {
  kubeconfig_server = (
    var.kubeconfig_server == "external_otc" ? opentelekomcloud_cce_cluster_v3.e2e.external_otc :
    var.kubeconfig_server == "external" ? opentelekomcloud_cce_cluster_v3.e2e.external :
    opentelekomcloud_cce_cluster_v3.e2e.internal
  )

  kubeconfig = {
    apiVersion        = "v1"
    kind              = "Config"
    "current-context" = opentelekomcloud_cce_cluster_v3.e2e.name
    clusters = [
      {
        name = opentelekomcloud_cce_cluster_v3.e2e.certificate_clusters[0].name
        cluster = {
          server                       = local.kubeconfig_server
          "certificate-authority-data" = opentelekomcloud_cce_cluster_v3.e2e.certificate_clusters[0].certificate_authority_data
        }
      }
    ]
    contexts = [
      {
        name = opentelekomcloud_cce_cluster_v3.e2e.name
        context = {
          cluster = opentelekomcloud_cce_cluster_v3.e2e.certificate_clusters[0].name
          user    = opentelekomcloud_cce_cluster_v3.e2e.certificate_users[0].name
        }
      }
    ]
    users = [
      {
        name = opentelekomcloud_cce_cluster_v3.e2e.certificate_users[0].name
        user = {
          "client-certificate-data" = opentelekomcloud_cce_cluster_v3.e2e.certificate_users[0].client_certificate_data
          "client-key-data"         = opentelekomcloud_cce_cluster_v3.e2e.certificate_users[0].client_key_data
        }
      }
    ]
  }
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
