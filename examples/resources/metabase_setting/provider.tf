terraform {
  required_providers {
    metabase = {
      source = "registry.terraform.io/ZeroGachis/metabase"
    }
  }
}

variable "metabase_endpoint" {
  description = "The URL to the Metabase API."
  type        = string
}

variable "metabase_username" {
  description = "The user name (or email address) to use to authenticate."
  type        = string
}

variable "metabase_password" {
  description = "The password to use to authenticate."
  type        = string
  sensitive   = true
}

provider "metabase" {
  endpoint = var.metabase_endpoint
  username = var.metabase_username
  password = var.metabase_password
}
