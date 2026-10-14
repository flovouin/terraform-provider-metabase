package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/flovouin/terraform-provider-metabase/internal/graphlock"
	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &CollectionPermissionResource{}
var _ resource.ResourceWithValidateConfig = &CollectionPermissionResource{}
var _ resource.ResourceWithModifyPlan = &CollectionPermissionResource{}

// Creates a new collection permission resource.
func NewCollectionPermissionResource() resource.Resource {
	return &CollectionPermissionResource{
		MetabaseBaseResource{name: "collection_permission"},
	}
}

// A resource handling the permission of a single group on a single collection, i.e. a single edge of the collection
// graph.
type CollectionPermissionResource struct {
	MetabaseBaseResource
}

// The Terraform model for a single edge of the collection graph.
// Apart from the ID, the attributes are the same as an edge in the `metabase_collection_graph` resource.
type CollectionPermissionResourceModel struct {
	Id         types.String `tfsdk:"id"`         // The ID of the edge, as `<group>/<collection>`.
	Group      types.Int64  `tfsdk:"group"`      // The permissions group to which the permission applies.
	Collection types.String `tfsdk:"collection"` // The collection to which the permission applies. The collection is a string because it could be the `root` collection.
	Permission types.String `tfsdk:"permission"` // The permission level (none, read or write).
}

func (r *CollectionPermissionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `The permission of a single permissions group on a single collection.

Unlike the ` + "`metabase_collection_graph`" + ` resource, which manages the entire collection graph and resets the permissions it does not define, this resource only manages a single (group, collection) edge of the graph. All other permissions are left untouched, whether they are managed by other ` + "`metabase_collection_permission`" + ` resources, or set outside of Terraform (e.g. in the Metabase interface). This is similar to the relationship between the ` + "`google_*_iam_member`" + ` and ` + "`google_*_iam_policy`" + ` resources of the Google provider.

~> **Warning:** Do not use this resource together with a ` + "`metabase_collection_graph`" + ` resource, and do not define the same (group, collection) pair in several ` + "`metabase_collection_permission`" + ` resources. They would overwrite each other's permissions.

Each change reads the current revision of the collection graph and sends only the managed edge to Metabase. Changes made by the provider are performed one at a time, along with the creation and update of collections.

The permission can be set to ` + "`none`" + `, e.g. to make sure a group has no access to a collection, as new collections inherit the permissions of their parent collection. Metabase omits ` + "`none`" + ` from the collection graph, so a pair missing from the graph is read as ` + "`none`" + `. When the group or the collection is deleted, when the collection is archived (like the ` + "`metabase_collection`" + ` resource does), or when the collection is moved inside a personal collection, the resource is considered deleted.

The permissions of personal collections, and of the collections inside them, are fixed and cannot be managed.

When the resource is deleted, the permission is set to ` + "`none`" + `, which is the same value used by the ` + "`metabase_collection_graph`" + ` resource when removing an edge.

Permissions for the Administrators group (ID ` + "`2`" + `) cannot be changed, and will result in an error.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the permission, as `<group>/<collection>`.",
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
			"collection": schema.StringAttribute{
				MarkdownDescription: "The ID of the collection to which the permission applies. This can be `root` for the root collection.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"permission": schema.StringAttribute{
				MarkdownDescription: "The level of permission: `none`, `read` or `write`.",
				Required:            true,
			},
		},
	}
}

func (r *CollectionPermissionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data CollectionPermissionResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The group may only be known during the apply, in which case it is checked when creating the resource.
	if !data.Group.IsNull() && !data.Group.IsUnknown() {
		resp.Diagnostics.Append(checkGroupIsNotAdministrators(data.Group.ValueInt64())...)
	}

	if !data.Permission.IsNull() && !data.Permission.IsUnknown() {
		resp.Diagnostics.Append(checkCollectionPermissionLevel(data.Permission.ValueString())...)
	}
}

// Checks that the group and the collection exist when the resource is created, such that a wrong ID is reported by
// the plan rather than during the apply. They can only change when the resource is created, as they require a
// replacement.
func (r *CollectionPermissionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if !req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var data CollectionPermissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The IDs are unknown when the group or the collection is created in the same apply, in which case they are checked
	// when creating the resource.
	if !data.Group.IsUnknown() {
		resp.Diagnostics.Append(checkPermissionsGroupExists(ctx, r.client, data.Group.ValueInt64())...)
	}
	if !data.Collection.IsUnknown() {
		resp.Diagnostics.Append(checkCollectionStatus(ctx, r.client, data.Collection.ValueString())...)
	}
}

// Returns an error if the given permission is not a collection permission level.
func checkCollectionPermissionLevel(permission string) diag.Diagnostics {
	var diags diag.Diagnostics

	switch metabase.CollectionPermissionLevel(permission) {
	case metabase.CollectionPermissionLevelNone, metabase.CollectionPermissionLevelRead, metabase.CollectionPermissionLevelWrite:
	default:
		diags.AddAttributeError(
			path.Root("permission"),
			"Invalid collection permission.",
			fmt.Sprintf("The permission must be %q, %q or %q, got: %q.", metabase.CollectionPermissionLevelNone, metabase.CollectionPermissionLevelRead, metabase.CollectionPermissionLevelWrite, permission),
		)
	}

	return diags
}

// Sets the permission for a single edge of the collection graph, leaving all other edges untouched. The graph is read
// right before the update. The update records a new revision of the collection graph, so it is tracked like the creation
// of collections (see `graphlock.CollectionGraphTracker`).
func updateCollectionPermission(ctx context.Context, client *metabase.ClientWithResponses, tracker *graphlock.CollectionGraphTracker, groupId int64, collectionId string, permission metabase.CollectionPermissionLevel) diag.Diagnostics {
	var diags diag.Diagnostics

	var updateResp *metabase.ReplaceCollectionPermissionsGraphResponse
	err := tracker.Track(ctx, client, func() (metabase.MetabaseResponse, error) {
		getResp, err := client.GetCollectionPermissionsGraphWithResponse(ctx)

		diags.Append(checkMetabaseResponse(getResp, err, []int{200}, "get collection graph")...)
		if diags.HasError() {
			return nil, errCollectionGraphNotRead
		}

		// Metabase only updates the edges present in the request.
		updateResp, err = client.ReplaceCollectionPermissionsGraphWithResponse(ctx, metabase.CollectionPermissionsGraph{
			Revision: getResp.JSON200.Revision,
			Groups: map[string]metabase.CollectionPermissionsGraphCollectionPermissionsMap{
				strconv.FormatInt(groupId, 10): {
					collectionId: permission,
				},
			},
		})
		return updateResp, err
	})
	// The error of reading the graph is already reported as a diagnostic.
	if errors.Is(err, errCollectionGraphNotRead) {
		return diags
	}

	diags.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update collection graph")...)

	return diags
}

// Returned to the collection graph tracker when the graph could not be read before updating it. The actual error is
// reported as a diagnostic.
var errCollectionGraphNotRead = errors.New("the collection graph could not be read")

// Returns whether the resource is considered deleted, because its group or its collection no longer exists, or because
// its collection was moved inside a personal collection, which fixes its permissions. Archived collections are
// considered deleted, like by the `metabase_collection` resource.
//
// TODO: Archiving a collection moves it to the trash, which can be restored. Its permissions are kept, and still define
// who can see and restore it from the trash, but they can no longer be managed by this resource. They are not part of
// the collection graph, so they cannot be read, although Metabase accepts updating them.
func (r *CollectionPermissionResource) isDeleted(ctx context.Context, data CollectionPermissionResourceModel) (bool, diag.Diagnostics) {
	groupExists, diags := permissionsGroupExists(ctx, r.client, data.Group.ValueInt64())
	if diags.HasError() || !groupExists {
		return !groupExists, diags
	}

	status, diags := getCollectionStatus(ctx, r.client, data.Collection.ValueString())
	return status != collectionStatusActive, diags
}

// Updates the model from the collection graph returned by the Metabase API. Metabase omits `none` from the graph, so a
// pair missing from the graph is read as `none`.
func (r *CollectionPermissionResource) readCollectionPermission(ctx context.Context, data *CollectionPermissionResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	getResp, err := r.client.GetCollectionPermissionsGraphWithResponse(ctx)

	diags.Append(checkMetabaseResponse(getResp, err, []int{200}, "get collection graph")...)
	if diags.HasError() {
		return diags
	}

	groupId := data.Group.ValueInt64()
	collectionId := data.Collection.ValueString()

	permission, ok := getResp.JSON200.Groups[strconv.FormatInt(groupId, 10)][collectionId]
	if !ok {
		permission = metabase.CollectionPermissionLevelNone
	}

	data.Id = types.StringValue(makeGraphEdgeId(groupId, collectionId))
	data.Permission = types.StringValue(string(permission))

	return diags
}

// Sends the permission defined in the plan to Metabase, and updates the plan with the value returned by Metabase.
func (r *CollectionPermissionResource) writeCollectionPermission(ctx context.Context, data *CollectionPermissionResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	groupId := data.Group.ValueInt64()
	collectionId := data.Collection.ValueString()

	diags.Append(checkGroupIsNotAdministrators(groupId)...)
	diags.Append(checkCollectionPermissionLevel(data.Permission.ValueString())...)
	if diags.HasError() {
		return diags
	}

	diags.Append(updateCollectionPermission(ctx, r.client, r.collectionGraph, groupId, collectionId, metabase.CollectionPermissionLevel(data.Permission.ValueString()))...)
	if diags.HasError() {
		return diags
	}

	diags.Append(r.readCollectionPermission(ctx, data)...)

	return diags
}

func (r *CollectionPermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *CollectionPermissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The group and the collection can only change when the resource is created, as they require a replacement. Both
	// are checked, such that all the errors are reported at once.
	resp.Diagnostics.Append(checkPermissionsGroupExists(ctx, r.client, data.Group.ValueInt64())...)
	resp.Diagnostics.Append(checkCollectionStatus(ctx, r.client, data.Collection.ValueString())...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.writeCollectionPermission(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionPermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *CollectionPermissionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleted, diags := r.isDeleted(ctx, *data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if deleted {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(r.readCollectionPermission(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionPermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *CollectionPermissionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.writeCollectionPermission(ctx, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionPermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *CollectionPermissionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// There is no permission to remove when the resource is already considered deleted.
	deleted, diags := r.isDeleted(ctx, *data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() || deleted {
		return
	}

	// Like when an edge is removed from the `metabase_collection_graph` resource, the permission is set to `none`.
	resp.Diagnostics.Append(updateCollectionPermission(ctx, r.client, r.collectionGraph, data.Group.ValueInt64(), data.Collection.ValueString(), metabase.CollectionPermissionLevelNone)...)
}

func (r *CollectionPermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groupId, collectionId, diags := parseGraphEdgeId(req.ID, "collection")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(checkGroupIsNotAdministrators(groupId)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Both are checked, such that all the errors are reported at once.
	resp.Diagnostics.Append(checkPermissionsGroupExists(ctx, r.client, groupId)...)
	resp.Diagnostics.Append(checkCollectionStatus(ctx, r.client, collectionId)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data := CollectionPermissionResourceModel{
		Group:      types.Int64Value(groupId),
		Collection: types.StringValue(collectionId),
		Permission: types.StringNull(),
	}

	resp.Diagnostics.Append(r.readCollectionPermission(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
