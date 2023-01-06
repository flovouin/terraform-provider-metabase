package provider

import (
	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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

	if !filter.Id.IsNull() {
		if !filter.DbId.IsNull() ||
			!filter.Name.IsNull() ||
			!filter.EntityType.IsNull() ||
			!filter.Schema.IsNull() {
			diags.AddError("No other attribute should be set when the table ID is defined.", "")
			return nil, diags
		}

		id := int(filter.Id.ValueInt64())
		p := tablePredicate(func(t metabase.Table) bool {
			return t.Id == id
		})

		return &p, diags
	}

	if filter.DbId.IsNull() &&
		filter.Name.IsNull() &&
		filter.EntityType.IsNull() &&
		filter.Schema.IsNull() {
		diags.AddError("At least one attribute is required to lookup the table.", "")
		return nil, diags
	}

	p := tablePredicate(func(t metabase.Table) bool {
		if !filter.DbId.IsNull() && int(filter.DbId.ValueInt64()) != t.DbId {
			return false
		}

		if !filter.Name.IsNull() && filter.Name.ValueString() != t.Name {
			return false
		}

		if !filter.EntityType.IsNull() && filter.EntityType.ValueString() != t.EntityType {
			return false
		}

		// The schema attribute can be `null`. Users can pass an empty schema to explicitly search for a null schema.
		if !filter.Schema.IsNull() {
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
