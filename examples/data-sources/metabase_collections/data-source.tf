# List all the collections in Metabase.
data "metabase_collections" "all" {}

# The path of a collection is built from the names of its ancestors, which makes it possible to reference a collection
# without knowing its Metabase ID.
output "campaigns_collection_id" {
  value = data.metabase_collections.all.ids_by_path["Marketing/Campaigns"]
}

# Collections can be filtered on their path to apply the same configuration to an entire subtree, for example to grant
# a group access to a collection and all its descendants.
locals {
  marketing_collection_ids = [
    for collection in data.metabase_collections.all.collections :
    collection.id
    if collection.personal_owner_id == null && startswith(collection.path, "Marketing")
  ]
}

output "marketing_collection_ids" {
  value = local.marketing_collection_ids
}
