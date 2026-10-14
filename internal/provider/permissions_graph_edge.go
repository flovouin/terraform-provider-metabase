package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// The model for a single edge in the permissions graph.
type DatabasePermissions struct {
	Group         types.Int64  `tfsdk:"group"`          // The ID of the permissions group to which the permission applies.
	Database      types.Int64  `tfsdk:"database"`       // The ID of the database to which the permission applies.
	ViewData      types.String `tfsdk:"view_data"`      // View data access permission.
	CreateQueries types.String `tfsdk:"create_queries"` // Create queries access permission.
	Download      types.Object `tfsdk:"download"`       // Download-related permission (only available with advanced permissions).
	DataModel     types.Object `tfsdk:"data_model"`     // Data-model-related permission (only available with advanced permissions).
	Details       types.String `tfsdk:"details"`        // Details permission (only available with advanced permissions).
}

// The object type definition for the `DatabasePermissions` model.
var databasePermissionsObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"group":          types.Int64Type,
		"database":       types.Int64Type,
		"view_data":      types.StringType,
		"create_queries": types.StringType,
		"download":       accessPermissionsObjectType,
		"data_model":     accessPermissionsObjectType,
		"details":        types.StringType,
	},
}

// The model for a single permission setting in an edge of the graph.
type AccessPermissions struct {
	Schemas types.String `tfsdk:"schemas"` // Schemas permissions.
}

// The object type definition for the `AccessPermissions` model.
var accessPermissionsObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"schemas": types.StringType,
	},
}

// Makes a `AccessPermissions` Terraform object from a Metabase API value.
// A nil input will be returned as a null object.
func makeAccessPermissionsFromDatabaseAccess(ctx context.Context, da *metabase.PermissionsGraphDatabaseAccess) (*types.Object, diag.Diagnostics) {
	if da == nil {
		nullObject := types.ObjectNull(accessPermissionsObjectType.AttrTypes)
		return &nullObject, diag.Diagnostics{}
	}

	var diags diag.Diagnostics
	schemas, err := da.Schemas.AsPermissionsGraphDatabaseAccessSchemas0()
	if err != nil {
		diags.AddError("Unexpected permissions value. This could be caused by using granular permissions (unsupported). Remove granular permissions and try again", err.Error())
		return nil, diags
	}

	obj, diags := types.ObjectValueFrom(ctx, accessPermissionsObjectType.AttrTypes, AccessPermissions{
		Schemas: stringValueOrNull(&schemas),
	})
	if diags.HasError() {
		return nil, diags
	}

	return &obj, diags
}

// Makes a single `DatabasePermissions` Terraform object from a Metabase API's response.
func makePermissionsObjectFromDatabasePermissions(ctx context.Context, groupId int, dbId int, p metabase.PermissionsGraphDatabasePermissions, existing *DatabasePermissions) (*types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	downloadAccess, accessDiags := makeAccessPermissionsFromDatabaseAccess(ctx, p.Download)
	diags.Append(accessDiags...)
	if diags.HasError() {
		return nil, diags
	}

	dataModelAccess, accessDiags := makeAccessPermissionsFromDatabaseAccess(ctx, p.DataModel)
	diags.Append(accessDiags...)
	if diags.HasError() {
		return nil, diags
	}

	var existingViewData *string
	var existingViewDataIsJson bool
	if existing != nil {
		existingViewData = existing.ViewData.ValueStringPointer()

		if existingViewData != nil {
			var viewDataObject map[string]any
			err := json.Unmarshal([]byte(*existingViewData), &viewDataObject)
			existingViewDataIsJson = err == nil
		}
	}

	var viewData string
	if viewDataString, err := p.ViewData.AsPermissionsGraphDatabasePermissionsViewData0(); err == nil {
		if existingViewData != nil && existingViewDataIsJson {
			// If the types between the existing model and the API response differ, assuming they have the same meaning.
			// For example, in some cases, sending { "public": "unrestricted" } will be returned as "unrestricted" by the API.
			viewData = *existingViewData
		} else {
			viewData = string(viewDataString)
		}
	} else {
		viewDataObject, err := p.ViewData.AsPermissionsGraphDatabasePermissionsViewData1()
		if err != nil {
			diags.AddError("Unexpected permissions value.", err.Error())
			return nil, diags
		}

		viewDataBytes, err := json.Marshal(viewDataObject)
		if err != nil {
			diags.AddError("Unexpected error marshaling view data permissions to JSON.", err.Error())
			return nil, diags
		}

		// Same reasoning as for the string case above: Metabase may reshape an object-form response it considers
		// equivalent (e.g. pruning tables or collapsing uniform schemas), so when the existing model already carries
		// an object form, keep that serialization to avoid a spurious, perpetual diff. Only fall back to the response
		// when there is no existing object to preserve (e.g. import).
		if existingViewData != nil && existingViewDataIsJson {
			viewData = *existingViewData
		} else {
			viewData = string(viewDataBytes)
		}
	}

	var existingCreateQueries *string
	var existingCreateQueriesIsJson bool
	if existing != nil {
		existingCreateQueries = existing.CreateQueries.ValueStringPointer()

		if existingCreateQueries != nil {
			var createQueriesObject map[string]any
			err := json.Unmarshal([]byte(*existingCreateQueries), &createQueriesObject)
			existingCreateQueriesIsJson = err == nil
		}
	}

	createQueries := string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No)
	if p.CreateQueries != nil {
		if createQueriesString, err := p.CreateQueries.AsPermissionsGraphDatabasePermissionsCreateQueries0(); err == nil {
			// Same reasoning as for view-data: if the existing model carries an object form but Metabase has
			// collapsed it to a scalar, keep the existing serialized form to avoid spurious diffs.
			if existingCreateQueries != nil && existingCreateQueriesIsJson {
				createQueries = *existingCreateQueries
			} else {
				createQueries = string(createQueriesString)
			}
		} else {
			createQueriesObject, err := p.CreateQueries.AsPermissionsGraphDatabasePermissionsCreateQueries1()
			if err != nil {
				diags.AddError("Unexpected permissions value.", err.Error())
				return nil, diags
			}

			createQueriesBytes, err := json.Marshal(createQueriesObject)
			if err != nil {
				diags.AddError("Unexpected error marshaling create-queries permissions to JSON.", err.Error())
				return nil, diags
			}

			// Metabase reshapes object-form create-queries it considers equivalent: it prunes "no" tables and
			// collapses uniform schemas. When the existing model already carries an object form, keep that
			// serialization so the (possibly reshaped) response does not produce a spurious, perpetual diff. The
			// existing object is also the only form guaranteed to be accepted on a subsequent write, since the
			// reshaped response can be a schema-level scalar that Metabase rejects. Only fall back to the response
			// when there is no existing object to preserve (e.g. import).
			if existingCreateQueries != nil && existingCreateQueriesIsJson {
				createQueries = *existingCreateQueries
			} else {
				createQueries = string(createQueriesBytes)
			}
		}
	}

	permissionsObject, objectDiags := types.ObjectValueFrom(ctx, databasePermissionsObjectType.AttrTypes, DatabasePermissions{
		Group:         types.Int64Value(int64(groupId)),
		Database:      types.Int64Value(int64(dbId)),
		ViewData:      types.StringValue(viewData),
		CreateQueries: types.StringValue(createQueries),
		Download:      *downloadAccess,
		DataModel:     *dataModelAccess,
		Details:       stringValueOrNull(p.Details),
	})
	diags.Append(objectDiags...)
	if diags.HasError() {
		return nil, diags
	}

	return &permissionsObject, diags
}

// Returns whether an edge returned by the Metabase API defines the `view-data` permission.
func hasViewDataPermissions(p metabase.PermissionsGraphDatabasePermissions) bool {
	viewDataBytes, err := p.ViewData.MarshalJSON()
	return err == nil && len(viewDataBytes) > 0 && string(viewDataBytes) != "null"
}

// Makes the permissions of a revoked edge. Metabase never removes an edge from the graph, so this is the lowest level of
// permissions an edge can have: `view-data` is set to `unrestricted`, and `create-queries` and `download` are revoked,
// as well as `data-model` and `details` with advanced permissions.
func makeRevokedDatabasePermissions(advancedPermissions bool) metabase.PermissionsGraphDatabasePermissions {
	none := metabase.NewDatabaseAccessSchemas(metabase.PermissionsGraphDatabaseAccessSchemas0None)
	createQueriesNo := metabase.NewCreateQueries(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No)

	p := metabase.PermissionsGraphDatabasePermissions{
		ViewData:      metabase.NewViewData(metabase.PermissionsGraphDatabasePermissionsViewData0Unrestricted),
		CreateQueries: &createQueriesNo,
		Download:      &metabase.PermissionsGraphDatabaseAccess{Schemas: &none},
	}

	if advancedPermissions {
		no := metabase.PermissionsGraphDatabasePermissionsDetailsNo
		p.DataModel = &metabase.PermissionsGraphDatabaseAccess{Schemas: &none}
		p.Details = &no
	}

	return p
}

// Returns whether an edge returned by the Metabase API has the permissions of a revoked edge (see
// `makeRevokedDatabasePermissions`). Metabase omits revoked permissions from its responses, e.g. a revoked edge is
// returned as `{"view-data": "unrestricted"}` on the free edition.
func isRevokedDatabasePermissions(p metabase.PermissionsGraphDatabasePermissions) bool {
	if v, err := p.ViewData.AsPermissionsGraphDatabasePermissionsViewData0(); err != nil || v != metabase.PermissionsGraphDatabasePermissionsViewData0Unrestricted {
		return false
	}

	if p.CreateQueries != nil {
		if v, err := p.CreateQueries.AsPermissionsGraphDatabasePermissionsCreateQueries0(); err != nil || v != metabase.PermissionsGraphDatabasePermissionsCreateQueries0No {
			return false
		}
	}

	isNone := func(a *metabase.PermissionsGraphDatabaseAccess) bool {
		if a == nil || a.Schemas == nil {
			return true
		}
		v, err := a.Schemas.AsPermissionsGraphDatabaseAccessSchemas0()
		return err == nil && v == metabase.PermissionsGraphDatabaseAccessSchemas0None
	}
	if !isNone(p.Download) || !isNone(p.DataModel) {
		return false
	}

	return p.Details == nil || *p.Details == metabase.PermissionsGraphDatabasePermissionsDetailsNo
}

// Makes a Metabase API `PermissionsGraphDatabaseAccess` struct from a Terraform model object.
// `setIfNull` can be used to set the default values (forbidding any access) to permissions.
// This is useful when removing permissions for example.
func makeDatasetAccessFromModel(ctx context.Context, apObj types.Object, setIfNull bool) (*metabase.PermissionsGraphDatabaseAccess, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !setIfNull && apObj.IsNull() {
		return nil, diags
	}

	var schemas metabase.PermissionsGraphDatabaseAccess_Schemas
	err := schemas.FromPermissionsGraphDatabaseAccessSchemas0(metabase.PermissionsGraphDatabaseAccessSchemas0None)
	if err != nil {
		diags.AddError("Unexpected error setting schemas to none value", err.Error())
		return nil, diags
	}

	if !apObj.IsNull() {
		var ap AccessPermissions
		asDiags := apObj.As(ctx, &ap, basetypes.ObjectAsOptions{})
		diags.Append(asDiags...)
		if diags.HasError() {
			return nil, diags
		}

		if !ap.Schemas.IsNull() {
			err := schemas.FromPermissionsGraphDatabaseAccessSchemas0(metabase.PermissionsGraphDatabaseAccessSchemas0(ap.Schemas.ValueString()))
			if err != nil {
				diags.AddError("Unexpected error setting permissions value", err.Error())
				return nil, diags
			}
		}
	}

	return &metabase.PermissionsGraphDatabaseAccess{
		Schemas: &schemas,
	}, diags
}

// Makes the Metabase API permissions for a single edge of the graph (a group and a database) from its Terraform model.
// `advancedPermissions` sets the advanced permissions that are not specified to their default values (forbidding any
// access).
func makeDatabasePermissionsFromModel(ctx context.Context, p DatabasePermissions, advancedPermissions bool) (*metabase.PermissionsGraphDatabasePermissions, diag.Diagnostics) {
	var diags diag.Diagnostics

	viewDataString := p.ViewData.ValueString()
	var viewData metabase.PermissionsGraphDatabasePermissions_ViewData
	var viewDataObject map[string]any
	// Tries to parse the string as JSON.
	if err := json.Unmarshal([]byte(viewDataString), &viewDataObject); err == nil {
		viewData.FromPermissionsGraphDatabasePermissionsViewData1(
			metabase.PermissionsGraphDatabasePermissionsViewData1(viewDataObject),
		)
	} else {
		viewData.FromPermissionsGraphDatabasePermissionsViewData0(
			metabase.PermissionsGraphDatabasePermissionsViewData0(viewDataString),
		)
	}

	var createQueries *metabase.PermissionsGraphDatabasePermissions_CreateQueries
	if !p.CreateQueries.IsNull() && !p.CreateQueries.IsUnknown() {
		createQueriesString := p.CreateQueries.ValueString()
		var createQueriesObject map[string]any
		var unionValue metabase.PermissionsGraphDatabasePermissions_CreateQueries
		// Tries to parse the string as JSON.
		if err := json.Unmarshal([]byte(createQueriesString), &createQueriesObject); err == nil {
			unionValue.FromPermissionsGraphDatabasePermissionsCreateQueries1(
				metabase.PermissionsGraphDatabasePermissionsCreateQueries1(createQueriesObject),
			)
		} else {
			unionValue.FromPermissionsGraphDatabasePermissionsCreateQueries0(
				metabase.PermissionsGraphDatabasePermissionsCreateQueries0(createQueriesString),
			)
		}
		createQueries = &unionValue
	} else {
		var unionValue metabase.PermissionsGraphDatabasePermissions_CreateQueries
		unionValue.FromPermissionsGraphDatabasePermissionsCreateQueries0(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No)
		createQueries = &unionValue
	}

	download, accessDiags := makeDatasetAccessFromModel(ctx, p.Download, advancedPermissions)
	diags.Append(accessDiags...)
	if diags.HasError() {
		return nil, diags
	}

	dataModel, accessDiags := makeDatasetAccessFromModel(ctx, p.DataModel, advancedPermissions)
	diags.Append(accessDiags...)
	if diags.HasError() {
		return nil, diags
	}

	details := valueApproximateStringOrNull[metabase.PermissionsGraphDatabasePermissionsDetails](p.Details)
	if details == nil && advancedPermissions {
		no := metabase.PermissionsGraphDatabasePermissionsDetailsNo
		details = &no
	}

	return &metabase.PermissionsGraphDatabasePermissions{
		ViewData:      viewData,
		CreateQueries: createQueries,
		Download:      download,
		DataModel:     dataModel,
		Details:       details,
	}, diags
}

// Returns whether the given database exists.
func databaseExists(ctx context.Context, client *metabase.ClientWithResponses, databaseId int64) (bool, diag.Diagnostics) {
	getResp, err := client.GetDatabaseWithResponse(ctx, int(databaseId))

	diags := checkMetabaseResponse(getResp, err, []int{200, 404}, "get database")
	if diags.HasError() {
		return false, diags
	}

	return getResp.StatusCode() == 200, diags
}

// Returns an error if the given database does not exist.
func checkDatabaseExists(ctx context.Context, client *metabase.ClientWithResponses, databaseId int64) diag.Diagnostics {
	exists, diags := databaseExists(ctx, client, databaseId)
	if !diags.HasError() && !exists {
		diags.AddAttributeError(
			path.Root("database"),
			"Database not found.",
			fmt.Sprintf("The database %d does not exist.", databaseId),
		)
	}

	return diags
}
