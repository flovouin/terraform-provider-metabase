package metabase

// This file ensures the Metabase responses conform to the `MetabaseResponse` interface, for convenience when processing
// them during Terraform operations.
type MetabaseResponse interface {
	StatusCode() int
	BodyString() string
}

func (r *GetCollectionPermissionsGraphResponse) BodyString() string {
	return string(r.Body)
}

func (r *ReplaceCollectionPermissionsGraphResponse) BodyString() string {
	return string(r.Body)
}

func (r *CreateCollectionResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetCollectionResponse) BodyString() string {
	return string(r.Body)
}

func (r *UpdateCollectionResponse) BodyString() string {
	return string(r.Body)
}

func (r *CreateDatabaseResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetDatabaseResponse) BodyString() string {
	return string(r.Body)
}

func (r *UpdateDatabaseResponse) BodyString() string {
	return string(r.Body)
}

func (r *DeleteDatabaseResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetPermissionsGraphResponse) BodyString() string {
	return string(r.Body)
}

func (r *ReplacePermissionsGraphResponse) BodyString() string {
	return string(r.Body)
}

func (r *CreatePermissionsGroupResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetPermissionsGroupResponse) BodyString() string {
	return string(r.Body)
}

func (r *UpdatePermissionsGroupResponse) BodyString() string {
	return string(r.Body)
}

func (r *DeletePermissionsGroupResponse) BodyString() string {
	return string(r.Body)
}

func (r *CreateSessionResponse) BodyString() string {
	return string(r.Body)
}

func (r *ListTablesResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetTableMetadataResponse) BodyString() string {
	return string(r.Body)
}
