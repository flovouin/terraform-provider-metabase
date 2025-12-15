# Read the current collection permissions graph.
# This is useful when managing permissions across multiple Terraform workspaces.
data "metabase_collection_graph" "current" {}

# You can specify groups to ignore when reading the graph.
# By default, the Administrators group (ID 2) is ignored.
data "metabase_collection_graph" "with_ignored_groups" {
  ignored_groups = [2]
}

output "revision" {
  value = data.metabase_collection_graph.current.revision
}

output "permissions" {
  value = data.metabase_collection_graph.current.permissions
}
