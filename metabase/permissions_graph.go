package metabase

// This file defines constructors for the union types of the permissions graph, for the values which apply to an entire
// database. Setting such a value marshals a string, which cannot fail.

// Returns a `view-data` permission defined by a single value for the entire database.
func NewViewData(v PermissionsGraphDatabasePermissionsViewData0) PermissionsGraphDatabasePermissions_ViewData {
	var viewData PermissionsGraphDatabasePermissions_ViewData
	_ = viewData.FromPermissionsGraphDatabasePermissionsViewData0(v)
	return viewData
}

// Returns a `create-queries` permission defined by a single value for the entire database.
func NewCreateQueries(v PermissionsGraphDatabasePermissionsCreateQueries0) PermissionsGraphDatabasePermissions_CreateQueries {
	var createQueries PermissionsGraphDatabasePermissions_CreateQueries
	_ = createQueries.FromPermissionsGraphDatabasePermissionsCreateQueries0(v)
	return createQueries
}

// Returns the schemas of an access permission (e.g. `download`) defined by a single value for the entire database.
func NewDatabaseAccessSchemas(v PermissionsGraphDatabaseAccessSchemas0) PermissionsGraphDatabaseAccess_Schemas {
	var schemas PermissionsGraphDatabaseAccess_Schemas
	_ = schemas.FromPermissionsGraphDatabaseAccessSchemas0(v)
	return schemas
}
