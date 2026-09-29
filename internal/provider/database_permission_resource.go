package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &DatabasePermissionResource{}
var _ resource.ResourceWithValidateConfig = &DatabasePermissionResource{}

// Creates a new database permission resource.
func NewDatabasePermissionResource() resource.Resource {
	return &DatabasePermissionResource{
		MetabaseBaseResource{name: "database_permission"},
	}
}

// A resource handling the permissions of a single group on a single database, i.e. a single edge of the permissions
// graph.
type DatabasePermissionResource struct {
	MetabaseBaseResource
}

// The Terraform model for a single edge of the permissions graph.
// Apart from the ID, the attributes are the same as an edge in the `metabase_permissions_graph` resource.
type DatabasePermissionResourceModel struct {
	Id            types.String `tfsdk:"id"`             // The ID of the edge, as `<group>/<database>`.
	Group         types.Int64  `tfsdk:"group"`          // The ID of the permissions group to which the permission applies.
	Database      types.Int64  `tfsdk:"database"`       // The ID of the database to which the permission applies.
	ViewData      types.String `tfsdk:"view_data"`      // View data access permission.
	CreateQueries types.String `tfsdk:"create_queries"` // Create queries access permission.
	Download      types.Object `tfsdk:"download"`       // Download-related permission.
	DataModel     types.Object `tfsdk:"data_model"`     // Data-model-related permission.
	Details       types.String `tfsdk:"details"`        // Details permission.
}

func (r *DatabasePermissionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `The permissions of a single permissions group on a single database.

Unlike the ` + "`metabase_permissions_graph`" + ` resource, which manages the entire permissions graph and resets the permissions it does not define, this resource only manages a single (group, database) edge of the graph. All other permissions are left untouched, whether they are managed by other ` + "`metabase_database_permission`" + ` resources, or set outside of Terraform (e.g. in the Metabase interface). This is similar to the relationship between the ` + "`google_*_iam_member`" + ` and ` + "`google_*_iam_policy`" + ` resources of the Google provider.

~> **Warning:** Do not use this resource together with a ` + "`metabase_permissions_graph`" + ` resource, and do not define the same (group, database) pair in several ` + "`metabase_database_permission`" + ` resources. They would overwrite each other's permissions.

Each change reads the current revision of the permissions graph and sends only the managed edge to Metabase. If the graph is modified by someone else in the meantime, the change is retried a few times. Changes made by the provider itself are performed one at a time.

The ` + "`download`" + `, ` + "`data_model`" + ` and ` + "`details`" + ` permissions are optional. When they are not set, they are left as is in Metabase and the current values are reported.

When the resource is deleted, the ` + "`create_queries`" + ` permission is set to ` + "`no`" + `, which is the same value used by the ` + "`metabase_permissions_graph`" + ` resource when removing an edge. The ` + "`view_data`" + ` permission is sent back unchanged, because Metabase rejects edges without it. Other permissions are left as is.

Metabase can return edges which do not define the ` + "`view_data`" + ` permission (e.g. a group with only the ` + "`data_model`" + ` permission). Such edges cannot be imported, and are considered missing: the resource will be created again by setting its permissions.

Permissions for the Administrators group (ID ` + "`2`" + `) cannot be changed, and will result in an error.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the permission, as `<group>/<database>`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"group": schema.Int64Attribute{
				MarkdownDescription: "The ID of the group to which the permission applies.",
				Required:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"database": schema.Int64Attribute{
				MarkdownDescription: "The ID of the database to which the permission applies.",
				Required:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"view_data": schema.StringAttribute{
				MarkdownDescription: "The permission definition for data access.",
				Required:            true,
			},
			"create_queries": schema.StringAttribute{
				MarkdownDescription: "The permission definition for creating queries.",
				Required:            true,
			},
			"download": schema.SingleNestedAttribute{
				MarkdownDescription: "The permission definition for downloading data. If not set, the current value in Metabase is left as is.",
				Optional:            true,
				Computed:            true,
				Attributes:          accessPermissionAttributes,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
			},
			"data_model": schema.SingleNestedAttribute{
				MarkdownDescription: "The permission definition for accessing the data model. If not set, the current value in Metabase is left as is.",
				Optional:            true,
				Computed:            true,
				Attributes:          accessPermissionAttributes,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
			},
			"details": schema.StringAttribute{
				MarkdownDescription: "The permission definition for accessing details. If not set, the current value in Metabase is left as is.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *DatabasePermissionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The group may only be known during the apply, in which case it is checked when creating the resource.
	if !data.Group.IsNull() && !data.Group.IsUnknown() {
		resp.Diagnostics.Append(checkGroupIsNotAdministrators(data.Group.ValueInt64())...)
	}
}

// Returns the ID of the resource, from the group and database IDs.
func makeDatabasePermissionId(groupId int64, databaseId int64) string {
	return fmt.Sprintf("%d/%d", groupId, databaseId)
}

// Converts the resource model to the model of an edge in the permissions graph. Values which are not known yet (not set
// in the configuration and not in the state) are converted to null, meaning they are not sent to Metabase.
func (m DatabasePermissionResourceModel) toEdge() DatabasePermissions {
	nullIfUnknown := func(o types.Object) types.Object {
		if o.IsUnknown() {
			return types.ObjectNull(accessPermissionsObjectType.AttrTypes)
		}
		return o
	}

	details := m.Details
	if details.IsUnknown() {
		details = types.StringNull()
	}

	return DatabasePermissions{
		Group:         m.Group,
		Database:      m.Database,
		ViewData:      m.ViewData,
		CreateQueries: m.CreateQueries,
		Download:      nullIfUnknown(m.Download),
		DataModel:     nullIfUnknown(m.DataModel),
		Details:       details,
	}
}

// Returns the permissions for the given group and database in the graph, or `nil` if the graph does not contain it or
// if it does not define the `view-data` permission.
func findDatabasePermissions(g metabase.PermissionsGraph, groupId int64, databaseId int64) *metabase.PermissionsGraphDatabasePermissions {
	dbPermissionsMap, ok := g.Groups[strconv.FormatInt(groupId, 10)]
	if !ok {
		return nil
	}

	dbPermissions, ok := dbPermissionsMap[strconv.FormatInt(databaseId, 10)]
	if !ok || !hasViewDataPermissions(dbPermissions) {
		return nil
	}

	return &dbPermissions
}

// Sends the given permissions for a single edge of the permissions graph, leaving all other edges untouched.
// `makePermissions` is passed the current permissions for the edge (possibly `nil`), and should return the permissions
// to send, or `nil` if no update is needed.
func updateDatabasePermissions(ctx context.Context, client *metabase.ClientWithResponses, groupId int64, databaseId int64, makePermissions func(*metabase.PermissionsGraphDatabasePermissions) *metabase.PermissionsGraphDatabasePermissions) diag.Diagnostics {
	return updateGraphWithRetry(ctx, &permissionsGraphMutex, "update permissions graph", func() (bool, diag.Diagnostics) {
		getResp, err := client.GetPermissionsGraphWithResponse(ctx)

		diags := checkMetabaseResponse(getResp, err, []int{200}, "get permissions graph")
		if diags.HasError() {
			return false, diags
		}

		permissions := makePermissions(findDatabasePermissions(*getResp.JSON200, groupId, databaseId))
		if permissions == nil {
			return false, diags
		}

		// Metabase only updates the edges present in the request.
		updateResp, err := client.ReplacePermissionsGraphWithResponse(ctx, metabase.PermissionsGraph{
			Revision: getResp.JSON200.Revision,
			Groups: map[string]metabase.PermissionsGraphDatabasePermissionsMap{
				strconv.FormatInt(groupId, 10): {
					strconv.FormatInt(databaseId, 10): *permissions,
				},
			},
		})
		if err == nil && isGraphRevisionConflict(updateResp) {
			return true, diags
		}

		diags.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update permissions graph")...)
		return false, diags
	})
}

// Updates the model from the permissions graph returned by the Metabase API.
// Returns `false` if the graph does not contain the edge for the model.
func (r *DatabasePermissionResource) readDatabasePermissions(ctx context.Context, data *DatabasePermissionResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	getResp, err := r.client.GetPermissionsGraphWithResponse(ctx)

	diags.Append(checkMetabaseResponse(getResp, err, []int{200}, "get permissions graph")...)
	if diags.HasError() {
		return false, diags
	}

	groupId := data.Group.ValueInt64()
	databaseId := data.Database.ValueInt64()

	permissions := findDatabasePermissions(*getResp.JSON200, groupId, databaseId)
	if permissions == nil {
		return false, diags
	}

	// Passing the existing model allows keeping the serialization of JSON permissions when Metabase reshapes them.
	existing := data.toEdge()
	edgeObject, objDiags := makePermissionsObjectFromDatabasePermissions(ctx, int(groupId), int(databaseId), *permissions, &existing)
	diags.Append(objDiags...)
	if diags.HasError() {
		return false, diags
	}

	var edge DatabasePermissions
	diags.Append(edgeObject.As(ctx, &edge, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return false, diags
	}

	data.Id = types.StringValue(makeDatabasePermissionId(groupId, databaseId))
	data.ViewData = edge.ViewData
	data.CreateQueries = edge.CreateQueries
	data.Download = edge.Download
	data.DataModel = edge.DataModel
	data.Details = edge.Details

	return true, diags
}

// Sends the permissions defined in the plan to Metabase, and updates the plan with the values returned by Metabase.
func (r *DatabasePermissionResource) writeDatabasePermissions(ctx context.Context, data *DatabasePermissionResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	groupId := data.Group.ValueInt64()
	databaseId := data.Database.ValueInt64()

	diags.Append(checkGroupIsNotAdministrators(groupId)...)
	if diags.HasError() {
		return diags
	}

	permissions, permDiags := makeDatabasePermissionsFromModel(ctx, data.toEdge(), false)
	diags.Append(permDiags...)
	if diags.HasError() {
		return diags
	}

	diags.Append(updateDatabasePermissions(ctx, r.client, groupId, databaseId, func(*metabase.PermissionsGraphDatabasePermissions) *metabase.PermissionsGraphDatabasePermissions {
		return permissions
	})...)
	if diags.HasError() {
		return diags
	}

	// The response to the update only contains the edges that were sent, which is why the graph is read again.
	found, readDiags := r.readDatabasePermissions(ctx, data)
	diags.Append(readDiags...)
	if diags.HasError() {
		return diags
	}
	if !found {
		diags.AddError(
			"Permissions not found after update.",
			fmt.Sprintf("Metabase did not return the permissions for group %d and database %d after updating them.", groupId, databaseId),
		)
	}

	return diags
}

func (r *DatabasePermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.writeDatabasePermissions(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DatabasePermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := r.readDatabasePermissions(ctx, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DatabasePermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.writeDatabasePermissions(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DatabasePermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateDatabasePermissions(ctx, r.client, data.Group.ValueInt64(), data.Database.ValueInt64(), makeDeletedDatabasePermissions)...)
}

// Returns the permissions to send to Metabase to remove the given permissions, or `nil` if there is nothing to remove.
// Like when an edge is removed from the `metabase_permissions_graph` resource, `create-queries` is set to `no`.
// Metabase rejects edges without `view-data`, so the current value is sent back.
func makeDeletedDatabasePermissions(current *metabase.PermissionsGraphDatabasePermissions) *metabase.PermissionsGraphDatabasePermissions {
	if current == nil {
		return nil
	}

	var createQueriesNo metabase.PermissionsGraphDatabasePermissions_CreateQueries
	// This cannot fail, as it only marshals a constant string.
	_ = createQueriesNo.FromPermissionsGraphDatabasePermissionsCreateQueries0(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No)

	return &metabase.PermissionsGraphDatabasePermissions{
		ViewData:      current.ViewData,
		CreateQueries: &createQueriesNo,
	}
}

func (r *DatabasePermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groupId, databaseId, diags := parseGraphEdgeImportId(req.ID, "database")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	databaseIdInt, err := strconv.ParseInt(databaseId, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Unable to convert the database ID to an integer.", databaseId)
		return
	}

	resp.Diagnostics.Append(checkGroupIsNotAdministrators(groupId)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var data DatabasePermissionResourceModel
	data.Group = types.Int64Value(groupId)
	data.Database = types.Int64Value(databaseIdInt)
	data.ViewData = types.StringNull()
	data.CreateQueries = types.StringNull()
	data.Download = types.ObjectNull(accessPermissionsObjectType.AttrTypes)
	data.DataModel = types.ObjectNull(accessPermissionsObjectType.AttrTypes)
	data.Details = types.StringNull()

	found, readDiags := r.readDatabasePermissions(ctx, &data)
	resp.Diagnostics.Append(readDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Permissions not found.",
			fmt.Sprintf("The permissions graph does not define the view data permission for group %d and database %d.", groupId, databaseIdInt),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
