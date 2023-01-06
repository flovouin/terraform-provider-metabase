# This **imports** a table using the ID of the parent database, the table name, and the table schema (dataset for
# BigQuery). At least one attribute must be set, but they are all optional.
resource "metabase_table" "table_by_name" {
  db_id  = 2 # Or use `metabase_database.db.id`.
  name   = "table_name"
  schema = "schema"

  # Optional read/write attributes that will be imported from Metabase if not specified.
  description  = "🗃️ Some very important data."
  display_name = "💮 My Table"

  # Each field (column) in this map will have its semantic type updated with the corresponding value.
  # Any field in the table not specified here will be left untouched.
  forced_field_types = {
    column_1 = null            # "No semantic type".
    column_2 = "type/Category" # "Category".
  }
}

# Although less useful, a table can be imported by its ID if it's already known.
resource "metabase_table" "table_by_id" {
  id = 1
}

# The ID of each column can be found using the `fields` attribute, which is a map between column names and field IDs.
output "field_id" {
  value = metabase_table.table_by_name.fields["offer"]
}
