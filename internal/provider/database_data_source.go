package provider

import (
	"context"
	"fmt"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &DatabaseDataSource{}

// Creates a new database data source.
func NewDatabaseDataSource() datasource.DataSource {
	return &DatabaseDataSource{}
}

// A data source obtaining details about a database.
type DatabaseDataSource struct {
	// The Metabase API client.
	client *metabase.ClientWithResponses
}

// The Terraform model for a database.
type DatabaseDataSourceModel struct {
	Id   types.Int64  `tfsdk:"id"`   // The ID of the database.
	Name types.String `tfsdk:"name"` // The name of the database.
}

func (d *DatabaseDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

func (d *DatabaseDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A Metabase database.

This data source can be useful to find the Metabase ID of a database based on the database name.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the database. If specified, the `name` should not be specified.",
				Optional:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the database. If specified, the `id` should not be specified.",
				Optional:            true,
			},
		},
	}
}

func (d *DatabaseDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *DatabaseDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data DatabaseDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database, diags := findDatabaseInMetabase(ctx, d.client, databaseFilter{
		Id:   data.Id,
		Name: data.Name,
	})
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Id = types.Int64Value(int64(database.Id))
	data.Name = types.StringValue(database.Name)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// A predicate whether a database returned by the Metabase API matches some criteria.
type databasePredicate func(metabase.Database) bool

// Finds a specific database in the given list based on a predicate.
func findDatabase(databases []metabase.Database, p databasePredicate) (*metabase.Database, diag.Diagnostics) {
	var diags diag.Diagnostics

	for _, d := range databases {
		if p(d) {
			return &d, diags
		}
	}

	diags.AddError("Unable to find the database given its attributes.", "")
	return nil, diags
}

// A filter defining how to find a given database. Terraform values can be null if the attribute should not be used for
// filtering.
type databaseFilter struct {
	Id   types.Int64  // The ID of the database.
	Name types.String // The name of the database.
}

// Makes a `databasePredicate` that will match databases based on the given Terraform values.
func makeDatabaseSearchPredicate(filter databaseFilter) (*databasePredicate, diag.Diagnostics) {
	var diags diag.Diagnostics

	idIsSet := !filter.Id.IsNull() && !filter.Id.IsUnknown()
	nameIsSet := !filter.Name.IsNull() && !filter.Name.IsUnknown()

	if idIsSet {
		if nameIsSet {
			diags.AddError("No other attribute should be set when the database ID is defined.", "")
			return nil, diags
		}

		id := int(filter.Id.ValueInt64())
		p := databasePredicate(func(d metabase.Database) bool {
			return d.Id == id
		})

		return &p, diags
	}

	if !nameIsSet {
		diags.AddError("At least one attribute is required to lookup the database.", "")
		return nil, diags
	}

	p := databasePredicate(func(d metabase.Database) bool {
		return filter.Name.ValueString() == d.Name
	})

	return &p, diags
}

// Given a predicate, finds a database from the list returned by the Metabase API.
func findDatabaseInMetabase(ctx context.Context, client *metabase.ClientWithResponses, filter databaseFilter) (*metabase.Database, diag.Diagnostics) {
	var diags diag.Diagnostics

	predicate, predicateDiags := makeDatabaseSearchPredicate(filter)
	diags.Append(predicateDiags...)
	if diags.HasError() {
		return nil, diags
	}

	// Finding the database from the list of all databases in Metabase.
	listResp, err := client.ListDatabasesWithResponse(ctx, &metabase.ListDatabasesParams{})

	diags.Append(checkMetabaseResponse(listResp, err, []int{200}, "list databases")...)
	if diags.HasError() {
		return nil, diags
	}

	database, diags := findDatabase(listResp.JSON200.Data, *predicate)
	diags.Append(diags...)
	if diags.HasError() {
		return nil, diags
	}

	return database, diags
}
