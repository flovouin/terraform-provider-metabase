# This finds a database using its ID.
# At least one attribute must be set, but they are all optional.
data "metabase_database" "my" {
  id = 1
}

# This finds a database using its Name.
data "metabase_database" "my" {
  Name = "my-database"
}
