terraform {
  required_version = ">= 1.6.0"

  required_providers {
    opentelekomcloud = {
      source  = "opentelekomcloud/opentelekomcloud"
      version = ">= 1.36.0"
    }
  }
}

provider "opentelekomcloud" {
  auth_url    = var.auth_url
  region      = var.region
  domain_name = var.domain_name
  user_name   = var.user_name
  password    = var.password
  tenant_id   = var.project_id != "" ? var.project_id : null
  tenant_name = var.project_name != "" ? var.project_name : null
}
