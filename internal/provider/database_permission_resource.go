package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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
	Id types.String `tfsdk:"id"` // The ID of the edge, as `<group>/<database>`.
	DatabasePermissions
}

func (r *DatabasePermissionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `The permissions of a single permissions group on a single database.

Unlike the ` + "`metabase_permissions_graph`" + ` resource, which manages the entire permissions graph and resets the permissions it does not define, this resource only manages a single (group, database) edge of the graph. All other permissions are left untouched, whether they are managed by other ` + "`metabase_database_permission`" + ` resources, or set outside of Terraform (e.g. in the Metabase interface). This is similar to the relationship between the ` + "`google_*_iam_member`" + ` and ` + "`google_*_iam_policy`" + ` resources of the Google provider.

~> **Warning:** Do not use this resource together with a ` + "`metabase_permissions_graph`" + ` resource, and do not define the same (group, database) pair in several ` + "`metabase_database_permission`" + ` resources. They would overwrite each other's permissions.

Each change only sends the managed edge to Metabase, without checking the revision of the permissions graph, as no other edge is changed. Changes made by the provider are performed one at a time. The permissions are read from the permissions graph restricted to the group, rather than from the entire graph.

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
				MarkdownDescription: "The permission definition for downloading data. If not set, the current value in Metabase is left as is. When the permission is revoked, Metabase omits it from its responses, and it is reported as `none`.",
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
				MarkdownDescription: "The permission definition for accessing the data model. If not set, the current value in Metabase is left as is. When the permission is revoked, Metabase omits it from its responses, and it is reported as `none`, which is always the case on the free edition.",
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
				MarkdownDescription: "The permission definition for accessing details. If not set, the current value in Metabase is left as is. When the permission is revoked, Metabase omits it from its responses, and it is reported as `no`, which is always the case on the free edition.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *DatabasePermissionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The group may only be known during the apply, in which case it is checked when creating the resource.
	if !config.Group.IsNull() && !config.Group.IsUnknown() {
		resp.Diagnostics.Append(checkGroupIsNotAdministrators(config.Group.ValueInt64())...)
	}

	resp.Diagnostics.Append(checkDatabasePermissions(config.DatabasePermissions)...)
}

// Returns errors for the combinations of permissions which Metabase changes when they are applied, and which would then
// differ from the configuration after the apply. When `view_data` is set to `blocked` for the entire database, Metabase
// revokes `create-queries` and `download`.
func checkDatabasePermissions(permissions DatabasePermissions) diag.Diagnostics {
	var diags diag.Diagnostics

	if permissions.ViewData.IsUnknown() || permissions.ViewData.ValueString() != string(metabase.PermissionsGraphDatabasePermissionsViewData0Blocked) {
		return diags
	}

	if !permissions.CreateQueries.IsUnknown() && permissions.CreateQueries.ValueString() != string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No) {
		diags.AddAttributeError(
			path.Root("create_queries"),
			"Invalid permissions for blocked data.",
			fmt.Sprintf("Metabase revokes the create queries permission when the view data permission is %q, so it must be %q.", metabase.PermissionsGraphDatabasePermissionsViewData0Blocked, metabase.PermissionsGraphDatabasePermissionsCreateQueries0No),
		)
	}

	if !permissions.Download.IsNull() && !permissions.Download.IsUnknown() {
		schemas, ok := permissions.Download.Attributes()["schemas"].(types.String)
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

// Checks that the group and the database exist when the resource is created, such that a wrong ID is reported by the
// plan rather than during the apply. They can only change when the resource is created, as they require a replacement.
//
// When the resource is updated, marks `download` as unknown if Metabase may revoke it. When the optional permissions
// are not set in the configuration, their values from the state are used in the plan (see the `UseStateForUnknown`
// plan modifiers). However Metabase revokes the `download` permission when `view-data` is set to `blocked`, so it is
// marked as unknown instead when `view_data` changes, unless it becomes `unrestricted`.
func (r *DatabasePermissionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Nothing to do when deleting the resource.
	if req.Plan.Raw.IsNull() {
		return
	}

	if req.State.Raw.IsNull() {
		var plan DatabasePermissionResourceModel
		resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
		if resp.Diagnostics.HasError() {
			return
		}

		// The IDs are unknown when the group or the database is created in the same apply, in which case they are
		// checked when creating the resource.
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

// Reads the permissions for a single edge from the permissions graph restricted to its group (see
// `metabase.PermissionsGraph.FindDatabasePermissions`). The graph is empty when the group does not exist.
func getDatabasePermissions(ctx context.Context, client *metabase.ClientWithResponses, groupId int64, databaseId int64) (*metabase.PermissionsGraphDatabasePermissions, diag.Diagnostics) {
	getResp, err := client.GetPermissionsGraphForGroupWithResponse(ctx, int(groupId))

	diags := checkMetabaseResponse(getResp, err, []int{200}, "get permissions graph")
	if diags.HasError() {
		return nil, diags
	}

	return getResp.JSON200.FindDatabasePermissions(int(groupId), int(databaseId)), diags
}

// Sends the given permissions for a single edge of the permissions graph, leaving all other edges untouched. The
// revision of the graph is not checked, as no other edge is changed. The update is serialized with the other updates
// of the graph by the provider. Returns the graph after the update, restricted to the group of the edge.
func updateDatabasePermissions(ctx context.Context, client *metabase.ClientWithResponses, tracker *graphlock.PermissionsGraphTracker, groupId int64, databaseId int64, permissions metabase.PermissionsGraphDatabasePermissions) (*metabase.PermissionsGraph, diag.Diagnostics) {
	force := true
	var updateResp *metabase.ReplacePermissionsGraphResponse
	err := tracker.Update(func() error {
		var err error
		// Metabase only updates the edges present in the request.
		updateResp, err = client.ReplacePermissionsGraphWithResponse(ctx, &metabase.ReplacePermissionsGraphParams{Force: &force}, metabase.PermissionsGraph{
			Groups: map[string]metabase.PermissionsGraphDatabasePermissionsMap{
				strconv.FormatInt(groupId, 10): {
					strconv.FormatInt(databaseId, 10): permissions,
				},
			},
		})
		return err
	})

	diags := checkMetabaseResponse(updateResp, err, []int{200}, "update permissions graph")
	if diags.HasError() {
		return nil, diags
	}

	return updateResp.JSON200, diags
}

// The `view-data` and `create-queries` permissions of an edge returned by Metabase after a write, which are stored in
// the private state. They are the only permissions which can be JSON objects, whose shape Metabase can change (see
// `Read`).
type writtenDatabasePermissions struct {
	ViewData      metabase.PermissionsGraphDatabasePermissions_ViewData       `json:"view-data"`
	CreateQueries *metabase.PermissionsGraphDatabasePermissions_CreateQueries `json:"create-queries,omitempty"`
}

// The key in the private state storing the permissions written by the last write.
const writtenDatabasePermissionsKey privateStateKey[writtenDatabasePermissions] = "written_permissions"

// Returns the written permissions of an edge returned by Metabase.
func makeWrittenDatabasePermissions(p metabase.PermissionsGraphDatabasePermissions) writtenDatabasePermissions {
	return writtenDatabasePermissions{
		ViewData:      p.ViewData,
		CreateQueries: p.CreateQueries,
	}
}

// Returns whether the permissions are the same, whatever the order of the keys of JSON objects. Permissions which
// cannot be compared are considered different.
func (w writtenDatabasePermissions) equal(other writtenDatabasePermissions) bool {
	decode := func(p writtenDatabasePermissions) (any, error) {
		serialized, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}

		// JSON objects are decoded as Go maps, which have no order.
		var v any
		err = json.Unmarshal(serialized, &v)
		return v, err
	}

	va, errA := decode(w)
	vb, errB := decode(other)

	return errA == nil && errB == nil && reflect.DeepEqual(va, vb)
}

// Makes the permissions to send to Metabase: the permissions of the plan, except the optional permissions, which are
// only sent when they are set in the configuration. Otherwise, the plan contains their values from the state (or
// unknown values), which should not be sent back to Metabase.
func makeRequestedDatabasePermissions(plan DatabasePermissionResourceModel, config DatabasePermissionResourceModel) DatabasePermissions {
	return DatabasePermissions{
		Group:         plan.Group,
		Database:      plan.Database,
		ViewData:      plan.ViewData,
		CreateQueries: plan.CreateQueries,
		Download:      config.Download,
		DataModel:     config.DataModel,
		Details:       config.Details,
	}
}

// Sends the given permissions of an edge to Metabase, and returns the permissions of the edge after the update.
func (r *DatabasePermissionResource) writeDatabasePermissions(ctx context.Context, request DatabasePermissions) (*metabase.PermissionsGraphDatabasePermissions, diag.Diagnostics) {
	var diags diag.Diagnostics

	groupId := request.Group.ValueInt64()
	databaseId := request.Database.ValueInt64()

	diags.Append(checkGroupIsNotAdministrators(groupId)...)
	// The permissions are checked again, as they may have been unknown when the configuration was validated.
	diags.Append(checkDatabasePermissions(request)...)
	if diags.HasError() {
		return nil, diags
	}

	// `false` leaves the optional permissions which are not set out of the request, rather than revoking them.
	permissions, permDiags := makeDatabasePermissionsFromModel(ctx, request, false)
	diags.Append(permDiags...)
	if diags.HasError() {
		return nil, diags
	}

	graph, updateDiags := updateDatabasePermissions(ctx, r.client, r.permissionsGraph, groupId, databaseId, *permissions)
	diags.Append(updateDiags...)
	if diags.HasError() {
		return nil, diags
	}

	// The response is the graph after the update, restricted to the group of the edge.
	updated := graph.FindDatabasePermissions(int(groupId), int(databaseId))
	if updated == nil {
		diags.AddError(
			"Permissions not found after update.",
			fmt.Sprintf("Metabase did not return the permissions for group %d and database %d after updating them.", groupId, databaseId),
		)
		return nil, diags
	}

	return updated, diags
}

func (r *DatabasePermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan *DatabasePermissionResourceModel
	var config DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The group and the database can only change when the resource is created, as they require a replacement. Both are
	// checked, such that all the errors are reported at once.
	resp.Diagnostics.Append(checkPermissionsGroupExists(ctx, r.client, plan.Group.ValueInt64())...)
	resp.Diagnostics.Append(checkDatabaseExists(ctx, r.client, plan.Database.ValueInt64())...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Id = types.StringValue(makeGraphEdgeId(plan.Group.ValueInt64(), strconv.FormatInt(plan.Database.ValueInt64(), 10)))

	updated, diags := r.writeDatabasePermissions(ctx, makeRequestedDatabasePermissions(*plan, config))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The permissions have just been written, so the JSON values of the plan are kept.
	//
	// TODO: The JSON values from the plan are kept even if Metabase stored different permissions, rather than the same
	// permissions in a different shape. The response is then stored as the written permissions, so the difference is
	// never reported. `metabase_permissions_graph` keeps the JSON values of its model in the same way.
	permissions, diags := makeDatabasePermissionsModel(ctx, int(plan.Group.ValueInt64()), int(plan.Database.ValueInt64()), updated.WithOmittedPermissionsRevoked(), &plan.DatabasePermissions)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.DatabasePermissions = *permissions

	resp.Diagnostics.Append(writePrivateState(ctx, resp.Private, writtenDatabasePermissionsKey, makeWrittenDatabasePermissions(*updated))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *DatabasePermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state *DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	written, diags := readPrivateState(ctx, req.Private, writtenDatabasePermissionsKey)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	permissions, diags := getDatabasePermissions(ctx, r.client, state.Group.ValueInt64(), state.Database.ValueInt64())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Metabase drops the edges of deleted groups and databases from the graph.
	if permissions == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// The JSON values of the state are kept as long as the response has not changed since the last write, or when no
	// permissions were stored after a write. Otherwise, the permissions were changed outside of Terraform, and the
	// response is used instead, such that the change is reported.
	var existing *DatabasePermissions
	if written == nil || written.equal(makeWrittenDatabasePermissions(*permissions)) {
		existing = &state.DatabasePermissions
	}

	model, diags := makeDatabasePermissionsModel(ctx, int(state.Group.ValueInt64()), int(state.Database.ValueInt64()), permissions.WithOmittedPermissionsRevoked(), existing)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.DatabasePermissions = *model

	// The private state is left as is: it should only change when the permissions are written. This way, changes made
	// outside of Terraform keep being reported until they are reverted.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *DatabasePermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan *DatabasePermissionResourceModel
	var config DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, diags := r.writeDatabasePermissions(ctx, makeRequestedDatabasePermissions(*plan, config))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The permissions have just been written, so the JSON values of the plan are kept (see `Create`).
	permissions, diags := makeDatabasePermissionsModel(ctx, int(plan.Group.ValueInt64()), int(plan.Database.ValueInt64()), updated.WithOmittedPermissionsRevoked(), &plan.DatabasePermissions)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.DatabasePermissions = *permissions

	resp.Diagnostics.Append(writePrivateState(ctx, resp.Private, writtenDatabasePermissionsKey, makeWrittenDatabasePermissions(*updated))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *DatabasePermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state *DatabasePermissionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupId := state.Group.ValueInt64()
	databaseId := state.Database.ValueInt64()

	permissions, diags := getDatabasePermissions(ctx, r.client, groupId, databaseId)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// There is nothing to revoke when the edge has no `view-data` permission, e.g. when its group or its database was
	// deleted.
	if permissions == nil {
		return
	}

	// Metabase never removes an edge from the graph, so its permissions are revoked instead, like when an edge is removed
	// from the `metabase_permissions_graph` resource. Revoking advanced permissions is ignored on the free edition.
	_, diags = updateDatabasePermissions(ctx, r.client, r.permissionsGraph, groupId, databaseId, metabase.NewRevokedDatabasePermissions(true))
	resp.Diagnostics.Append(diags...)
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

	permissions, diags := getDatabasePermissions(ctx, r.client, groupId, databaseIdInt)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if permissions == nil {
		resp.Diagnostics.AddError(
			"Permissions not found.",
			fmt.Sprintf("The permissions graph does not define the view data permission for group %d and database %d.", groupId, databaseIdInt),
		)
		return
	}

	// There is no JSON value to keep, so the values of the response are used.
	model, diags := makeDatabasePermissionsModel(ctx, int(groupId), int(databaseIdInt), permissions.WithOmittedPermissionsRevoked(), nil)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	data := DatabasePermissionResourceModel{
		Id:                  types.StringValue(makeGraphEdgeId(groupId, databaseId)),
		DatabasePermissions: *model,
	}

	resp.Diagnostics.Append(writePrivateState(ctx, resp.Private, writtenDatabasePermissionsKey, makeWrittenDatabasePermissions(*permissions))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
