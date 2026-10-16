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

func (r *PermissionsGraphResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `The graph of permissions between permissions groups and databases.

Metabase exposes a single resource to define all permissions related to databases. This means a single permissions graph resource should be defined in the entire Terraform configuration. However this is not the same as the collection graph, and the two can be combined to grant permissions.

The permissions graph cannot be created or deleted. Trying to create it will result in an error. It should be imported instead. Trying to delete the resource will succeed with no impact on Metabase (it is a no-op).

Permissions for the Administrators group cannot be changed. To avoid issues during the update, all permissions for the Administrators group are ignored by default. This behavior can be changed using the ignored groups attribute.

Metabase never removes a (group, database) pair from the graph. Instead, the pairs which are not in the configuration have their permissions revoked: ` + "`view_data`" + ` is set to ` + "`unrestricted`" + `, and ` + "`create_queries`" + `, ` + "`download`" + ` (as well as ` + "`data_model`" + ` and ` + "`details`" + ` with advanced permissions) are revoked. Pairs with revoked permissions are considered absent when reading the graph.

Metabase grants default permissions when a database or a group is created. Those are reported as changes on the next plan, and are revoked by the next apply if they are not part of the configuration.`,

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
						"view_data": schema.StringAttribute{
							MarkdownDescription: "The permission definition for data access.",
							Required:            true,
						},
						"create_queries": schema.StringAttribute{
							MarkdownDescription: "The permission definition for creating queries.",
							Required:            true,
						},
						"download": schema.SingleNestedAttribute{
							MarkdownDescription: "The permission definition for downloading data.",
							Optional:            true,
							Attributes: map[string]schema.Attribute{
								"schemas": schema.StringAttribute{
									MarkdownDescription: "The permission to download results from the entire database: `full` (up to 1 million rows), `limited` (up to 10 thousand rows) or `none`. Download permissions are only available on paid plans.",
									Optional:            true,
								},
							},
						},
						"data_model": schema.SingleNestedAttribute{
							MarkdownDescription: "The permission definition for accessing the data model.",
							Optional:            true,
							Attributes: map[string]schema.Attribute{
								"schemas": schema.StringAttribute{
									MarkdownDescription: "The permission to edit the table metadata of the entire database: `all` or `none`. Data model permissions are only available on paid plans.",
									Optional:            true,
								},
							},
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

// Updates the given `PermissionsGraphResourceModel` from the `PermissionsGraph` returned by the Metabase API.
func updateModelFromPermissionsGraph(ctx context.Context, g metabase.PermissionsGraph, data *PermissionsGraphResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Revision = types.Int64Value(int64(g.Revision))

	ignoredGroups, groupsDiags := getIgnoredPermissionsGroups(ctx, data.IgnoredGroups)
	diags.Append(groupsDiags...)
	if diags.HasError() {
		return diags
	}

	// Getting the permissions of the current model, to handle weird cases in the Metabase API response.
	existingModelPermissions := make([]DatabasePermissions, 0, len(data.Permissions.Elements()))
	diags.Append(data.Permissions.ElementsAs(ctx, &existingModelPermissions, false)...)
	if diags.HasError() {
		return diags
	}

	// Indexed by (group, database), as a linear scan per pair is quadratic in the size of the graph.
	type permissionsKey struct {
		group    int64
		database int64
	}
	existingByKey := make(map[permissionsKey]*DatabasePermissions, len(existingModelPermissions))
	for i := range existingModelPermissions {
		existing := &existingModelPermissions[i]
		if existing.Group.IsNull() || existing.Database.IsNull() {
			continue
		}
		key := permissionsKey{existing.Group.ValueInt64(), existing.Database.ValueInt64()}
		if _, ok := existingByKey[key]; !ok {
			existingByKey[key] = existing
		}
	}

	permissionsList := make([]attr.Value, 0, len(data.Permissions.Elements()))
	for groupId, dbPermissionsMap := range g.Groups {
		// Permissions for ignored groups are not stored in the state for clarity.
		if ignoredGroups[groupId] {
			continue
		}

		groupIdInt, err := strconv.Atoi(groupId)
		if err != nil {
			diags.AddError("Could not convert the group ID to an integer.", err.Error())
			return diags
		}

		for dbId, dbPermissions := range dbPermissionsMap {
			// Ignore the Metabase Analytics database until we have proper support.
			if dbId == metabase.MetabaseAnalyticsDatabaseId {
				continue
			}

			// Metabase 0.61+ can return edges that carry only advanced permissions (e.g. an internal group with
			// just `data-model` set) and omit `view-data` entirely. The provider models every edge around
			// `view-data`, so it cannot represent these fragments; skip them rather than failing to parse the
			// empty `view-data` union.
			if !dbPermissions.HasViewData() {
				continue
			}

			dbIdInt, err := strconv.Atoi(dbId)
			if err != nil {
				diags.AddError("Could not convert the database ID to an integer.", err.Error())
				return diags
			}

			// The permissions of the pair in the Terraform model, if any.
			existingPermission := existingByKey[permissionsKey{int64(groupIdInt), int64(dbIdInt)}]

			// Metabase never removes a pair from the graph: the pairs removed from the configuration are revoked instead (see
			// `metabase.NewRevokedDatabasePermissions`). Such pairs are skipped, as if they were not in the graph, otherwise
			// they would be reported as changes on every plan. A pair is skipped when both:
			//   - It is not in the Terraform model, i.e. in the state, or in the plan after an update. A pair declared in the
			//     configuration is always read, even with the lowest permissions, otherwise it would be missing from the state.
			//   - It is revoked. The other pairs which are not in the Terraform model (e.g. permissions granted in the Metabase
			//     interface) are read, such that they are reported as changes and revoked by the next apply.
			if existingPermission == nil && dbPermissions.IsRevoked() {
				continue
			}

			permissionsObject, objDiags := makePermissionsObjectFromDatabasePermissions(ctx, groupIdInt, dbIdInt, dbPermissions, existingPermission)
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

// Makes the entire permissions graph from the Terraform model.
// Passing the current state allows comparing the plan to an existing set of permissions. This allows explicitly
// revoking the permissions removed from the plan (see `metabase.NewRevokedDatabasePermissions`).
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

		dbPermissions, permDiags := makeDatabasePermissionsFromModel(ctx, p, advancedPermissions)
		diags.Append(permDiags...)
		if diags.HasError() {
			return nil, diags
		}

		dbPermMap[databaseId] = *dbPermissions
	}

	// If the state is passed, it is used to detect removed permissions (or permissions added outside of Terraform).
	// Those permissions are explicitly revoked.
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

			dbPermMap[databaseId] = metabase.NewRevokedDatabasePermissions(advancedPermissions)
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

	// Serialized with the other updates of the graph (see `graphlock.PermissionsGraphTracker`).
	var updateResp *metabase.ReplacePermissionsGraphResponse
	err := r.permissionsGraph.Update(func() error {
		var err error
		updateResp, err = r.client.ReplacePermissionsGraphWithResponse(ctx, *body)
		return err
	})

	resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update permissions graph")...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only the revision is taken from the response, the permissions are the planned ones. Metabase grants default
	// permissions on new databases and to new groups, e.g. created in the same apply, which would otherwise be reported
	// as an inconsistent result. Those are reported as drift when the state is refreshed.
	data.Revision = types.Int64Value(int64(updateResp.JSON200.Revision))

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
