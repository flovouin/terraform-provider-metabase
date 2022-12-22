package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithSchema = &PermissionsGraphResource{}
var _ resource.ResourceWithImportState = &PermissionsGraphResource{}

// Creates a new permissions graph resource.
func NewPermissionsGraphResource() resource.Resource {
	return &PermissionsGraphResource{
		MetabaseBaseResource{name: "permissions_graph"},
	}
}

// A resource handling the Metabase permissions graph.
type PermissionsGraphResource struct {
	MetabaseBaseResource
}

// The Terraform model for the graph.
// Permissions are stored as a list of edges rather than a map like in the API.
type PermissionsGraphResourceModel struct {
	Revision            types.Int64 `tfsdk:"revision"`             // The revision number for the graph, set by Metabase.
	AdvancedPermissions types.Bool  `tfsdk:"advanced_permissions"` // Whether advanced permissions should be set. This is only available to paid versions of Metabase.
	IgnoredGroups       types.Set   `tfsdk:"ignored_groups"`       // The list of groups that should be ignored when updating permissions.
	Permissions         types.Set   `tfsdk:"permissions"`          // The list of permissions (edges) in the graph.
}

// The model for a single edge in the permissions graph.
type DatabasePermissions struct {
	Group     types.Int64  `tfsdk:"group"`      // The ID of the permissions group to which the permission applies.
	Database  types.Int64  `tfsdk:"database"`   // The ID of the database to which the permission applies.
	Data      types.Object `tfsdk:"data"`       // Data-related permission.
	Download  types.Object `tfsdk:"download"`   // Download-related permission (only available with advanced permissions).
	DataModel types.Object `tfsdk:"data_model"` // Data-model-related permission (only available with advanced permissions).
	Details   types.String `tfsdk:"details"`    // Details permission (only available with advanced permissions).
}

// The object type definition for the `DatabasePermissions` model.
var databasePermissionsObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"group":      types.Int64Type,
		"database":   types.Int64Type,
		"data":       accessPermissionsObjectType,
		"download":   accessPermissionsObjectType,
		"data_model": accessPermissionsObjectType,
		"details":    types.StringType,
	},
}

// The model for a single permission setting in an edge of the graph.
type AccessPermissions struct {
	Native  types.String `tfsdk:"native"`  // Native-access (SQL) permissions.
	Schemas types.String `tfsdk:"schemas"` // Schemas permissions.
}

// The schema for the `AccessPermissions` model.
var accessPermissionAttributes = map[string]schema.Attribute{
	"native": schema.StringAttribute{
		MarkdownDescription: "The permission for native SQL querying",
		Optional:            true,
	},
	"schemas": schema.StringAttribute{
		MarkdownDescription: "The permission to access data through the Metabase interface",
		Optional:            true,
	},
}

// The object type definition for the `AccessPermissions` model.
var accessPermissionsObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"native":  types.StringType,
		"schemas": types.StringType,
	},
}

func (r *PermissionsGraphResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `The graph of permissions between permissions groups and databases.

Metabase exposes a single resource to define all permissions related to databases. This means a single permissions graph resource should be defined in the entire Terraform configuration. However this is not the same as the collection graph, and the two can be combined to grant permissions.

The permissions graph cannot be created or deleted. Trying to create it will result in an error. It should be imported instead. Trying to delete the resource will succeed with no impact on Metabase (it is a no-op).

Permissions for the Administrators group cannot be changed. To avoid issues during the update, all permissions for the Administrators group are ignored by default. This behavior can be changed using the ignored groups attribute.`,

		Attributes: map[string]schema.Attribute{
			"revision": schema.Int64Attribute{
				MarkdownDescription: "The revision number for the graph.",
				Computed:            true,
			},
			"advanced_permissions": schema.BoolAttribute{
				MarkdownDescription: "Whether advanced permissions should be set even when not explicitly specified.",
				Required:            true,
			},
			"ignored_groups": schema.SetAttribute{
				ElementType:         types.Int64Type,
				MarkdownDescription: "The list of group IDs that should be ignored when reading and updating permissions. By default, this contains the Administrators group (`[2]`).",
				Optional:            true,
			},
			"permissions": schema.SetNestedAttribute{
				MarkdownDescription: "A list of permissions for a given group and database. A (group, database) pair should appear only once in the list.",
				Required:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"group": schema.Int64Attribute{
							MarkdownDescription: "The ID of the group to which the permission applies.",
							Required:            true,
						},
						"database": schema.Int64Attribute{
							MarkdownDescription: "The ID of the database to which the permission applies.",
							Required:            true,
						},
						"data": schema.SingleNestedAttribute{
							MarkdownDescription: "The permission definition for data access.",
							Optional:            true,
							Attributes:          accessPermissionAttributes,
						},
						"download": schema.SingleNestedAttribute{
							MarkdownDescription: "The permission definition for downloading data.",
							Optional:            true,
							Attributes:          accessPermissionAttributes,
						},
						"data_model": schema.SingleNestedAttribute{
							MarkdownDescription: "The permission definition for accessing the data model.",
							Optional:            true,
							Attributes:          accessPermissionAttributes,
						},
						"details": schema.StringAttribute{
							MarkdownDescription: "The permission definition for accessing details.",
							Optional:            true,
						},
					},
				},
			},
		},
	}
}

// Makes a `AccessPermissions` Terraform object from a Metabase API value.
// A nil input will be returned as a null object.
func makeAccessPermissionsFromDatabaseAccess(ctx context.Context, da *metabase.PermissionsGraphDatabaseAccess) (*types.Object, diag.Diagnostics) {
	if da == nil {
		nullObject := types.ObjectNull(accessPermissionsObjectType.AttrTypes)
		return &nullObject, diag.Diagnostics{}
	}

	obj, diags := types.ObjectValueFrom(ctx, accessPermissionsObjectType.AttrTypes, AccessPermissions{
		Native:  stringValueOrNull(da.Native),
		Schemas: stringValueOrNull(da.Schemas),
	})
	if diags.HasError() {
		return nil, diags
	}

	return &obj, diags
}

// Makes a single `DatabasePermissions` Terraform object from a Metabase API's response.
func makePermissionsObjectFromDatabasePermissions(ctx context.Context, groupId string, dbId string, p metabase.PermissionsGraphDatabasePermissions) (*types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	groupIdInt, err := strconv.Atoi(groupId)
	if err != nil {
		diags.AddError("Could not convert the group ID to an integer.", err.Error())
		return nil, diags
	}

	dbIdInt, err := strconv.Atoi(dbId)
	if err != nil {
		diags.AddError("Could not convert the database ID to an integer.", err.Error())
		return nil, diags
	}

	dataAccess, accessDiags := makeAccessPermissionsFromDatabaseAccess(ctx, p.Data)
	diags.Append(accessDiags...)
	if diags.HasError() {
		return nil, diags
	}

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

	permissionsObject, objectDiags := types.ObjectValueFrom(ctx, databasePermissionsObjectType.AttrTypes, DatabasePermissions{
		Group:     types.Int64Value(int64(groupIdInt)),
		Database:  types.Int64Value(int64(dbIdInt)),
		Data:      *dataAccess,
		Download:  *downloadAccess,
		DataModel: *dataModelAccess,
		Details:   stringValueOrNull(p.Details),
	})
	diags.Append(objectDiags...)
	if diags.HasError() {
		return nil, diags
	}

	return &permissionsObject, diags
}

// Updates the given `PermissionsGraphResourceModel` from the `PermissionsGraph` returned by the Metabase API.
func updateModelFromPermissionsGraph(ctx context.Context, g metabase.PermissionsGraph, data *PermissionsGraphResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Revision = types.Int64Value(int64(g.Revision))

	ignoredGroups, groupsDiags := getIgnoredPermissionsGroups(ctx, data.IgnoredGroups)
	diags.Append(groupsDiags...)
	if diags.HasError() {
		return diags
	}

	permissionsList := make([]attr.Value, 0, len(data.Permissions.Elements()))
	for groupId, dbPermissionsMap := range g.Groups {
		// Permissions for ignored groups are not stored in the state for clarity.
		if ignoredGroups[groupId] {
			continue
		}

		for dbId, dbPermissions := range dbPermissionsMap {
			permissionsObject, objDiags := makePermissionsObjectFromDatabasePermissions(ctx, groupId, dbId, dbPermissions)
			diags.Append(objDiags...)
			if diags.HasError() {
				return diags
			}

			permissionsList = append(permissionsList, *permissionsObject)
		}
	}

	permissionsSet, setDiags := types.SetValue(databasePermissionsObjectType, permissionsList)
	diags.Append(setDiags...)
	if diags.HasError() {
		return diags
	}

	data.Permissions = permissionsSet

	return diags
}

// Makes a Metabase API `PermissionsGraphDatabaseAccess` struct from a Terraform model object.
// `setIfNull` can be used to set the default values (forbidding any access) to permissions.
// This is useful when removing permissions for example.
// `setNative` determines whether the `native` attribute should be set in the access object.
// This is useful because the "data model" permission does not support it.
func makeDatasetAccessFromModel(ctx context.Context, apObj types.Object, setIfNull bool, setNative bool) (*metabase.PermissionsGraphDatabaseAccess, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !setIfNull && apObj.IsNull() {
		return nil, diags
	}

	// Default values ("none") forbid any access.
	var native *metabase.PermissionsGraphDatabaseAccessNative
	schemas := metabase.PermissionsGraphDatabaseAccessSchemasNone
	if setNative {
		none := metabase.PermissionsGraphDatabaseAccessNativeNone
		native = &none
	}

	if !apObj.IsNull() {
		var ap AccessPermissions
		asDiags := apObj.As(ctx, &ap, types.ObjectAsOptions{})
		diags.Append(asDiags...)
		if diags.HasError() {
			return nil, diags
		}

		if setNative && !ap.Native.IsNull() {
			nativeValue := metabase.PermissionsGraphDatabaseAccessNative(ap.Native.ValueString())
			native = &nativeValue
		}

		if !ap.Schemas.IsNull() {
			schemas = metabase.PermissionsGraphDatabaseAccessSchemas(ap.Schemas.ValueString())
		}
	}

	return &metabase.PermissionsGraphDatabaseAccess{
		Native:  native,
		Schemas: &schemas,
	}, diags
}

// Makes the entire permissions graph from the Terraform model.
// Passing the current state allows comparing the plan to an existing set of permissions. This allows explicitly
// removing permissions by sending "none" values to the Metabase API.
// The Metabase API automatically removes "none" values and does not return them.
func makePermissionsGraphFromModel(ctx context.Context, data PermissionsGraphResourceModel, state *PermissionsGraphResourceModel) (*metabase.PermissionsGraph, diag.Diagnostics) {
	var diags diag.Diagnostics

	advancedPermissions := data.AdvancedPermissions.ValueBool()
	revision := int(data.Revision.ValueInt64())

	permissions := make([]DatabasePermissions, 0, len(data.Permissions.Elements()))
	diags.Append(data.Permissions.ElementsAs(ctx, &permissions, false)...)
	if diags.HasError() {
		return nil, diags
	}

	groups := make(map[string]metabase.PermissionsGraphDatabasePermissionsMap, len(permissions))
	for _, p := range permissions {
		if p.Group.IsNull() {
			diags.AddError("Unexpected null group in permission.", "")
			return nil, diags
		}
		if p.Database.IsNull() {
			diags.AddError("Unexpected null database in permission.", "")
			return nil, diags
		}
		groupId := strconv.FormatInt(p.Group.ValueInt64(), 10)
		databaseId := strconv.FormatInt(p.Database.ValueInt64(), 10)

		dbPermMap, ok := groups[groupId]
		if !ok {
			dbPermMap = make(metabase.PermissionsGraphDatabasePermissionsMap)
			groups[groupId] = dbPermMap
		}

		_, permExists := dbPermMap[databaseId]
		if permExists {
			diags.AddError("Found duplicate permissions definition.", fmt.Sprintf("Group ID: %s, Database ID: %s.", groupId, databaseId))
			return nil, diags
		}

		data, accessDiags := makeDatasetAccessFromModel(ctx, p.Data, true, true)
		diags.Append(accessDiags...)
		if diags.HasError() {
			return nil, diags
		}

		download, accessDiags := makeDatasetAccessFromModel(ctx, p.Download, advancedPermissions, true)
		diags.Append(accessDiags...)
		if diags.HasError() {
			return nil, diags
		}

		dataModel, accessDiags := makeDatasetAccessFromModel(ctx, p.DataModel, advancedPermissions, false)
		diags.Append(accessDiags...)
		if diags.HasError() {
			return nil, diags
		}

		details := valueApproximateStringOrNull[metabase.PermissionsGraphDatabasePermissionsDetails](p.Details)
		if details == nil && advancedPermissions {
			no := metabase.No
			details = &no
		}

		dbPermMap[databaseId] = metabase.PermissionsGraphDatabasePermissions{
			Data:      data,
			Download:  download,
			DataModel: dataModel,
			Details:   details,
		}
	}

	// If the state is passed, it is used to detect removed permissions (or permissions added outside of Terraform).
	// Those permissions are explicitly set to "none" in order to delete them.
	if state != nil {
		// When making the request to the Metabase API, the currently known revision number should be passed.
		// It will be increased and returned by Metabase.
		revision = int(state.Revision.ValueInt64())

		statePermissions := make([]DatabasePermissions, 0, len(state.Permissions.Elements()))
		diags.Append(state.Permissions.ElementsAs(ctx, &statePermissions, false)...)
		if diags.HasError() {
			return nil, diags
		}

		for _, p := range statePermissions {
			if p.Group.IsNull() {
				diags.AddError("Unexpected null group in permissions.", "")
				return nil, diags
			}
			if p.Database.IsNull() {
				diags.AddError("Unexpected null database in permissions.", "")
				return nil, diags
			}
			groupId := strconv.FormatInt(p.Group.ValueInt64(), 10)
			databaseId := strconv.FormatInt(p.Database.ValueInt64(), 10)

			dbPermMap, ok := groups[groupId]
			if !ok {
				dbPermMap = make(metabase.PermissionsGraphDatabasePermissionsMap)
				groups[groupId] = dbPermMap
			}

			_, permExists := dbPermMap[databaseId]
			if permExists {
				// The permissions has already been set to a newer (or equal) value using the plan.
				continue
			}

			// If the permission does not exist in the plan but exists in the state, it should be explicitly deleted by
			// creating the permission with "none" values.
			nativeNone := metabase.PermissionsGraphDatabaseAccessNativeNone
			schemasNone := metabase.PermissionsGraphDatabaseAccessSchemasNone
			deletedPermissions := metabase.PermissionsGraphDatabasePermissions{
				Data: &metabase.PermissionsGraphDatabaseAccess{
					Native:  &nativeNone,
					Schemas: &schemasNone,
				},
			}
			if advancedPermissions {
				deletedPermissions.Download = &metabase.PermissionsGraphDatabaseAccess{
					Native:  &nativeNone,
					Schemas: &schemasNone,
				}
				deletedPermissions.DataModel = &metabase.PermissionsGraphDatabaseAccess{
					Schemas: &schemasNone,
				}
				no := metabase.No
				deletedPermissions.Details = &no
			}
			dbPermMap[databaseId] = deletedPermissions
		}
	}

	return &metabase.PermissionsGraph{
		Revision: revision,
		Groups:   groups,
	}, diags
}

func (r *PermissionsGraphResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	resp.Diagnostics.AddError("Creating the permissions graph is not allowed, import it instead.", "")
}

func (r *PermissionsGraphResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *PermissionsGraphResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := r.client.GetPermissionsGraphWithResponse(ctx)

	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200}, "get permissions graph")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromPermissionsGraph(ctx, *getResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PermissionsGraphResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *PermissionsGraphResourceModel
	var state *PermissionsGraphResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := makePermissionsGraphFromModel(ctx, *data, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateResp, err := r.client.ReplacePermissionsGraphWithResponse(ctx, *body)

	resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update permissions graph")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromPermissionsGraph(ctx, *updateResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PermissionsGraphResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Delete operation is not supported for the Metabase permissions graph.", "")
}

func (r *PermissionsGraphResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	revision, err := strconv.Atoi(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to convert revision to an integer.", req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("revision"), revision)...)
}
