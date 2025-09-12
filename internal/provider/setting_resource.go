package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &SettingResource{}

// Creates a new setting resource.
func NewSettingResource() resource.Resource {
	return &SettingResource{
		MetabaseBaseResource{name: "setting"},
	}
}

// A resource handling a Metabase instance setting.
type SettingResource struct {
	MetabaseBaseResource
}

// The Terraform model for a setting.
type SettingResourceModel struct {
	Id           types.String `tfsdk:"id"`            // A unique identifier for the setting (based on key).
	Key          types.String `tfsdk:"key"`           // The setting key.
	Value        types.String `tfsdk:"value"`         // The current value of the setting.
	DefaultValue types.String `tfsdk:"default_value"` // The default value of the setting (computed).
	Description  types.String `tfsdk:"description"`   // A description of what this setting does (computed).
}

func (r *SettingResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A Metabase instance setting. This resource manages individual settings for a Metabase instance.

When this resource is destroyed, the setting will be reset to its default value. This ensures that removing the resource from Terraform configuration doesn't leave the setting in an unknown state.

~> **Note:** Some Metabase settings require the Enterprise Edition to be configured. Attempting to set these settings on a Community Edition instance will result in an error. Please refer to the [Metabase documentation](https://www.metabase.com/docs/latest/configuring-metabase/settings) for details about which settings require Enterprise Edition.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "A unique identifier for the setting (same as the key).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "The setting key (e.g., 'application-name', 'email-from-address').",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				MarkdownDescription: "The value to set for this setting.",
				Required:            true,
			},
			"default_value": schema.StringAttribute{
				MarkdownDescription: "The default value of the setting, as returned by Metabase.",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description of what this setting does, as returned by Metabase.",
				Computed:            true,
			},
		},
	}
}

// Updates the model from a setting value returned by the API.
func updateModelFromAPIResponse(value *interface{}, key string, data *SettingResourceModel) diag.Diagnostics {
	data.Id = types.StringValue(key)
	data.Key = types.StringValue(key)

	if value == nil {
		// Setting is at default value
		data.DefaultValue = data.Value
		data.Description = types.StringNull()
		return nil
	}

	// Convert any value type to string
	// If it's a complex object (map, slice, etc.), serialize it as JSON
	var valueStr string
	if jsonBytes, err := json.Marshal(*value); err == nil {
		// If it can be marshaled as JSON, use the JSON string
		valueStr = string(jsonBytes)
	} else {
		// Otherwise, use the string representation
		valueStr = fmt.Sprintf("%v", *value)
	}

	// Try to normalize the JSON if it's valid JSON
	if normalized, err := normalizeJSON(valueStr); err == nil {
		valueStr = normalized
	}

	data.Value = types.StringValue(valueStr)
	data.DefaultValue = types.StringValue("")
	data.Description = types.StringValue("Setting value (API returned direct value)")

	return nil
}

// parseValueForAPI parses a string value and returns the appropriate type for the API
// If the string is valid JSON, it returns the parsed JSON object
// Otherwise, it returns the string as-is
func parseValueForAPI(value string) interface{} {
	// Try to parse as JSON
	var jsonValue interface{}
	if err := json.Unmarshal([]byte(value), &jsonValue); err == nil {
		// If it's valid JSON, return the parsed object
		return jsonValue
	}
	// If it's not valid JSON, return the string as-is
	return value
}

// normalizeJSON normalizes a JSON string by parsing and re-marshaling it
// This ensures consistent formatting and field ordering
func normalizeJSON(jsonStr string) (string, error) {
	var jsonValue interface{}
	if err := json.Unmarshal([]byte(jsonStr), &jsonValue); err != nil {
		return "", err
	}

	// Re-marshal with consistent formatting
	normalizedBytes, err := json.Marshal(jsonValue)
	if err != nil {
		return "", err
	}

	return string(normalizedBytes), nil
}

// Helper function to handle setting API responses
func (r *SettingResource) handleSettingResponse(ctx context.Context, updateResp *metabase.UpdateSettingResponse, data *SettingResourceModel, operation string) error {
	if updateResp.StatusCode() != 200 && updateResp.StatusCode() != 204 {
		return fmt.Errorf("unexpected status %d for %s", updateResp.StatusCode(), operation)
	}

	if updateResp.StatusCode() == 200 && updateResp.JSON200 != nil {
		// Direct response with value
		updateModelFromAPIResponse(updateResp.JSON200, data.Key.ValueString(), data)
		return nil
	}

	// Status 204 or 200 with nil JSON - fetch current state
	getResp, err := r.client.GetSettingWithResponse(ctx, data.Key.ValueString())
	if err != nil {
		return fmt.Errorf("failed to get setting after %s: %w", operation, err)
	}

	if getResp.StatusCode() == 200 {
		updateModelFromAPIResponse(getResp.JSON200, data.Key.ValueString(), data)
	} else if getResp.StatusCode() == 204 {
		updateModelFromAPIResponse(nil, data.Key.ValueString(), data)
	} else {
		return fmt.Errorf("unexpected status %d when getting setting", getResp.StatusCode())
	}

	return nil
}

func (r *SettingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *SettingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update the setting
	updateResp, err := r.client.UpdateSettingWithResponse(ctx, data.Key.ValueString(), metabase.UpdateSettingBody{
		Value: parseValueForAPI(data.Value.ValueString()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create setting", err.Error())
		return
	}

	// Handle response and update model
	if err := r.handleSettingResponse(ctx, updateResp, data, "create"); err != nil {
		resp.Diagnostics.AddError("Failed to handle setting response", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SettingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *SettingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := r.client.GetSettingWithResponse(ctx, data.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read setting", err.Error())
		return
	}

	if getResp.StatusCode() == 404 {
		resp.State.RemoveResource(ctx)
		return
	}

	if getResp.StatusCode() == 200 {
		updateModelFromAPIResponse(getResp.JSON200, data.Key.ValueString(), data)
	} else if getResp.StatusCode() == 204 {
		updateModelFromAPIResponse(nil, data.Key.ValueString(), data)
	} else {
		resp.Diagnostics.AddError("Unexpected response", fmt.Sprintf("Expected status 200 or 204, got %d", getResp.StatusCode()))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SettingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *SettingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update the setting
	updateResp, err := r.client.UpdateSettingWithResponse(ctx, data.Key.ValueString(), metabase.UpdateSettingBody{
		Value: parseValueForAPI(data.Value.ValueString()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to update setting", err.Error())
		return
	}

	// Handle response and update model
	if err := r.handleSettingResponse(ctx, updateResp, data, "update"); err != nil {
		resp.Diagnostics.AddError("Failed to handle setting response", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SettingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *SettingResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Reset the setting to its default value
	updateResp, err := r.client.UpdateSettingWithResponse(ctx, data.Key.ValueString(), metabase.UpdateSettingBody{
		Value: data.DefaultValue.ValueString(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Unexpected error while calling the Metabase API for operation 'delete (reset) setting'.", err.Error())
		return
	}

	if updateResp.StatusCode() != 200 && updateResp.StatusCode() != 204 && updateResp.StatusCode() != 404 {
		resp.Diagnostics.AddError("Unexpected response while calling the Metabase API for operation 'delete (reset) setting'.", fmt.Sprintf("Expected status 200, 204, or 404, got %d", updateResp.StatusCode()))
		return
	}
}

func (r *SettingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The import ID is the setting key
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
