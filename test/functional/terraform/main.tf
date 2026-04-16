locals {
  common_tags = {
    project = "t-cloud-public-csi-driver"
    purpose = "functional-tests"
  }

  cluster_name = "${var.name_prefix}-cluster"
}

resource "opentelekomcloud_vpc_eip_v1" "cluster_api" {
  count = var.cluster_public_access ? 1 : 0

  publicip {
    type = var.cluster_api_eip_type
    name = "${var.name_prefix}-cce-api"
  }

  bandwidth {
    name        = var.cluster_api_bandwidth_name
    size        = var.cluster_api_bandwidth_size
    share_type  = var.cluster_api_bandwidth_share_type
    charge_mode = var.cluster_api_bandwidth_charge_mode
  }

  tags = local.common_tags
}

resource "opentelekomcloud_vpc_v1" "e2e" {
  name = "${var.name_prefix}-vpc"
  cidr = var.vpc_cidr
}

resource "opentelekomcloud_vpc_subnet_v1" "e2e" {
  name              = "${var.name_prefix}-subnet"
  cidr              = var.subnet_cidr
  gateway_ip        = var.subnet_gateway_ip
  vpc_id            = opentelekomcloud_vpc_v1.e2e.id
  availability_zone = var.availability_zone

  tags = local.common_tags
}

resource "opentelekomcloud_compute_keypair_v2" "e2e" {
  name = "${var.name_prefix}-keypair"
}

resource "opentelekomcloud_cce_cluster_v3" "e2e" {
  name                    = local.cluster_name
  description             = "Ephemeral cluster for T Cloud Public CSI driver EVS functional tests"
  cluster_type            = "VirtualMachine"
  flavor_id               = var.cluster_flavor_id
  cluster_version         = var.cluster_version != "" ? var.cluster_version : null
  vpc_id                  = opentelekomcloud_vpc_v1.e2e.id
  subnet_id               = opentelekomcloud_vpc_subnet_v1.e2e.network_id
  container_network_type  = "overlay_l2"
  container_network_cidr  = var.container_network_cidr
  kubernetes_svc_ip_range = var.kubernetes_svc_ip_range
  authentication_mode     = "rbac"
  kube_proxy_mode         = "ipvs"
  api_access_trustlist    = length(var.cluster_api_access_trustlist) > 0 ? var.cluster_api_access_trustlist : null
  timezone                = "UTC"
  billing_mode            = 0
  eip                     = var.cluster_public_access ? opentelekomcloud_vpc_eip_v1.cluster_api[0].publicip[0].ip_address : null

  delete_all_storage = "true"
  delete_all_network = "true"
}

resource "opentelekomcloud_cce_node_v3" "e2e" {
  count = var.node_count

  name              = format("%s-node-%02d", var.name_prefix, count.index + 1)
  cluster_id        = opentelekomcloud_cce_cluster_v3.e2e.id
  availability_zone = var.availability_zone
  subnet_id         = opentelekomcloud_vpc_subnet_v1.e2e.network_id
  os                = var.node_os
  flavor_id         = var.node_flavor_id
  key_pair          = opentelekomcloud_compute_keypair_v2.e2e.name
  runtime           = "containerd"
  billing_mode      = 0
  agency_name       = var.node_agency_name != "" ? var.node_agency_name : null
  bandwidth_size    = var.node_bandwidth_size

  labels = {
    "tcloudpublic.com/csi-e2e" = "true"
  }

  root_volume {
    size       = var.node_root_volume_size
    volumetype = var.node_root_volume_type
  }

  data_volumes {
    size       = var.node_data_volume_size
    volumetype = var.node_data_volume_type
    extend_params = {
      useType = "docker"
    }
  }
}
