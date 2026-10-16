package metabase

// This file defines constructors for the union types of the permissions graph, for the values which apply to an entire
// database. Setting such a value marshals a string, which cannot fail.
//
// It also defines how Metabase handles the permissions of a single edge of the graph (a group and a database).

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

// Returns whether an edge returned by the Metabase API defines the `view-data` permission.
func (p PermissionsGraphDatabasePermissions) HasViewData() bool {
	viewDataBytes, err := p.ViewData.MarshalJSON()
	return err == nil && len(viewDataBytes) > 0 && string(viewDataBytes) != "null"
}

// Makes the permissions of a revoked edge. Metabase never removes an edge from the graph, so this is the lowest level of
// permissions an edge can have: `view-data` is set to `unrestricted`, and `create-queries` and `download` are revoked,
// as well as `data-model` and `details` with advanced permissions.
func NewRevokedDatabasePermissions(advancedPermissions bool) PermissionsGraphDatabasePermissions {
	none := NewDatabaseAccessSchemas(PermissionsGraphDatabaseAccessSchemas0None)
	createQueriesNo := NewCreateQueries(PermissionsGraphDatabasePermissionsCreateQueries0No)

	p := PermissionsGraphDatabasePermissions{
		ViewData:      NewViewData(PermissionsGraphDatabasePermissionsViewData0Unrestricted),
		CreateQueries: &createQueriesNo,
		Download:      &PermissionsGraphDatabaseAccess{Schemas: &none},
	}

	if advancedPermissions {
		no := PermissionsGraphDatabasePermissionsDetailsNo
		p.DataModel = &PermissionsGraphDatabaseAccess{Schemas: &none}
		p.Details = &no
	}

	return p
}

// Returns whether an edge returned by the Metabase API has the permissions of a revoked edge (see
// `NewRevokedDatabasePermissions`). Metabase omits revoked permissions from its responses, e.g. a revoked edge is
// returned as `{"view-data": "unrestricted"}` on the free edition.
func (p PermissionsGraphDatabasePermissions) IsRevoked() bool {
	if v, err := p.ViewData.AsPermissionsGraphDatabasePermissionsViewData0(); err != nil || v != PermissionsGraphDatabasePermissionsViewData0Unrestricted {
		return false
	}

	if p.CreateQueries != nil {
		if v, err := p.CreateQueries.AsPermissionsGraphDatabasePermissionsCreateQueries0(); err != nil || v != PermissionsGraphDatabasePermissionsCreateQueries0No {
			return false
		}
	}

	isNone := func(a *PermissionsGraphDatabaseAccess) bool {
		if a == nil || a.Schemas == nil {
			return true
		}
		v, err := a.Schemas.AsPermissionsGraphDatabaseAccessSchemas0()
		return err == nil && v == PermissionsGraphDatabaseAccessSchemas0None
	}
	if !isNone(p.Download) || !isNone(p.DataModel) {
		return false
	}

	return p.Details == nil || *p.Details == PermissionsGraphDatabasePermissionsDetailsNo
}
