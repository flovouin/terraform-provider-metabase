package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &PermissionsGraphDataSource{}

// Creates a new permissions graph data source.
func NewPermissionsGraphDataSource() datasource.DataSource {
	return &PermissionsGraphDataSource{}
}

// A data source for reading the Metabase permissions graph.
type PermissionsGraphDataSource struct {
	// The Metabase API client.
	client *metabase.ClientWithResponses
}

// The Terraform model for the permissions graph data source.
type PermissionsGraphDataSourceModel struct {
	Revision            types.Int64 `tfsdk:"revision"`             // The revision number for the graph, set by Metabase.
	IgnoredGroups       types.Set   `tfsdk:"ignored_groups"`       // The list of groups that should be ignored when reading permissions.
	Permissions         types.Set   `tfsdk:"permissions"`          // The list of permissions (edges) in the graph.
}

func (d *PermissionsGraphDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_permissions_graph"
}

func (d *PermissionsGraphDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A data source for reading the current Metabase permissions graph.

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
				MarkdownDescription: "A list of permissions for a given group and database.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"group": schema.Int64Attribute{
							MarkdownDescription: "The ID of the group to which the permission applies.",
							Computed:            true,
						},
						"database": schema.Int64Attribute{
							MarkdownDescription: "The ID of the database to which the permission applies.",
							Computed:            true,
						},
						"view_data": schema.StringAttribute{
							MarkdownDescription: "The permission definition for data access.",
							Computed:            true,
						},
						"create_queries": schema.StringAttribute{
							MarkdownDescription: "The permission definition for creating queries.",
							Computed:            true,
						},
						"download": schema.SingleNestedAttribute{
							MarkdownDescription: "The permission definition for downloading data.",
							Computed:            true,
							Attributes: map[string]schema.Attribute{
								"schemas": schema.StringAttribute{
									MarkdownDescription: "The permission to access data through the Metabase interface.",
									Computed:            true,
								},
							},
						},
						"data_model": schema.SingleNestedAttribute{
							MarkdownDescription: "The permission definition for accessing the data model.",
							Computed:            true,
							Attributes: map[string]schema.Attribute{
								"schemas": schema.StringAttribute{
									MarkdownDescription: "The permission to access data through the Metabase interface.",
									Computed:            true,
								},
							},
						},
						"details": schema.StringAttribute{
							MarkdownDescription: "The permission definition for accessing details.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *PermissionsGraphDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Makes a single `DatabasePermissions` Terraform object from a Metabase API's response for the data source.
func makeDataSourcePermissionsObjectFromDatabasePermissions(ctx context.Context, groupId int, dbId int, p metabase.PermissionsGraphDatabasePermissions) (*types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	createQueries := metabase.PermissionsGraphDatabasePermissionsCreateQueriesNo
	if p.CreateQueries != nil {
		createQueries = *p.CreateQueries
	}

	downloadAccess, accessDiags := makeAccessPermissionsFromDatabaseAccess(ctx, p.Download)
	diags.Append(accessDiags...)
	if diags.HasError() {
		return nil, diags
	}

	dataModelAccess, accessDiags := makeAccessPermissionsFromDatabaseAccess(ctx, p.DataModel)
	diags.Append(accessDiags...)
	if diags.HasError() {
		return nil, diags
	}

	var viewData string
	if viewDataString, err := p.ViewData.AsPermissionsGraphDatabasePermissionsViewData0(); err == nil {
		viewData = string(viewDataString)
	} else {
		viewDataObject, err := p.ViewData.AsPermissionsGraphDatabasePermissionsViewData1()
		if err != nil {
			diags.AddError("Unexpected permissions value.", err.Error())
			return nil, diags
		}
		// For the data source, we'll just use a string representation
		viewData = fmt.Sprintf("%v", viewDataObject)
	}

	permissionsObject, objectDiags := types.ObjectValueFrom(ctx, databasePermissionsObjectType.AttrTypes, DatabasePermissions{
		Group:         types.Int64Value(int64(groupId)),
		Database:      types.Int64Value(int64(dbId)),
		ViewData:      types.StringValue(viewData),
		CreateQueries: types.StringValue(string(createQueries)),
		Download:      *downloadAccess,
		DataModel:     *dataModelAccess,
		Details:       stringValueOrNull(p.Details),
	})
	diags.Append(objectDiags...)
	if diags.HasError() {
		return nil, diags
	}

	return &permissionsObject, diags
}

// Updates the given `PermissionsGraphDataSourceModel` from the `PermissionsGraph` returned by the Metabase API.
func updateDataSourceModelFromPermissionsGraph(ctx context.Context, g metabase.PermissionsGraph, data *PermissionsGraphDataSourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Revision = types.Int64Value(int64(g.Revision))

	ignoredGroups, groupsDiags := getIgnoredPermissionsGroups(ctx, data.IgnoredGroups)
	diags.Append(groupsDiags...)
	if diags.HasError() {
		return diags
	}

	permissionsList := make([]attr.Value, 0)
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

			dbIdInt, err := strconv.Atoi(dbId)
			if err != nil {
				diags.AddError("Could not convert the database ID to an integer.", err.Error())
				return diags
			}

			permissionsObject, objDiags := makeDataSourcePermissionsObjectFromDatabasePermissions(ctx, groupIdInt, dbIdInt, dbPermissions)
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

func (d *PermissionsGraphDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data PermissionsGraphDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := d.client.GetPermissionsGraphWithResponse(ctx)

	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200}, "read permissions graph")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateDataSourceModelFromPermissionsGraph(ctx, *getResp.JSON200, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
