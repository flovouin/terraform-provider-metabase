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
