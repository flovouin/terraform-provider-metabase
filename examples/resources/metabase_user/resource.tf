resource "metabase_permissions_group" "data_analysts" {
  name = "🧑‍🔬 Data Analysts"
}

resource "metabase_user" "jane" {
  email      = "jane.doe@example.com"
  first_name = "Jane"
  last_name  = "Doe"
  locale     = "en"

  # Authoritative: groups that are not listed are removed from the user. The `All Users` group is implicit, and the
  # `Administrators` group is managed through `is_superuser`.
  group_ids = [metabase_permissions_group.data_analysts.id]
}

resource "metabase_user" "admin" {
  email        = "john.doe@example.com"
  first_name   = "John"
  last_name    = "Doe"
  is_superuser = true
}

# `is_active` is best left unset: the provider then reports users that were deactivated outside of Terraform, but never
# reactivates them. Setting it explicitly makes (de)activation an intentional, reviewable change.
resource "metabase_user" "returning" {
  email      = "erika.mustermann@example.com"
  first_name = "Erika"
  last_name  = "Mustermann"
  is_active  = true
}
