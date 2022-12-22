resource "metabase_collection" "business_reports" {
  name        = "📈 Business reports"
  color       = "#32a852"
  description = "Contains reports accessible to business stakeholders."
}

resource "metabase_permissions_group" "data_analysts" {
  name = "🧑‍🔬 Data Analysts"
}

resource "metabase_permissions_group" "business_stakeholders" {
  name = "👔 Business Stakeholders"
}

resource "metabase_collection_graph" "graph" {
  permissions = [
    {
      group      = metabase_permissions_group.data_analysts.id
      collection = metabase_collection.business_reports.id
      permission = "write"
    },
    {
      group      = metabase_permissions_group.business_stakeholders.id
      collection = metabase_collection.business_reports.id
      permission = "read"
    },
  ]
}
