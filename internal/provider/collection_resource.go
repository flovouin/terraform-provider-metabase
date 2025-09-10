package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zerogachis/terraform-provider-metabase/internal/planmodifiers"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &CollectionResource{}

// Creates a new collection resource.
func NewCollectionResource() resource.Resource {
	return &CollectionResource{
		MetabaseBaseResource{name: "collection"},
	}
}

// A resource handling a Metabase collection, containing cards (questions) and dashboards.
type CollectionResource struct {
	MetabaseBaseResource
}

// The Terraform model for a collection.
type CollectionResourceModel struct {
	Id          types.String `tfsdk:"id"`          // The ID of the collection.
	Name        types.String `tfsdk:"name"`        // The name of the collection.
	Description types.String `tfsdk:"description"` // A description for the collection.
	Slug        types.String `tfsdk:"slug"`        // The slug used in URLs.
	EntityId    types.String `tfsdk:"entity_id"`   // A unique string identifier.
	Location    types.String `tfsdk:"location"`    // A path-like location, useful for sub-collections.
	ParentId    types.Int64  `tfsdk:"parent_id"`   // The ID of the parent collection, if any.
}

func (r *CollectionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Metabase collection.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The collection ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The collection name.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description for the collection.",
				Optional:            true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "The slug for the collection, used in URLs.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					planmodifiers.UseStateForUnknownIfAttributeUnchanged[types.String](path.Root("name")),
				},
			},
			"entity_id": schema.StringAttribute{
				MarkdownDescription: "A unique string identifier for the collection.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"location": schema.StringAttribute{
				MarkdownDescription: "A path-like location, useful when this is a sub-collection.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					planmodifiers.UseStateForUnknownIfAttributeUnchanged[types.Int64](path.Root("parent_id")),
				},
			},
			"parent_id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the parent collection, if any.",
				Optional:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
		},
	}
}

// Updates the given `CollectionResourceModel` from the `Collection` returned by the Metabase API.
func updateModelFromCollection(col metabase.Collection, data *CollectionResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	// The ID can be a string because of the "root" collection.
	// All user-created collections will have an integer ID.
	if id, err := col.Id.AsCollectionId0(); err == nil {
		data.Id = types.StringValue(id)
	} else if id, err := col.Id.AsCollectionId1(); err == nil {
		data.Id = types.StringValue(fmt.Sprint(id))
	} else {
		marshalled, _ := col.Id.MarshalJSON()
		diags.AddError("Unable to parse collection ID.", string(marshalled))
		return diags
	}

	data.Name = types.StringValue(col.Name)
	data.Description = stringValueOrNull(col.Description)
	data.Slug = stringValueOrNull(col.Slug)
	data.EntityId = stringValueOrNull(col.EntityId)
	data.Location = stringValueOrNull(col.Location)

	// The parent ID is used when posting to the API, but it is not returned.
	// However, it can be inferred from the `location`, which is also a way of checking that the parent was correctly
	// taken into account.
	data.ParentId = types.Int64Null()
	if col.Location != nil && *col.Location != "/" {
		hierarchy := strings.Split(*col.Location, "/")

		for i := len(hierarchy) - 1; i >= 0; i-- {
			parentIdStr := hierarchy[i]
			// Taking the first non-empty substring. Needed because the location is usually something like `/12/`.
			if len(parentIdStr) == 0 {
				continue
			}

			parentId, err := strconv.ParseInt(parentIdStr, 10, 64)
			if err != nil {
				diags.AddError("Unable to parse parent ID from location.", err.Error())
				return diags
			}

			data.ParentId = types.Int64Value(parentId)
			break
		}
	}

	return diags
}

func (r *CollectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *CollectionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createResp, err := r.client.CreateCollectionWithResponse(ctx, metabase.CreateCollectionBody{
		Name:        data.Name.ValueString(),
		Description: valueStringOrNull(data.Description),
		ParentId:    valueInt64OrNull(data.ParentId),
	})

	resp.Diagnostics.Append(checkMetabaseResponse(createResp, err, []int{200}, "create collection")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromCollection(*createResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *CollectionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := r.client.GetCollectionWithResponse(ctx, data.Id.ValueString())

	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200, 404}, "get collection")...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Collections are still accessible by their ID after being archived. However we should treat them as deleted, as this
	// is what the delete operation does.
	if getResp.StatusCode() == 404 || *getResp.JSON200.Archived {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(updateModelFromCollection(*getResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *CollectionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	collectionName := data.Name.ValueString()
	updateResp, err := r.client.UpdateCollectionWithResponse(ctx, data.Id.ValueString(), metabase.UpdateCollectionBody{
		Name:        &collectionName,
		Description: valueStringOrNull(data.Description),
		ParentId:    valueInt64OrNull(data.ParentId),
	})

	resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update collection")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromCollection(*updateResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *CollectionResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	archived := true
	// A collection cannot be deleted, but it can be archived.
	updateResp, err := r.client.UpdateCollectionWithResponse(ctx, data.Id.ValueString(), metabase.UpdateCollectionBody{
		Archived: &archived,
	})

	resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "delete (archive) collection")...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *CollectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
