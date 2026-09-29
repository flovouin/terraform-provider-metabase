package provider

import (
	"context"
	"fmt"
	"strconv"

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
	Permission types.String `tfsdk:"permission"` // The permission level (read or write).
}

func (r *CollectionPermissionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `The permission of a single permissions group on a single collection.

Unlike the ` + "`metabase_collection_graph`" + ` resource, which manages the entire collection graph and resets the permissions it does not define, this resource only manages a single (group, collection) edge of the graph. All other permissions are left untouched, whether they are managed by other ` + "`metabase_collection_permission`" + ` resources, or set outside of Terraform (e.g. in the Metabase interface). This is similar to the relationship between the ` + "`google_*_iam_member`" + ` and ` + "`google_*_iam_policy`" + ` resources of the Google provider.

~> **Warning:** Do not use this resource together with a ` + "`metabase_collection_graph`" + ` resource, and do not define the same (group, collection) pair in several ` + "`metabase_collection_permission`" + ` resources. They would overwrite each other's permissions.

Each change reads the current revision of the collection graph and sends only the managed edge to Metabase. If the graph is modified by someone else in the meantime, the change is retried a few times. Changes made by the provider itself are performed one at a time.

When the resource is deleted, the permission is set to ` + "`none`" + `, which is the same value used by the ` + "`metabase_collection_graph`" + ` resource when removing an edge. If the permission is set to ` + "`none`" + ` outside of Terraform, the resource is considered missing and will be created again.

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
				MarkdownDescription: "The level of permission (`read` or `write`).",
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

// Returns an error if the given permission cannot be set by the resource. `none` is not allowed, as it is the absence
// of permission, which is obtained by deleting the resource.
func checkCollectionPermissionLevel(permission string) diag.Diagnostics {
	var diags diag.Diagnostics

	level := metabase.CollectionPermissionLevel(permission)
	if level != metabase.CollectionPermissionLevelRead && level != metabase.CollectionPermissionLevelWrite {
		diags.AddAttributeError(
			path.Root("permission"),
			"Invalid collection permission.",
			fmt.Sprintf("The permission must be either %q or %q, got: %q.", metabase.CollectionPermissionLevelRead, metabase.CollectionPermissionLevelWrite, permission),
		)
	}

	return diags
}

// Returns the ID of the resource, from the group and collection IDs.
func makeCollectionPermissionId(groupId int64, collectionId string) string {
	return fmt.Sprintf("%d/%s", groupId, collectionId)
}

// Sets the permission for a single edge of the collection graph, leaving all other edges untouched.
func updateCollectionPermission(ctx context.Context, client *metabase.ClientWithResponses, groupId int64, collectionId string, permission metabase.CollectionPermissionLevel) diag.Diagnostics {
	return updateGraphWithRetry(ctx, &collectionGraphMutex, "update collection graph", func() (bool, diag.Diagnostics) {
		getResp, err := client.GetCollectionPermissionsGraphWithResponse(ctx)

		diags := checkMetabaseResponse(getResp, err, []int{200}, "get collection graph")
		if diags.HasError() {
			return false, diags
		}

		// Metabase only updates the edges present in the request.
		updateResp, err := client.ReplaceCollectionPermissionsGraphWithResponse(ctx, metabase.CollectionPermissionsGraph{
			Revision: getResp.JSON200.Revision,
			Groups: map[string]metabase.CollectionPermissionsGraphCollectionPermissionsMap{
				strconv.FormatInt(groupId, 10): {
					collectionId: permission,
				},
			},
		})
		if err == nil && isGraphRevisionConflict(updateResp) {
			return true, diags
		}

		diags.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update collection graph")...)
		return false, diags
	})
}

// Updates the model from the collection graph returned by the Metabase API.
// Returns `false` if the graph does not grant any permission for the model's group and collection.
func (r *CollectionPermissionResource) readCollectionPermission(ctx context.Context, data *CollectionPermissionResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	getResp, err := r.client.GetCollectionPermissionsGraphWithResponse(ctx)

	diags.Append(checkMetabaseResponse(getResp, err, []int{200}, "get collection graph")...)
	if diags.HasError() {
		return false, diags
	}

	groupId := data.Group.ValueInt64()
	collectionId := data.Collection.ValueString()

	permission, ok := getResp.JSON200.Groups[strconv.FormatInt(groupId, 10)][collectionId]
	if !ok || permission == metabase.CollectionPermissionLevelNone {
		return false, diags
	}

	data.Id = types.StringValue(makeCollectionPermissionId(groupId, collectionId))
	data.Permission = types.StringValue(string(permission))

	return true, diags
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

	diags.Append(updateCollectionPermission(ctx, r.client, groupId, collectionId, metabase.CollectionPermissionLevel(data.Permission.ValueString()))...)
	if diags.HasError() {
		return diags
	}

	found, readDiags := r.readCollectionPermission(ctx, data)
	diags.Append(readDiags...)
	if diags.HasError() {
		return diags
	}
	if !found {
		diags.AddError(
			"Permission not found after update.",
			fmt.Sprintf("Metabase did not return the permission for group %d and collection %s after updating it.", groupId, collectionId),
		)
	}

	return diags
}

func (r *CollectionPermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
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

func (r *CollectionPermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *CollectionPermissionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := r.readCollectionPermission(ctx, data)
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

	// Like when an edge is removed from the `metabase_collection_graph` resource, the permission is set to `none`.
	resp.Diagnostics.Append(updateCollectionPermission(ctx, r.client, data.Group.ValueInt64(), data.Collection.ValueString(), metabase.CollectionPermissionLevelNone)...)
}

func (r *CollectionPermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groupId, collectionId, diags := parseGraphEdgeImportId(req.ID, "collection")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(checkGroupIsNotAdministrators(groupId)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data := CollectionPermissionResourceModel{
		Group:      types.Int64Value(groupId),
		Collection: types.StringValue(collectionId),
		Permission: types.StringNull(),
	}

	found, readDiags := r.readCollectionPermission(ctx, &data)
	resp.Diagnostics.Append(readDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Permission not found.",
			fmt.Sprintf("The collection graph does not grant any permission to group %d on collection %s.", groupId, collectionId),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
