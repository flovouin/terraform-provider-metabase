# This finds a table using the ID of the parent database, the table name, and the table schema (dataset for BigQuery).
# At least one attribute must be set, but they are all optional.
data "metabase_table" "table_by_name" {
  db_id  = 2 # Or use `metabase_database.db.id`.
  name   = "table_name"
  schema = "schema"
}

# Although less useful, a table can be found by its ID if it's already known.
data "metabase_table" "table_by_id" {
  id = 1
}

output "table_id" {
  value = data.metabase_table.table_by_name.id
}

# The ID of each column can be found using the `fields` attribute, which is a map between column names and field IDs.
output "field_id" {
  value = data.metabase_table.table_by_name.fields["column_name"]
}

output "table_name" {
  value = data.metabase_table.table_by_id.name
}
