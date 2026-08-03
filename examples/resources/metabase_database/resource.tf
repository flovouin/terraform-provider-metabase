variable "database_password" {
  description = "Password used by Metabase to connect to the custom database."
  type        = string
  sensitive   = true
  ephemeral   = true
}

resource "metabase_database" "bigquery" {
  name = "🗃️ Big Query"

  bigquery_details = {
    service_account_key      = file("sa-key.json")
    project_id               = "gcp-project"
    dataset_filters_type     = "inclusion"
    dataset_filters_patterns = "included_dataset"
  }
}

resource "metabase_database" "imported" {
  name = "⬇️ Imported"

  bigquery_details = {
    # If you don't have access to the service account key, you can use the redacted value to ensure there is no diff
    # when importing the resource. If you do have a key, a one-time apply will be needed to reset the key.
    service_account_key      = "**MetabasePass**"
    project_id               = "gcp-project"
    dataset_filters_type     = "exclusion"
    dataset_filters_patterns = "excluded_dataset"
  }
}

# If an engine is not supported by the provider, you can also set a raw configuration that will be passed through to the
# Metabase API.
resource "metabase_database" "custom" {
  name = "🔧 Custom"

  custom_details = {
    engine = "postgres"

    details_json = jsonencode({
      host                    = "127.0.0.1"
      port                    = 5432
      dbname                  = "database"
      user                    = "user"
      schema-filters-type     = "inclusion"
      schema-filters-patterns = "this_schema_only"
      ssl                     = false
      tunnel-enabled          = false
      advanced-options        = false
    })

    sensitive_details_json_wo = jsonencode({
      password = var.database_password
    })
    sensitive_details_json_wo_version = 1

    # Detail attributes redacted by Metabase should be listed here so they are not incorrectly detected as a change.
    # Also list attributes that are present in both JSON objects.
    redacted_attributes = [
      "password",
    ]
  }
}
