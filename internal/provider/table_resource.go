package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &TableResource{}

// Creates a new table resource.
func NewTableResource() resource.Resource {
	return &TableResource{
		MetabaseBaseResource{name: "table"},
	}
}

// A resource handling an (existing) table.
type TableResource struct {
	MetabaseBaseResource
}

// The Terraform model for a table.
type TableResourceModel struct {
	Id               types.Int64  `tfsdk:"id"`                 // The ID of the table.
	DbId             types.Int64  `tfsdk:"db_id"`              // The ID of the parent database.
	Name             types.String `tfsdk:"name"`               // The name of the table.
	EntityType       types.String `tfsdk:"entity_type"`        // The type of table.
	Schema           types.String `tfsdk:"schema"`             // The database schema in which the table is located. For BigQuery, this is the dataset name.
	DisplayName      types.String `tfsdk:"display_name"`       // The name displayed in the interface for the table.
	Description      types.String `tfsdk:"description"`        // A description for the table.
	Fields           types.Map    `tfsdk:"fields"`             // A map where keys are field (column) names and values are the corresponding Metabase integer IDs.
	ForcedFieldTypes types.Map    `tfsdk:"forced_field_types"` // A map where keys are field (column) names and values are Metabase semantic types. Not all fields have to be specified.
}

func (r *TableResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `An existing Metabase table, part of a parent database.

This resource never creates or deletes tables, as they are managed by Metabase itself. However the table and its fields can be updated.

Instead of being created, the table will be looked up based on its id or a combination of (db_id, name, entity_type, and/or schema). The unspecified attributes will be filled with the values from Metabase's response.

Like its data source counterpart, this resource exposes the ID of the fields (columns) in the table.

The display name and the description of the table can be set. If not specified, the remote values are available instead.

Finally, this resource may define the semantic type for all or a subset of the fields (columns) using the forced_field_types attribute. Only the fields in the map will be updated, all other fields are left as is.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the table. If specified, the `db_id`, `name`, `entity_type`, and `schema` should not be specified.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
					int64planmodifier.RequiresReplace(),
				},
			},
			"db_id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the parent database. If specified, it is used to find the existing table.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
					int64planmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the table. If specified, it is used to find the existing table.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"entity_type": schema.StringAttribute{
				MarkdownDescription: "The type of table. If specified, it is used to find the existing table.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"schema": schema.StringAttribute{
				MarkdownDescription: "The database schema in which the table is located. For BigQuery, this is the dataset name. If specified, it is used to find the existing table.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "The name displayed in the interface for the table.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description for the table.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"fields": schema.MapAttribute{
				MarkdownDescription: "A map where keys are field (column) names and values are their Metabase ID.",
				ElementType:         types.Int64Type,
				Computed:            true,
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"forced_field_types": schema.MapAttribute{
				MarkdownDescription: "A map where keys are field (column) names and values are Metabase semantic types. Not all fields have to be specified.",
				ElementType:         types.StringType,
				Optional:            true,
			},
		},
	}
}

// Updates the given `TableResourceModel` from the `Table` returned by the Metabase API.
func updateModelFromTable(t metabase.TableMetadata, data *TableResourceModel) diag.Diagnostics {
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

	if !data.ForcedFieldTypes.IsNull() {
		// Only the semantic types for the fields referenced in the model before populating it are set.
		forcedFieldTypes := make(map[string]attr.Value, len(data.ForcedFieldTypes.Elements()))
		for fieldName := range data.ForcedFieldTypes.Elements() {
			var field *metabase.Field
			for _, f := range t.Fields {
				if f.Name == fieldName {
					field = &f
					break
				}
			}

			if field == nil {
				diags.AddError("Unable to find field in table definition.", fmt.Sprintf("Field name: %s", fieldName))
				return diags
			}

			forcedFieldTypes[fieldName] = stringValueOrNull(field.SemanticType)
		}

		forcedFieldTypesValue, forceFieldTypesDiags := types.MapValue(types.StringType, forcedFieldTypes)
		diags.Append(forceFieldTypesDiags...)
		if diags.HasError() {
			return diags
		}
		data.ForcedFieldTypes = forcedFieldTypesValue
	}

	return diags
}

func (r *TableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Although this gets information from the plan, it will be updated with the response from the Metabase API when the
	// table if found. This will describe the current state of the table, from which an update can be made if needed.
	var state *TableResourceModel
	var plan *TableResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	table, diags := findTableInMetabase(ctx, r.client, tableFilter{
		Id:         state.Id,
		DbId:       state.DbId,
		Name:       state.Name,
		EntityType: state.EntityType,
		Schema:     state.Schema,
	})
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Keeping a copy of attributes that might have been specified by the user.
	displayName := plan.DisplayName
	description := plan.Description
	forcedFieldTypes := plan.ForcedFieldTypes

	resp.Diagnostics.Append(updateModelFromTable(*table, state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(updateModelFromTable(*table, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Starting from the state, updates the attributes that have been set in the Terraform configuration.
	// However if the attributes have not been specified in Terraform, they should not cause a diff, i.e. all unknown
	// values are instead retrieved from the Metabase response (already in the plan from `updateModelFromTable`).
	if !displayName.IsUnknown() {
		plan.DisplayName = displayName
	}
	if !description.IsUnknown() {
		plan.Description = description
	}
	// This is not a computed field, no need to check for an unknown value.
	plan.ForcedFieldTypes = forcedFieldTypes

	// Now that the table has been "imported" into `state` and the `plan` contains the expected values, a regular update
	// can be performed.
	resp.Diagnostics.Append(r.updateTableIfNeeded(ctx, *state, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *TableResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	includeHiddenFields := true
	getResp, err := r.client.GetTableMetadataWithResponse(ctx, int(data.Id.ValueInt64()), &metabase.GetTableMetadataParams{
		IncludeHiddenFields: &includeHiddenFields,
	})

	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200, 404}, "get table metadata")...)
	if resp.Diagnostics.HasError() {
		return
	}

	if getResp.StatusCode() == 404 {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(updateModelFromTable(*getResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Updates the fields in a table such that they have the expected semantic types.
func (r *TableResource) updateForcedFieldTypes(ctx context.Context, plan TableResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var forcedFieldTypes map[string]*string
	diags.Append(plan.ForcedFieldTypes.ElementsAs(ctx, &forcedFieldTypes, false)...)
	if diags.HasError() {
		return diags
	}

	var fields map[string]int64
	diags.Append(plan.Fields.ElementsAs(ctx, &fields, false)...)
	if diags.HasError() {
		return diags
	}

	for fieldName, semanticType := range forcedFieldTypes {
		fieldId, ok := fields[fieldName]
		if !ok {
			diags.AddError("Unable to find the ID of the field to update.", fmt.Sprintf("Field name: %s", fieldName))
			return diags
		}

		updateResp, err := r.client.UpdateFieldWithResponse(ctx, int(fieldId), metabase.UpdateFieldBody{
			SemanticType: semanticType,
		})

		diags.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update field")...)
		if diags.HasError() {
			return diags
		}
	}

	return diags
}

// Compares the given `state` and `plan`, and update the table and its fields where necessary.
func (r *TableResource) updateTableIfNeeded(ctx context.Context, state TableResourceModel, plan *TableResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if !state.DisplayName.Equal(plan.DisplayName) ||
		!state.Description.Equal(plan.Description) {
		updateResp, err := r.client.UpdateTableWithResponse(ctx, int(plan.Id.ValueInt64()), metabase.UpdateTableBody{
			DisplayName: valueStringOrNull(plan.DisplayName),
			Description: valueStringOrNull(plan.Description),
		})

		diags.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update table")...)
		if diags.HasError() {
			return diags
		}
	}

	if !state.ForcedFieldTypes.Equal(plan.ForcedFieldTypes) {
		diags.Append(r.updateForcedFieldTypes(ctx, *plan)...)
		if diags.HasError() {
			return diags
		}
	}

	// Contrary to other resources, the response of the API to the update operation is not used to populate the Terraform
	// model because it does not contain the list of fields. The "table metadata" has to be fetched again.
	includeHiddenFields := true
	getResp, err := r.client.GetTableMetadataWithResponse(ctx, int(plan.Id.ValueInt64()), &metabase.GetTableMetadataParams{
		IncludeHiddenFields: &includeHiddenFields,
	})

	diags.Append(checkMetabaseResponse(getResp, err, []int{200}, "get table metadata")...)
	if diags.HasError() {
		return diags
	}

	diags.Append(updateModelFromTable(*getResp.JSON200, plan)...)
	if diags.HasError() {
		return diags
	}

	return diags
}

func (r *TableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan *TableResourceModel
	var state *TableResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.updateTableIfNeeded(ctx, *state, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Delete operation is not supported for Metabase tables.", "The table will be left intact.")
}

func (r *TableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importStatePassthroughIntegerId(ctx, req, resp)
}
