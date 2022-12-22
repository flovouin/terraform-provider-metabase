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
	DisplayName types.String `tfsdk:"display_name"` // The name displayed in the interface for the table.
	EntityType  types.String `tfsdk:"entity_type"`  // The type of table.
	Schema      types.String `tfsdk:"schema"`       // The database schema in which the table is located. For BigQuery, this is the dataset name.
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
			"display_name": schema.StringAttribute{
				MarkdownDescription: "The name displayed in the interface for the table.",
				Computed:            true,
			},
			"entity_type": schema.StringAttribute{
				MarkdownDescription: "The type of table.",
				Optional:            true,
			},
			"schema": schema.StringAttribute{
				MarkdownDescription: "The database schema in which the table is located. For BigQuery, this is the dataset name.",
				Optional:            true,
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
	data.DisplayName = stringValueOrNull(t.DisplayName)
	data.EntityType = types.StringValue(t.EntityType)
	data.Schema = stringValueOrNull(t.Schema)

	// The Metabase API returns a full definition of each field. Only the integer IDs are exposed, as this is what is
	// needed to define dashboard filters for example.
	fields := make(map[string]attr.Value, len(t.Fields))
	for _, f := range t.Fields {
		fields[f.Name] = types.Int64Value(int64(f.Id))
	}
	fieldsValue, fieldsDiags := types.MapValue(types.Int64Type, fields)
	diags.Append(fieldsDiags...)
	if diags.HasError() {
		return diags
	}
	data.Fields = fieldsValue

	return diags
}

// Finds a specific table in the given list based on a predicate.
func findTable(tables []metabase.Table, p tablePredicate) (*metabase.Table, diag.Diagnostics) {
	var diags diag.Diagnostics

	for _, t := range tables {
		if p(t) {
			return &t, diags
		}
	}

	diags.AddError("Unable to find the table given its attributes.", "")
	return nil, diags
}

// A predicate whether a table returned by the Metabase API matches some criteria.
type tablePredicate func(metabase.Table) bool

// Makes a `tablePredicate` that will match tables based on the Terraform model for the data source.
// A predicate can either be an exact match based on the ID of the table, or a search based on one or several table
// attributes.
func makeSearchPredicateFromTableDataSourceModel(data TableDataSourceModel) (*tablePredicate, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !data.Id.IsNull() {
		if !data.DbId.IsNull() ||
			!data.Name.IsNull() ||
			!data.EntityType.IsNull() ||
			!data.Schema.IsNull() {
			diags.AddError("No other attribute should be set when the table ID is defined.", "")
			return nil, diags
		}

		id := int(data.Id.ValueInt64())
		p := tablePredicate(func(t metabase.Table) bool {
			return t.Id == id
		})

		return &p, diags
	}

	if data.DbId.IsNull() &&
		data.Name.IsNull() &&
		data.EntityType.IsNull() &&
		data.Schema.IsNull() {
		diags.AddError("At least one attribute is required to lookup the table.", "")
		return nil, diags
	}

	p := tablePredicate(func(t metabase.Table) bool {
		if !data.DbId.IsNull() && int(data.DbId.ValueInt64()) != t.DbId {
			return false
		}

		if !data.Name.IsNull() && data.Name.ValueString() != t.Name {
			return false
		}

		if !data.EntityType.IsNull() && data.EntityType.ValueString() != t.EntityType {
			return false
		}

		// The schema attribute can be `null`. Users can pass an empty schema to explicitly search for a null schema.
		if !data.Schema.IsNull() {
			schema := data.Schema.ValueString()
			schemaIsEmpty := len([]rune(schema)) == 0
			tableSchemaIsEmptyOrNull := t.Schema == nil || len([]rune(*t.Schema)) == 0
			if schemaIsEmpty != tableSchemaIsEmptyOrNull ||
				(!schemaIsEmpty && !tableSchemaIsEmptyOrNull && schema != *t.Schema) {
				return false
			}
		}

		return true
	})

	return &p, diags
}

func (d *TableDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data TableDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	predicate, diags := makeSearchPredicateFromTableDataSourceModel(data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Finding the table from the list of all tables in Metabase.
	// The API is not paginated and returns all results in a single response.
	// Also, it does not support query parameters to limit results to what we're searching for.
	listResp, err := d.client.ListTablesWithResponse(ctx)

	resp.Diagnostics.Append(checkMetabaseResponse(listResp, err, []int{200}, "list tables")...)
	if resp.Diagnostics.HasError() {
		return
	}

	table, diags := findTable(*listResp.JSON200, *predicate)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Querying the found table specifically. The tables returned in the list do not contain information about fields.
	includeHiddenFields := true
	metadataResp, err := d.client.GetTableMetadataWithResponse(ctx, table.Id, &metabase.GetTableMetadataParams{IncludeHiddenFields: &includeHiddenFields})

	resp.Diagnostics.Append(checkMetabaseResponse(metadataResp, err, []int{200}, "get table metadata")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromTableMetadata(*metadataResp.JSON200, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
