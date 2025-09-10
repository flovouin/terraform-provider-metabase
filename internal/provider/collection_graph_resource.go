package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &CollectionGraphResource{}

// Creates a new collection graph resource.
func NewCollectionGraphResource() resource.Resource {
	return &CollectionGraphResource{
		MetabaseBaseResource{name: "collection_graph"},
	}
}

// A resource handling the entire permissions graph for Metabase collections.
type CollectionGraphResource struct {
	MetabaseBaseResource
}

// The Terraform model for the graph.
// Instead of representing the graph as a map, it is stored as a list of edges (group ↔️ collection permission).
// This is easier to model using Terraform schemas.
type CollectionGraphResourceModel struct {
	Revision      types.Int64 `tfsdk:"revision"`       // The revision number for the graph, set by Metabase.
	IgnoredGroups types.Set   `tfsdk:"ignored_groups"` // The list of groups that should be ignored when updating permissions.
	Permissions   types.Set   `tfsdk:"permissions"`    // The list of permissions (edges) in the graph.
}

// The model for a single edge in the permissions graph.
type CollectionPermission struct {
	Group      types.Int64  `tfsdk:"group"`      // The permissions group to which the permission applies.
	Collection types.String `tfsdk:"collection"` // The collection to which the permission applies. The collection is a string because it could be the `root` collection.
	Permission types.String `tfsdk:"permission"` // The permission level (read or write).
}

func (r *CollectionGraphResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `The graph of permissions between permissions groups and collections.

Metabase exposes a single resource to define all permissions related to collections. This means a single collection graph resource should be defined in the entire Terraform configuration.

The collection graph cannot be created or deleted. Trying to create it will result in an error. It should be imported instead. Trying to delete the resource will succeed with no impact on Metabase (it is a no-op).

Permissions for the Administrators group cannot be changed. To avoid issues during the update, all permissions for the Administrators group are ignored by default. This behavior can be changed using the ignored groups attribute.`,

		Attributes: map[string]schema.Attribute{
			"revision": schema.Int64Attribute{
				MarkdownDescription: "The revision number for the graph.",
				Computed:            true,
			},
			"ignored_groups": schema.SetAttribute{
				ElementType:         types.Int64Type,
				MarkdownDescription: "The list of group IDs that should be ignored when reading and updating permissions. By default, this contains the Administrators group (`[2]`).",
				Optional:            true,
			},
			"permissions": schema.SetNestedAttribute{
				MarkdownDescription: "A list of permissions for a given group and collection. A (group, collection) pair should appear only once in the list.",
				Required:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"group": schema.Int64Attribute{
							MarkdownDescription: "The ID of the group to which the permission applies.",
							Required:            true,
						},
						"collection": schema.StringAttribute{
							MarkdownDescription: "The ID of the collection to which the permission applies.",
							Required:            true,
						},
						"permission": schema.StringAttribute{
							MarkdownDescription: "The level of permission (`read` or `write`).",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

// Makes a single permission (edge) object to be stored in the model.
func makePermissionObjectFromPermission(ctx context.Context, groupId string, colId string, p metabase.CollectionPermissionLevel) (*types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	// Groups are received as strings because they are keys of a JSON map, but they should all correspond to integers.
	groupIdInt, err := strconv.Atoi(groupId)
	if err != nil {
		diags.AddError("Could not convert group ID to int.", err.Error())
		return nil, diags
	}

	permissionObject, objectDiags := types.ObjectValueFrom(ctx, map[string]attr.Type{
		"group":      types.Int64Type,
		"collection": types.StringType,
		"permission": types.StringType,
	}, CollectionPermission{
		Group:      types.Int64Value(int64(groupIdInt)),
		Collection: types.StringValue(colId),
		Permission: types.StringValue(string(p)),
	})
	diags.Append(objectDiags...)
	if diags.HasError() {
		return nil, diags
	}

	return &permissionObject, diags
}

// Updates the given `CollectionGraphResourceModel` from the `CollectionPermissionsGraph` returned by the Metabase API.
func updateModelFromCollectionPermissionsGraph(ctx context.Context, g metabase.CollectionPermissionsGraph, data *CollectionGraphResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Revision = types.Int64Value(int64(g.Revision))

	ignoredGroups, groupsDiags := getIgnoredPermissionsGroups(ctx, data.IgnoredGroups)
	diags.Append(groupsDiags...)
	if diags.HasError() {
		return diags
	}

	permissionsList := make([]attr.Value, 0, len(data.Permissions.Elements()))
	for groupId, colPermissionsMap := range g.Groups {
		// Permissions for ignored groups are not stored in the state for clarity.
		if ignoredGroups[groupId] {
			continue
		}

		for colId, permission := range colPermissionsMap {
			// Skipping `none` permissions for clarity. Only read or write permissions should be specified.
			if permission == metabase.CollectionPermissionLevelNone {
				continue
			}

			permissionObject, objDiags := makePermissionObjectFromPermission(ctx, groupId, colId, permission)
			diags.Append(objDiags...)
			if diags.HasError() {
				return diags
			}

			permissionsList = append(permissionsList, *permissionObject)
		}
	}

	permissionsSet, setDiags := types.SetValue(types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"group":      types.Int64Type,
			"collection": types.StringType,
			"permission": types.StringType,
		},
	}, permissionsList)
	diags.Append(setDiags...)
	if diags.HasError() {
		return diags
	}

	data.Permissions = permissionsSet

	return diags
}

// Creates the `CollectionPermissionsGraph` to send to the API, based on the Terraform plan, but also the existing state
// (if permissions need to be removed).
func makeCollectionPermissionsGraphFromModel(ctx context.Context, data CollectionGraphResourceModel, state *CollectionGraphResourceModel) (*metabase.CollectionPermissionsGraph, diag.Diagnostics) {
	var diags diag.Diagnostics

	revision := int(data.Revision.ValueInt64())

	permissions := make([]CollectionPermission, 0, len(data.Permissions.Elements()))
	diags.Append(data.Permissions.ElementsAs(ctx, &permissions, false)...)
	if diags.HasError() {
		return nil, diags
	}

	// Creating the permissions map from the plan.
	groups := make(map[string]metabase.CollectionPermissionsGraphCollectionPermissionsMap, len(permissions))
	for _, p := range permissions {
		if p.Group.IsNull() {
			diags.AddError("Unexpected null group in permission.", "")
			return nil, diags
		}
		if p.Collection.IsNull() {
			diags.AddError("Unexpected null collection in permission.", "")
			return nil, diags
		}
		groupId := strconv.FormatInt(p.Group.ValueInt64(), 10)
		collectionId := p.Collection.ValueString()

		colPermMap, ok := groups[groupId]
		if !ok {
			colPermMap = make(metabase.CollectionPermissionsGraphCollectionPermissionsMap)
			groups[groupId] = colPermMap
		}

		_, permExists := colPermMap[collectionId]
		if permExists {
			diags.AddError("Found duplicate permission definition.", fmt.Sprintf("Group ID: %s, Collection ID: %s.", groupId, collectionId))
			return nil, diags
		}

		colPermMap[collectionId] = metabase.CollectionPermissionLevel(p.Permission.ValueString())
	}

	if state != nil {
		// When making the request to the Metabase API, the currently known revision number should be passed.
		// It will be increased and returned by Metabase.
		revision = int(state.Revision.ValueInt64())

		// Comparing with previous permissions, in case some need to be removed.
		statePermissions := make([]CollectionPermission, 0, len(state.Permissions.Elements()))
		diags.Append(state.Permissions.ElementsAs(ctx, &statePermissions, false)...)
		if diags.HasError() {
			return nil, diags
		}

		for _, p := range statePermissions {
			if p.Group.IsNull() {
				diags.AddError("Unexpected null group in permission.", "")
				return nil, diags
			}
			if p.Collection.IsNull() {
				diags.AddError("Unexpected null collection in permission.", "")
				return nil, diags
			}
			groupId := strconv.FormatInt(p.Group.ValueInt64(), 10)
			collectionId := p.Collection.ValueString()

			colPermMap, ok := groups[groupId]
			if !ok {
				colPermMap = make(metabase.CollectionPermissionsGraphCollectionPermissionsMap)
				groups[groupId] = colPermMap
			}

			_, permExists := colPermMap[collectionId]
			if permExists {
				continue
			}

			// If the permission does not exist in the plan but exists in the state, it should be explicitly deleted by
			// creating the permission with a "none" value.
			// Those "none" permissions will not be read back into the state, effectively deleting them.
			colPermMap[collectionId] = metabase.CollectionPermissionLevelNone
		}
	}

	return &metabase.CollectionPermissionsGraph{
		Revision: revision,
		Groups:   groups,
	}, diags
}

func (r *CollectionGraphResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	resp.Diagnostics.AddError("Creating the permissions graph is not allowed, import it instead.", "")
}

func (r *CollectionGraphResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *CollectionGraphResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := r.client.GetCollectionPermissionsGraphWithResponse(ctx)

	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200}, "read collection graph")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromCollectionPermissionsGraph(ctx, *getResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionGraphResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *CollectionGraphResourceModel
	var state *CollectionGraphResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only updating permissions if necessary. The update could have been triggered by `ignored_groups` only.
	if !data.Permissions.Equal(state.Permissions) {
		body, diags := makeCollectionPermissionsGraphFromModel(ctx, *data, state)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		updateResp, err := r.client.ReplaceCollectionPermissionsGraphWithResponse(ctx, *body)

		resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update collection graph")...)
		if resp.Diagnostics.HasError() {
			return
		}

		resp.Diagnostics.Append(updateModelFromCollectionPermissionsGraph(ctx, *updateResp.JSON200, data)...)
		if resp.Diagnostics.HasError() {
			return
		}
	} else {
		// If no update was performed, the current revision number is still valid.
		data.Revision = state.Revision
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionGraphResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"Delete operation is not supported for the Metabase collection permissions graph.",
		"The permission graph has been left intact and is no longer part of the Terraform state.",
	)
}

func (r *CollectionGraphResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	revision, err := strconv.Atoi(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to convert revision to an integer.", req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("revision"), revision)...)
}
