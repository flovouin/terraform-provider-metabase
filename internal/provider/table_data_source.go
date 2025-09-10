package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &TableDataSource{}

// Creates a new table data source.
func NewTableDataSource() datasource.DataSource {
	return &TableDataSource{}
}

// A data source obtaining details about a table in a database.
// This is useful because Metabase automatically creates table references once a database has been added.
// The Metabase ID for those tables (and their fields) is referenced in cards (questions), which makes them necessary to
// create those other resources.
type TableDataSource struct {
	// The Metabase API client.
	client *metabase.ClientWithResponses
}

// The Terraform model for a table.
type TableDataSourceModel struct {
	Id          types.Int64  `tfsdk:"id"`           // The ID of the table.
	DbId        types.Int64  `tfsdk:"db_id"`        // The ID of the parent database.
	Name        types.String `tfsdk:"name"`         // The name of the table.
	EntityType  types.String `tfsdk:"entity_type"`  // The type of table.
	Schema      types.String `tfsdk:"schema"`       // The database schema in which the table is located. For BigQuery, this is the dataset name.
	DisplayName types.String `tfsdk:"display_name"` // The name displayed in the interface for the table.
	Description types.String `tfsdk:"description"`  // A description for the table.
	Fields      types.Map    `tfsdk:"fields"`       // A map where keys are field (column) names and values are the corresponding Metabase integer IDs.
}

func (d *TableDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_table"
}

func (d *TableDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A Metabase table, part of a parent database.

This data source can be useful to find the Metabase ID of a table based on the table name. Table IDs are required when creating cards (questions) and other resources.

Metabase also assigns an ID to each field (column) in the table. Those are also useful to define dashboard filters for example.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the table. If specified, the `db_id`, `name`, `entity_type`, and `schema` should not be specified.",
				Optional:            true,
			},
			"db_id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the parent database.",
				Optional:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the table.",
				Optional:            true,
			},
			"entity_type": schema.StringAttribute{
				MarkdownDescription: "The type of table.",
				Optional:            true,
			},
			"schema": schema.StringAttribute{
				MarkdownDescription: "The database schema in which the table is located. For BigQuery, this is the dataset name.",
				Optional:            true,
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "The name displayed in the interface for the table.",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description for the table.",
				Computed:            true,
			},
			"fields": schema.MapAttribute{
				MarkdownDescription: "A map where keys are field (column) names and values are their Metabase ID.",
				ElementType:         types.Int64Type,
				Computed:            true,
			},
		},
	}
}

func (d *TableDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*metabase.ClientWithResponses)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected client type when configuring Metabase resource.",
			fmt.Sprintf("Expected *metabase.ClientWithResponses, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = client
}

// Updates the given `TableDataSourceModel` from the `Table` returned by the Metabase API.
func updateModelFromTableMetadata(t metabase.TableMetadata, data *TableDataSourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Id = types.Int64Value(int64(t.Id))
	data.DbId = types.Int64Value(int64(t.DbId))
	data.Name = types.StringValue(t.Name)
	data.EntityType = types.StringValue(t.EntityType)
	data.Schema = stringValueOrNull(t.Schema)
	data.DisplayName = types.StringValue(t.DisplayName)
	data.Description = stringValueOrNull(t.Description)

	fieldsValue, fieldsDiags := makeTableFieldsValue(t)
	diags.Append(fieldsDiags...)
	if diags.HasError() {
		return diags
	}
	data.Fields = *fieldsValue

	return diags
}

func (d *TableDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data TableDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	table, diags := findTableInMetabase(ctx, d.client, tableFilter{
		Id:         data.Id,
		DbId:       data.DbId,
		Name:       data.Name,
		EntityType: data.EntityType,
		Schema:     data.Schema,
	})
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromTableMetadata(*table, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
