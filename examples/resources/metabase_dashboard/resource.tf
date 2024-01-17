data "metabase_table" "table" {
  name = "my_table"
}

resource "metabase_card" "some_great_insights" {
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

resource "metabase_dashboard" "some_great_dashboard" {
  name        = "📈 Some great dashboard"
  description = "💡 With plenty of actionable stuff."

  parameters_json = jsonencode([
    {
      id        = "83e68ca2"
      name      = "Date range"
      slug      = "date_filter"
      type      = "date/all-options"
      sectionId = "date"
      default   = "past30days"
    },
  ])

  cards_json = jsonencode([
    {
      card_id                = metabase_card.some_great_insights.id
      col                    = 7
      row                    = 0
      size_x                 = 11
      size_y                 = 6
      series                 = []
      visualization_settings = {}
      parameter_mappings = [
        {
          parameter_id = "83e68ca2",
          card_id      = metabase_card.some_great_insights.id,
          target = [
            "dimension",
            ["field", data.metabase_table.table.fields["filter_date_column"], null]
          ]
        }
      ]
    },
    {
      card_id = null
      col     = 0
      row     = 0
      size_x  = 7
      size_y  = 6
      series  = []
      visualization_settings = {
        virtual_card = {
          name                   = null
          display                = "text"
          visualization_settings = {}
          dataset_query          = {}
          archived               = false
        },
        text                  = "# ❗️ Some catchy title\n\nIsn't this great?"
        "dashcard.background" = false
      }
      parameter_mappings = []
    }
  ])
}
