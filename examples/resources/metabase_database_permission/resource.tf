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

# Metabase grants the All Users group full access to new databases. The lowest permissions make sure it can only view the
# data of the questions it has access to.
resource "metabase_database_permission" "all_users_bigquery" {
  group          = 1
  database       = metabase_database.bigquery.id
  view_data      = "unrestricted"
  create_queries = "no"
  download = {
    schemas = "none"
  }
}

resource "metabase_permissions_group" "marketing" {
  name = "📣 Marketing"
}

data "metabase_table" "campaigns" {
  db_id  = metabase_database.bigquery.id
  schema = "included_dataset"
  name   = "campaigns"
}

# Permissions can also be set per schema (dataset for BigQuery) or per table, with a JSON object. For a single table,
# Metabase only accepts `query-builder` or `no`, as native queries can only be allowed for the entire database.
resource "metabase_database_permission" "marketing_bigquery" {
  group     = metabase_permissions_group.marketing.id
  database  = metabase_database.bigquery.id
  view_data = "unrestricted"
  create_queries = jsonencode({
    included_dataset = {
      (data.metabase_table.campaigns.id) = "query-builder"
    }
  })
}
