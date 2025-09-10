package metabase

// This file ensures the Metabase responses conform to the `MetabaseResponse` interface, for convenience when processing
// them during Terraform operations.
type MetabaseResponse interface {
	StatusCode() int
	BodyString() string
	HasExpectedStatusWithoutExpectedBody() bool
}

func (r *CreateCardResponse) BodyString() string {
	return string(r.Body)
}

func (r *CreateCardResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *GetCardResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetCardResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *UpdateCardResponse) BodyString() string {
	return string(r.Body)
}

func (r *UpdateCardResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *GetCollectionPermissionsGraphResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetCollectionPermissionsGraphResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *ReplaceCollectionPermissionsGraphResponse) BodyString() string {
	return string(r.Body)
}

func (r *ReplaceCollectionPermissionsGraphResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *CreateCollectionResponse) BodyString() string {
	return string(r.Body)
}

func (r *CreateCollectionResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *GetCollectionResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetCollectionResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *UpdateCollectionResponse) BodyString() string {
	return string(r.Body)
}

func (r *UpdateCollectionResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *ListCollectionItemsResponse) BodyString() string {
	return string(r.Body)
}

func (r *ListCollectionItemsResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *CreateDashboardResponse) BodyString() string {
	return string(r.Body)
}

func (r *CreateDashboardResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *GetDashboardResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetDashboardResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *UpdateDashboardResponse) BodyString() string {
	return string(r.Body)
}

func (r *UpdateDashboardResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *DeleteDashboardResponse) BodyString() string {
	return string(r.Body)
}

func (r *DeleteDashboardResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return false
}

func (r *CreateDatabaseResponse) BodyString() string {
	return string(r.Body)
}

func (r *CreateDatabaseResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *GetDatabaseResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetDatabaseResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *UpdateDatabaseResponse) BodyString() string {
	return string(r.Body)
}

func (r *UpdateDatabaseResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *DeleteDatabaseResponse) BodyString() string {
	return string(r.Body)
}

func (r *DeleteDatabaseResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return false
}

func (r *GetPermissionsGraphResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetPermissionsGraphResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *ReplacePermissionsGraphResponse) BodyString() string {
	return string(r.Body)
}

func (r *ReplacePermissionsGraphResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *CreatePermissionsGroupResponse) BodyString() string {
	return string(r.Body)
}

func (r *CreatePermissionsGroupResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *GetPermissionsGroupResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetPermissionsGroupResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *UpdatePermissionsGroupResponse) BodyString() string {
	return string(r.Body)
}

func (r *UpdatePermissionsGroupResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *DeletePermissionsGroupResponse) BodyString() string {
	return string(r.Body)
}

func (r *DeletePermissionsGroupResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return false
}

func (r *CreateSessionResponse) BodyString() string {
	return string(r.Body)
}

func (r *CreateSessionResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *ListTablesResponse) BodyString() string {
	return string(r.Body)
}

func (r *ListTablesResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *GetTableMetadataResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetTableMetadataResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *UpdateTableResponse) BodyString() string {
	return string(r.Body)
}

func (r *UpdateTableResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *GetFieldResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetFieldResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *UpdateFieldResponse) BodyString() string {
	return string(r.Body)
}

func (r *UpdateFieldResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *GetContentTranslationCsvResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetContentTranslationCsvResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && len(r.Body) == 0
}

func (r *GetContentTranslationDictionaryResponse) BodyString() string {
	return string(r.Body)
}

func (r *GetContentTranslationDictionaryResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}

func (r *UploadContentTranslationDictionaryResponse) BodyString() string {
	return string(r.Body)
}

func (r *UploadContentTranslationDictionaryResponse) HasExpectedStatusWithoutExpectedBody() bool {
	return r.StatusCode() == 200 && r.JSON200 == nil
}
