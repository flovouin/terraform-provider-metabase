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

  permissions = [
    {
      group    = metabase_permissions_group.data_analysts.id
      database = metabase_database.bigquery.id
      data = {
        # Native: Yes
        native = "write"
        # Data access: Unrestricted
        schemas = "all"
      }
    },
    {
      group    = metabase_permissions_group.business_stakeholders.id
      database = metabase_database.bigquery.id
      data = {
        # Native: No (by omitting the `native` attribute or setting it to "none")
        # Data access: Unrestricted
        schemas = "all"
      }
    },
    # Permissions for the "All Users" group. Those cannot be removed entirely, but they can be limited.
    # The example below gives the minimum set of permissions for the free version of Metabase:
    {
      group    = 1 # ID for the "All Users" group.
      database = 2
      # Cannot be removed but has no impact when using the free version of Metabase.
      download = {
        native  = "full"
        schemas = "full"
      }
      # Omitting the `data` attribute entirely result in the lowest level of permissions:
      # Data access: No self-service
      # Native: No
    },
  ]
}
