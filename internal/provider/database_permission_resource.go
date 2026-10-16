package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/flovouin/terraform-provider-metabase/internal/graphlock"
	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
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
var _ resource.ResourceWithModifyPlan = &DatabasePermissionResource{}

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

func (r *DatabasePermissionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `The permissions of a single permissions group on a single database.

Unlike the ` + "`metabase_permissions_graph`" + ` resource, which manages the entire permissions graph and resets the permissions it does not define, this resource only manages a single (group, database) edge of the graph. All other permissions are left untouched, whether they are managed by other ` + "`metabase_database_permission`" + ` resources, or set outside of Terraform (e.g. in the Metabase interface). This is similar to the relationship between the ` + "`google_*_iam_member`" + ` and ` + "`google_*_iam_policy`" + ` resources of the Google provider.

~> **Warning:** Do not use this resource together with a ` + "`metabase_permissions_graph`" + ` resource, and do not define the same (group, database) pair in several ` + "`metabase_database_permission`" + ` resources. They would overwrite each other's permissions.

Each change reads the current revision of the permissions graph and sends only the managed edge to Metabase. Changes made by the provider are performed one at a time.

~> **Warning:** Metabase grants default permissions when a database or a group is created, e.g. full access to the All Users group on a new database. This resource only manages its own (group, database) pair, so it cannot see those default permissions. Managing the permissions of the All Users group explicitly is recommended, e.g. with the lowest permissions.

The lowest permissions can be declared to make sure a group has no access to a database, beyond viewing the data of the questions it has access to: ` + "`view_data`" + ` set to ` + "`unrestricted`" + `, ` + "`create_queries`" + ` set to ` + "`no`" + `, and ` + "`download`" + ` set to ` + "`none`" + `. Unlike the ` + "`metabase_permissions_graph`" + ` resource, which considers the pairs with the lowest permissions absent, this resource manages such a pair like any other.

The ` + "`download`" + `, ` + "`data_model`" + ` and ` + "`details`" + ` permissions are optional. When they are not set in the configuration, they are never sent to Metabase, and the current values are reported.

Metabase never removes a (group, database) pair from the graph. When the resource is deleted, its permissions are revoked, like when a pair is removed from the ` + "`metabase_permissions_graph`" + ` resource: ` + "`view_data`" + ` is set to ` + "`unrestricted`" + `, and ` + "`create_queries`" + `, ` + "`download`" + `, ` + "`data_model`" + ` and ` + "`details`" + ` are revoked.

When the group or the database is deleted, the resource is considered deleted.

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
				MarkdownDescription: "The permission definition for data access (\"View data\" in the Metabase interface). Either a single value for the entire database: `unrestricted`, or, with advanced permissions (paid plans), `blocked`, `impersonated` or `sandboxed` (`legacy-no-self-service` may be returned for instances migrated from the legacy permissions model). Or a JSON-encoded object defining the permission per schema (`jsonencode({ <schema> = <value> })`) or per table (`jsonencode({ <schema> = { <table ID> = <value> } })`).",
				Required:            true,
			},
			"create_queries": schema.StringAttribute{
				MarkdownDescription: "The permission definition for creating queries (\"Create queries\" in the Metabase interface). Either a single value for the entire database: `query-builder-and-native` (query builder and native SQL), `query-builder` (query builder only) or `no`. Or a JSON-encoded object defining the permission per table: `jsonencode({ <schema> = { <table ID> = <value> } })`, where the value is `query-builder` or `no` (native queries can only be allowed for the entire database). When the permission is `no`, Metabase omits it from its responses, and it is reported as `no`.",
				Required:            true,
			},
			"download": schema.SingleNestedAttribute{
				MarkdownDescription: "The permission definition for downloading data. If not set, the current value in Metabase is left as is.",
				Optional:            true,
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"schemas": schema.StringAttribute{
						MarkdownDescription: "The permission to download results from the entire database: `full` (up to 1 million rows), `limited` (up to 10 thousand rows) or `none`. Download permissions are only available on paid plans.",
						Optional:            true,
					},
				},
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
			},
			"data_model": schema.SingleNestedAttribute{
				MarkdownDescription: "The permission definition for accessing the data model. If not set, the current value in Metabase is left as is.",
				Optional:            true,
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"schemas": schema.StringAttribute{
						MarkdownDescription: "The permission to edit the table metadata of the entire database: `all` or `none`. Data model permissions are only available on paid plans.",
						Optional:            true,
					},
				},
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
			},
			"details": schema.StringAttribute{
				MarkdownDescription: "The permission definition for accessing details. If not set, the current value in Metabase is left as is.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
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

	resp.Diagnostics.Append(checkBlockedViewData(data)...)
}

// Returns an error if the permissions contradict a `view_data` permission set to `blocked` for the entire database.
// Metabase then revokes `create-queries` and `download`, which would differ from the configuration after the apply.
func checkBlockedViewData(data DatabasePermissionResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if data.ViewData.IsUnknown() || data.ViewData.ValueString() != string(metabase.PermissionsGraphDatabasePermissionsViewData0Blocked) {
		return diags
	}

	if !data.CreateQueries.IsUnknown() && data.CreateQueries.ValueString() != string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No) {
		diags.AddAttributeError(
			path.Root("create_queries"),
			"Invalid permissions for blocked data.",
			fmt.Sprintf("Metabase revokes the create queries permission when the view data permission is %q, so it must be %q.", metabase.PermissionsGraphDatabasePermissionsViewData0Blocked, metabase.PermissionsGraphDatabasePermissionsCreateQueries0No),
		)
	}

	if !data.Download.IsNull() && !data.Download.IsUnknown() {
		schemas, ok := data.Download.Attributes()["schemas"].(types.String)
		if ok && !schemas.IsUnknown() && schemas.ValueString() != string(metabase.PermissionsGraphDatabaseAccessSchemas0None) {
			diags.AddAttributeError(
				path.Root("download"),
				"Invalid permissions for blocked data.",
				fmt.Sprintf("Metabase revokes the download permission when the view data permission is %q, so it must be %q or not set.", metabase.PermissionsGraphDatabasePermissionsViewData0Blocked, metabase.PermissionsGraphDatabaseAccessSchemas0None),
			)
		}
	}

	return diags
}

// When the optional permissions are not set in the configuration, their values from the state are used in the plan
// (see the `UseStateForUnknown` plan modifiers). However Metabase revokes the `download` permission when `view-data` is
// set to `blocked`, so it is marked as unknown instead when `view_data` changes, unless it becomes `unrestricted`.
func (r *DatabasePermissionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Nothing to do when deleting the resource.
	if req.Plan.Raw.IsNull() {
		return
	}

	// The group and the database can only change when the resource is created, as they require a replacement. They are
	// checked by the plan, such that a wrong ID is reported before the apply, unless it is unknown (e.g. when the group
	// or the database is created in the same apply), in which case it is checked when creating the resource.
	if req.State.Raw.IsNull() {
		var plan DatabasePermissionResourceModel
		resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
		if resp.Diagnostics.HasError() {
			return
		}

		if !plan.Group.IsUnknown() {
			resp.Diagnostics.Append(checkPermissionsGroupExists(ctx, r.client, plan.Group.ValueInt64())...)
		}
		if !plan.Database.IsUnknown() {
			resp.Diagnostics.Append(checkDatabaseExists(ctx, r.client, plan.Database.ValueInt64())...)
		}
		return
	}

	var state, plan, config DatabasePermissionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// `blocked` can be set for the entire database, or for some tables in a JSON object.
	viewDataMayBlock := !plan.ViewData.Equal(state.ViewData) && plan.ViewData.ValueString() != string(metabase.PermissionsGraphDatabasePermissionsViewData0Unrestricted)
	if !config.Download.IsNull() || !viewDataMayBlock {
		return
	}

	plan.Download = types.ObjectUnknown(accessPermissionsObjectType.AttrTypes)
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
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

// Sends the given permissions for a single edge of the permissions graph, leaving all other edges untouched. The graph
// is read right before the update, as the update must include its current revision. The update is serialized with the
// other updates of the graph by the provider.
func updateDatabasePermissions(ctx context.Context, client *metabase.ClientWithResponses, tracker *graphlock.PermissionsGraphTracker, groupId int64, databaseId int64, permissions metabase.PermissionsGraphDatabasePermissions) diag.Diagnostics {
	var diags diag.Diagnostics

	_ = tracker.Update(func() error {
		getResp, err := client.GetPermissionsGraphWithResponse(ctx)

		diags.Append(checkMetabaseResponse(getResp, err, []int{200}, "get permissions graph")...)
		if diags.HasError() {
			return nil
		}

		// Metabase only updates the edges present in the request.
		updateResp, err := client.ReplacePermissionsGraphWithResponse(ctx, metabase.PermissionsGraph{
			Revision: getResp.JSON200.Revision,
			Groups: map[string]metabase.PermissionsGraphDatabasePermissionsMap{
				strconv.FormatInt(groupId, 10): {
					strconv.FormatInt(databaseId, 10): permissions,
				},
			},
		})

		diags.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update permissions graph")...)
		return nil
	})

	return diags
}

// The key in the private state storing the `view-data` and `create-queries` permissions returned by Metabase after the
// last write. See `readDatabasePermissions`.
const writtenDatabasePermissionsPrivateKey = "written_permissions"

// Returns a canonical JSON serialization of the `view-data` and `create-queries` permissions returned by Metabase, such
// that two responses can be compared.
func makeComparableDatabasePermissions(p metabase.PermissionsGraphDatabasePermissions) ([]byte, error) {
	b, err := json.Marshal(metabase.PermissionsGraphDatabasePermissions{
		ViewData:      p.ViewData,
		CreateQueries: p.CreateQueries,
	})
	if err != nil {
		return nil, err
	}

	// Going through a generic value sorts the keys of JSON objects.
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}

	return json.Marshal(v)
}

// Updates the model from the permissions graph returned by the Metabase API.
// Returns `false` if the graph does not contain the edge for the model. Otherwise, also returns the comparable
// permissions from the response, which should be stored in the private state after a write.
//
// Metabase can return JSON permissions in a different but equivalent shape (e.g. collapsing a uniform object to a
// single value). To avoid a perpetual diff, the JSON values of the model are kept, as long as the response has not
// changed since the last write (`written`). If it has, the permissions were changed outside of Terraform, and the
// response is used instead, such that the change is reported. If `written` is `nil`, the model values are kept.
func (r *DatabasePermissionResource) readDatabasePermissions(ctx context.Context, data *DatabasePermissionResourceModel, written []byte) (bool, []byte, diag.Diagnostics) {
	var diags diag.Diagnostics

	getResp, err := r.client.GetPermissionsGraphWithResponse(ctx)

	diags.Append(checkMetabaseResponse(getResp, err, []int{200}, "get permissions graph")...)
	if diags.HasError() {
		return false, nil, diags
	}

	groupId := data.Group.ValueInt64()
	databaseId := data.Database.ValueInt64()

	permissions := findDatabasePermissions(*getResp.JSON200, groupId, databaseId)
	if permissions == nil {
		return false, nil, diags
	}

	current, err := makeComparableDatabasePermissions(*permissions)
	if err != nil {
		diags.AddError("Unexpected error serializing permissions.", err.Error())
		return false, nil, diags
	}

	// Passing the existing model allows keeping the serialization of JSON permissions when Metabase reshapes them.
	var existing *DatabasePermissions
	if written == nil || bytes.Equal(written, current) {
		edge := data.toEdge()
		existing = &edge
	}
	edgeObject, objDiags := makePermissionsObjectFromDatabasePermissions(ctx, int(groupId), int(databaseId), *permissions, existing)
	diags.Append(objDiags...)
	if diags.HasError() {
		return false, nil, diags
	}

	var edge DatabasePermissions
	diags.Append(edgeObject.As(ctx, &edge, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return false, nil, diags
	}

	data.Id = types.StringValue(makeGraphEdgeId(groupId, strconv.FormatInt(databaseId, 10)))
	data.ViewData = edge.ViewData
	data.CreateQueries = edge.CreateQueries
	// Metabase omits revoked permissions from its responses, so the revoked values of the model are kept.
	data.Download = keepRevokedAccessPermissions(edge.Download, data.Download)
	data.DataModel = keepRevokedAccessPermissions(edge.DataModel, data.DataModel)
	if !edge.Details.IsNull() || data.Details.ValueString() != string(metabase.PermissionsGraphDatabasePermissionsDetailsNo) {
		data.Details = edge.Details
	}

	return true, current, diags
}

// Returns the access permissions read from Metabase, unless they are missing and the model revokes them. Metabase omits
// revoked permissions (`none`) from its responses, which would otherwise be reported as a different value.
func keepRevokedAccessPermissions(read types.Object, model types.Object) types.Object {
	if !read.IsNull() || model.IsNull() || model.IsUnknown() {
		return read
	}

	schemas, ok := model.Attributes()["schemas"].(types.String)
	if ok && schemas.ValueString() == string(metabase.PermissionsGraphDatabaseAccessSchemas0None) {
		return model
	}

	return read
}

// Sends the permissions defined in the plan to Metabase, and updates the plan with the values returned by Metabase.
// The optional permissions are only sent when they are set in the configuration. Otherwise, the plan would contain
// the previous values from the state (or unknown values), which should not be sent back to Metabase.
// Returns the value to store in the private state (see `readDatabasePermissions`).
func (r *DatabasePermissionResource) writeDatabasePermissions(ctx context.Context, data *DatabasePermissionResourceModel, config DatabasePermissionResourceModel) ([]byte, diag.Diagnostics) {
	var diags diag.Diagnostics

	groupId := data.Group.ValueInt64()
	databaseId := data.Database.ValueInt64()

	diags.Append(checkGroupIsNotAdministrators(groupId)...)
	if diags.HasError() {
		return nil, diags
	}

	edge := data.toEdge()
	configEdge := config.toEdge()
	edge.Download = configEdge.Download
	edge.DataModel = configEdge.DataModel
	edge.Details = configEdge.Details

	permissions, permDiags := makeDatabasePermissionsFromModel(ctx, edge, false)
	diags.Append(permDiags...)
	if diags.HasError() {
		return nil, diags
	}

	diags.Append(updateDatabasePermissions(ctx, r.client, r.permissionsGraph, groupId, databaseId, *permissions)...)
	if diags.HasError() {
		return nil, diags
	}

	// The response to the update only contains the edges that were sent, which is why the graph is read again.
	// The permissions have just been written, so the values from the plan are kept.
	//
	// TODO: The JSON values from the plan are kept even if Metabase stored different permissions, rather than the same
	// permissions in a different shape. The response is then stored as the written permissions, so the difference is
	// never reported. `metabase_permissions_graph` keeps the JSON values of its model in the same way.
	found, written, readDiags := r.readDatabasePermissions(ctx, data, nil)
	diags.Append(readDiags...)
	if diags.HasError() {
		return nil, diags
	}
	if !found {
		diags.AddError(
			"Permissions not found after update.",
			fmt.Sprintf("Metabase did not return the permissions for group %d and database %d after updating them.", groupId, databaseId),
		)
	}

	return written, diags
}

func (r *DatabasePermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *DatabasePermissionResourceModel
	var config DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The group and the database can only change when the resource is created, as they require a replacement. Both are
	// checked, such that all the errors are reported at once.
	resp.Diagnostics.Append(checkPermissionsGroupExists(ctx, r.client, data.Group.ValueInt64())...)
	resp.Diagnostics.Append(checkDatabaseExists(ctx, r.client, data.Database.ValueInt64())...)
	if resp.Diagnostics.HasError() {
		return
	}

	written, diags := r.writeDatabasePermissions(ctx, data, config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.Private.SetKey(ctx, writtenDatabasePermissionsPrivateKey, written)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DatabasePermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	written, diags := req.Private.GetKey(ctx, writtenDatabasePermissionsPrivateKey)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, _, diags := r.readDatabasePermissions(ctx, data, written)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	// The private state is left as is: it should only change when the permissions are written. This way, changes made
	// outside of Terraform keep being reported until they are reverted.
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DatabasePermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *DatabasePermissionResourceModel
	var config DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	written, diags := r.writeDatabasePermissions(ctx, data, config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.Private.SetKey(ctx, writtenDatabasePermissionsPrivateKey, written)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DatabasePermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupId := data.Group.ValueInt64()
	databaseId := data.Database.ValueInt64()

	getResp, err := r.client.GetPermissionsGraphWithResponse(ctx)

	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200}, "get permissions graph")...)
	if resp.Diagnostics.HasError() {
		return
	}

	// There is nothing to revoke when the edge has no `view-data` permission, e.g. when its group or its database was
	// deleted.
	if findDatabasePermissions(*getResp.JSON200, groupId, databaseId) == nil {
		return
	}

	// Metabase never removes an edge from the graph, so its permissions are revoked instead, like when an edge is removed
	// from the `metabase_permissions_graph` resource. Revoking advanced permissions is ignored on the free edition.
	resp.Diagnostics.Append(updateDatabasePermissions(ctx, r.client, r.permissionsGraph, groupId, databaseId, makeRevokedDatabasePermissions(true))...)
}

func (r *DatabasePermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groupId, databaseId, diags := parseGraphEdgeId(req.ID, "database")
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

	// Both are checked, such that all the errors are reported at once.
	resp.Diagnostics.Append(checkPermissionsGroupExists(ctx, r.client, groupId)...)
	resp.Diagnostics.Append(checkDatabaseExists(ctx, r.client, databaseIdInt)...)
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

	found, written, readDiags := r.readDatabasePermissions(ctx, &data, nil)
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

	resp.Diagnostics.Append(resp.Private.SetKey(ctx, writtenDatabasePermissionsPrivateKey, written)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
