# Looking up a user that was created by SSO rather than by Terraform.
data "metabase_user" "jane" {
  email = "jane.doe@example.com"
}

# Users can also be looked up using their Metabase ID.
data "metabase_user" "by_id" {
  id = 3
}
