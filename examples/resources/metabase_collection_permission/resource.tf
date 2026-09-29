resource "metabase_collection" "business_reports" {
  name        = "📈 Business reports"
  color       = "#32a852"
  description = "Contains reports accessible to business stakeholders."
}

resource "metabase_permissions_group" "business_stakeholders" {
  name = "👔 Business Stakeholders"
}

# Only the permission of the Business Stakeholders group on this collection is managed. The permissions of other groups
# and collections are left untouched.
resource "metabase_collection_permission" "business_stakeholders_reports" {
  group      = metabase_permissions_group.business_stakeholders.id
  collection = metabase_collection.business_reports.id
  permission = "read"
}

# The root collection can be referenced using `root`.
resource "metabase_collection_permission" "business_stakeholders_root" {
  group      = metabase_permissions_group.business_stakeholders.id
  collection = "root"
  permission = "read"
}
