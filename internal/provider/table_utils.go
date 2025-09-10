package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// A predicate whether a table returned by the Metabase API matches some criteria.
type tablePredicate func(metabase.Table) bool

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

// A filter defining how to find a given table. Terraform values can be null if the attribute should not be used for
// filtering.
type tableFilter struct {
	Id         types.Int64  // The ID of the table.
	DbId       types.Int64  // The ID of the parent database.
	Name       types.String // The name of the table.
	EntityType types.String // The type of table.
	Schema     types.String // The database schema in which the table is located. For BigQuery, this is the dataset name.
}

// Makes a `tablePredicate` that will match tables based on the given Terraform values.
// A predicate can either be an exact match based on the ID of the table, or a search based on one or several table
// attributes.
func makeSearchPredicate(filter tableFilter) (*tablePredicate, diag.Diagnostics) {
	var diags diag.Diagnostics

	idIsSet := !filter.Id.IsNull() && !filter.Id.IsUnknown()
	dbIdIsSet := !filter.DbId.IsNull() && !filter.DbId.IsUnknown()
	nameIsSet := !filter.Name.IsNull() && !filter.Name.IsUnknown()
	entityTypeIsSet := !filter.EntityType.IsNull() && !filter.EntityType.IsUnknown()
	schemaIsSet := !filter.Schema.IsNull() && !filter.Schema.IsUnknown()

	if idIsSet {
		if dbIdIsSet || nameIsSet || entityTypeIsSet || schemaIsSet {
			diags.AddError("No other attribute should be set when the table ID is defined.", "")
			return nil, diags
		}

		id := int(filter.Id.ValueInt64())
		p := tablePredicate(func(t metabase.Table) bool {
			return t.Id == id
		})

		return &p, diags
	}

	if !dbIdIsSet && !nameIsSet && !entityTypeIsSet && !schemaIsSet {
		diags.AddError("At least one attribute is required to lookup the table.", "")
		return nil, diags
	}

	p := tablePredicate(func(t metabase.Table) bool {
		if dbIdIsSet && int(filter.DbId.ValueInt64()) != t.DbId {
			return false
		}

		if nameIsSet && filter.Name.ValueString() != t.Name {
			return false
		}

		if entityTypeIsSet && filter.EntityType.ValueString() != t.EntityType {
			return false
		}

		// The schema attribute can be `null`. Users can pass an empty schema to explicitly search for a null schema.
		if schemaIsSet {
			schema := filter.Schema.ValueString()
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

// Given a predicate, finds a table from the list returned by the Metabase API.
func findTableInMetabase(ctx context.Context, client *metabase.ClientWithResponses, filter tableFilter) (*metabase.TableMetadata, diag.Diagnostics) {
	var diags diag.Diagnostics

	predicate, predicateDiags := makeSearchPredicate(filter)
	diags.Append(predicateDiags...)
	if diags.HasError() {
		return nil, diags
	}

	// Finding the table from the list of all tables in Metabase.
	// The API is not paginated and returns all results in a single response.
	// Also, it does not support query parameters to limit results to what we're searching for.
	listResp, err := client.ListTablesWithResponse(ctx)

	diags.Append(checkMetabaseResponse(listResp, err, []int{200}, "list tables")...)
	if diags.HasError() {
		return nil, diags
	}

	table, diags := findTable(*listResp.JSON200, *predicate)
	diags.Append(diags...)
	if diags.HasError() {
		return nil, diags
	}

	// Querying the found table specifically. The tables returned in the list do not contain information about fields.
	includeHiddenFields := true
	metadataResp, err := client.GetTableMetadataWithResponse(ctx, table.Id, &metabase.GetTableMetadataParams{
		IncludeHiddenFields: &includeHiddenFields,
	})

	diags.Append(checkMetabaseResponse(metadataResp, err, []int{200}, "get table metadata")...)
	if diags.HasError() {
		return nil, diags
	}

	return metadataResp.JSON200, diags
}

// Makes a Terraform map value where keys are field names and values are the corresponding IDs.
func makeTableFieldsValue(t metabase.TableMetadata) (*basetypes.MapValue, diag.Diagnostics) {
	var diags diag.Diagnostics

	fields := make(map[string]attr.Value, len(t.Fields))
	for _, f := range t.Fields {
		fields[f.Name] = types.Int64Value(int64(f.Id))
	}

	fieldsValue, fieldsDiags := types.MapValue(types.Int64Type, fields)
	diags.Append(fieldsDiags...)
	if diags.HasError() {
		return nil, diags
	}

	return &fieldsValue, diags
}
