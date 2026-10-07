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

resource "metabase_permissions_group" "business_stakeholders" {
  name = "👔 Business Stakeholders"
}

resource "metabase_permissions_graph" "graph" {
  advanced_permissions = false

  # The permissions of the (group, database) pairs which are not listed below, e.g. for the "All Users" group, are
  # revoked: the group can still view data through the questions it has access to, but it cannot create queries nor
  # download results. On paid plans with advanced permissions, a pair can be listed with `view_data = "blocked"` to
  # also prevent the group from viewing data.

  permissions = [
    {
      group          = metabase_permissions_group.data_analysts.id
      database       = metabase_database.bigquery.id
      view_data      = "unrestricted"
      create_queries = "query-builder-and-native"
    },
    {
      group    = metabase_permissions_group.business_stakeholders.id
      database = metabase_database.bigquery.id
      # This looks like no other value can be set, at least in the free version of Metabase.
      view_data      = "unrestricted"
      create_queries = "query-builder"
    },
  ]
}
