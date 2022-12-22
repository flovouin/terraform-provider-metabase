data "metabase_table" "table" {
  name = "my_table"
}

resource "metabase_card" "some_great_insights" {
  # To avoid unnecessary diffs, the exact list of attributes below should be specified.
  json = jsonencode({
    name                = "💡 Some great insights"
    description         = "📖 You will learn a lot from this graph!"
    collection_id       = null
    collection_position = null
    cache_ttl           = null
    query_type          = "query"
    dataset_query = {
      database = data.metabase_table.table.db_id
      query = {
        source-table = data.metabase_table.table.id
        aggregation = [
          ["count"]
        ]
        breakout = [
          ["field", data.metabase_table.table.fields["group_by_column"], null]
        ]
      }
      type = "query"
    }
    parameter_mappings     = []
    display                = "pie"
    visualization_settings = {}
    parameters             = []
  })
}
