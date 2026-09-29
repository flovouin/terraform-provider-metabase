resource "metabase_database" "bigquery" {
  name = "🗃️ Big Query"

  bigquery_details = {
    service_account_key      = file("sa-key.json")
    project_id               = "gcp-project"
    dataset_filters_type     = "inclusion"
    dataset_filters_patterns = "included_dataset"
  }
}

resource "metabase_permissions_group" "data_analysts" {
  name = "🧑‍🔬 Data Analysts"
}

# Only the permissions of the Data Analysts group on this database are managed. The permissions of other groups and
# databases are left untouched.
resource "metabase_database_permission" "data_analysts_bigquery" {
  group          = metabase_permissions_group.data_analysts.id
  database       = metabase_database.bigquery.id
  view_data      = "unrestricted"
  create_queries = "query-builder-and-native"
}
