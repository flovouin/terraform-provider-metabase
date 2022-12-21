resource "metabase_collection" "business_reports" {
  name        = "📈 Business reports"
  color       = "#32a852"
  description = "Contains reports accessible to business stakeholders."
}

resource "metabase_collection" "marketing_reports" {
  name        = "💸 Marketing reports"
  color       = "#4287f5"
  description = "All about marketing and how it's performing."
  parent_id   = metabase_collection.business_reports.id
}
