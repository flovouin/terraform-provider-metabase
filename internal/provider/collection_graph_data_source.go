package provider

import (
	"context"
	"fmt"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &CollectionGraphDataSource{}

// Creates a new collection graph data source.
func NewCollectionGraphDataSource() datasource.DataSource {
	return &CollectionGraphDataSource{}
}

// A data source for reading the Metabase collection permissions graph.
type CollectionGraphDataSource struct {
	// The Metabase API client.
	client *metabase.ClientWithResponses
}

// The Terraform model for the collection graph data source.
type CollectionGraphDataSourceModel struct {
	Revision      types.Int64 `tfsdk:"revision"`       // The revision number for the graph, set by Metabase.
	IgnoredGroups types.Set   `tfsdk:"ignored_groups"` // The list of groups that should be ignored when reading permissions.
	Permissions   types.Set   `tfsdk:"permissions"`    // The list of permissions (edges) in the graph.
}

func (d *CollectionGraphDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection_graph"
}

func (d *CollectionGraphDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A data source for reading the current Metabase collection permissions graph.

This data source is useful when managing permissions across multiple Terraform workspaces or when you want to read the current state of permissions without modifying them.

Unlike the resource, this data source only reads the permissions graph and does not make any changes to Metabase.`,

		Attributes: map[string]schema.Attribute{
			"revision": schema.Int64Attribute{
				MarkdownDescription: "The revision number for the graph.",
				Computed:            true,
			},
			"ignored_groups": schema.SetAttribute{
				ElementType:         types.Int64Type,
				MarkdownDescription: "The list of group IDs that should be ignored when reading permissions. By default, this contains the Administrators group (`[2]`).",
				Optional:            true,
			},
			"permissions": schema.SetNestedAttribute{
				MarkdownDescription: "A list of permissions for a given group and collection.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"group": schema.Int64Attribute{
							MarkdownDescription: "The ID of the group to which the permission applies.",
							Computed:            true,
						},
						"collection": schema.StringAttribute{
							MarkdownDescription: "The ID of the collection to which the permission applies.",
							Computed:            true,
						},
						"permission": schema.StringAttribute{
							MarkdownDescription: "The level of permission (`read` or `write`).",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *CollectionGraphDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*metabase.ClientWithResponses)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected client type when configuring Metabase data source.",
			fmt.Sprintf("Expected *metabase.ClientWithResponses, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = client
}

// Updates the given `CollectionGraphDataSourceModel` from the `CollectionPermissionsGraph` returned by the Metabase API.
func updateDataSourceModelFromCollectionPermissionsGraph(ctx context.Context, g metabase.CollectionPermissionsGraph, data *CollectionGraphDataSourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Revision = types.Int64Value(int64(g.Revision))

	ignoredGroups, groupsDiags := getIgnoredPermissionsGroups(ctx, data.IgnoredGroups)
	diags.Append(groupsDiags...)
	if diags.HasError() {
		return diags
	}

	permissionsList := make([]attr.Value, 0)
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

func (d *CollectionGraphDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data CollectionGraphDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := d.client.GetCollectionPermissionsGraphWithResponse(ctx)

	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200}, "read collection graph")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateDataSourceModelFromCollectionPermissionsGraph(ctx, *getResp.JSON200, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
