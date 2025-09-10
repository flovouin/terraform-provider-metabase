package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &PermissionsGroupResource{}

// Creates a new permissions group resource.
func NewPermissionsGroupResource() resource.Resource {
	return &PermissionsGroupResource{
		MetabaseBaseResource{name: "permissions_group"},
	}
}

// A resource handling a permissions group.
type PermissionsGroupResource struct {
	MetabaseBaseResource
}

// The Terraform model for a permissions group.
type PermissionsGroupResourceModel struct {
	Id   types.Int64  `tfsdk:"id"`   // The ID of the permissions group.
	Name types.String `tfsdk:"name"` // A user-displayable name for the group.
}

func (r *PermissionsGroupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Metabase permissions group.",

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the permissions group.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "A user-displayable name for the group.",
				Required:            true,
			},
		},
	}
}

// Updates the given `PermissionsGroupResourceModel` from the `PermissionsGroup` returned by the Metabase API.
func updateModelFromPermissionsGroup(pg metabase.PermissionsGroup, data *PermissionsGroupResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Id = types.Int64Value(int64(pg.Id))
	data.Name = types.StringValue(pg.Name)

	return diags
}

func (r *PermissionsGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *PermissionsGroupResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createResp, err := r.client.CreatePermissionsGroupWithResponse(ctx, metabase.CreatePermissionsGroupBody{
		Name: data.Name.ValueString(),
	})

	resp.Diagnostics.Append(checkMetabaseResponse(createResp, err, []int{200}, "create permissions group")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromPermissionsGroup(*createResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PermissionsGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *PermissionsGroupResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := r.client.GetPermissionsGroupWithResponse(ctx, int(data.Id.ValueInt64()))

	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200, 204, 404}, "get permissions group")...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The Metabase API can also return "no content" when the group has been deleted.
	if getResp.StatusCode() == 404 || getResp.StatusCode() == 204 {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(updateModelFromPermissionsGroup(*getResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PermissionsGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *PermissionsGroupResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateResp, err := r.client.UpdatePermissionsGroupWithResponse(ctx, int(data.Id.ValueInt64()), metabase.UpdatePermissionsGroupBody{
		Name: data.Name.ValueString(),
	})

	resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update permissions group")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromPermissionsGroup(*updateResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PermissionsGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *PermissionsGroupResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteResp, err := r.client.DeletePermissionsGroupWithResponse(ctx, int(data.Id.ValueInt64()))

	resp.Diagnostics.Append(checkMetabaseResponse(deleteResp, err, []int{204}, "delete permissions group")...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *PermissionsGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importStatePassthroughIntegerId(ctx, req, resp)
}
