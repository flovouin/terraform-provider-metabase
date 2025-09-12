package provider

import (
	"context"
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

// Updates the given `SettingResourceModel` from the `Setting` returned by the Metabase API.
func updateModelFromSetting(setting metabase.Setting, data *SettingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Id = types.StringValue(setting.Key)
	data.Key = types.StringValue(setting.Key)
	data.Value = types.StringValue(setting.Value)
	data.DefaultValue = types.StringValue(setting.DefaultValue)
	data.Description = stringValueOrNull(setting.Description)

	return diags
}

// Sets the model to represent a setting at its default value (when API returns 204 or 200 with nil JSON).
func setModelToDefaultValue(data *SettingResourceModel) {
	data.Id = data.Key
	data.DefaultValue = data.Value
	data.Description = types.StringNull()
}

func (r *SettingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *SettingResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Update the setting with the provided value
	updateResp, err := r.client.UpdateSettingWithResponse(ctx, data.Key.ValueString(), metabase.UpdateSettingBody{
		Value: data.Value.ValueString(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Unexpected error while calling the Metabase API for operation 'create setting'.", err.Error())
		return
	}

	if updateResp.StatusCode() != 200 && updateResp.StatusCode() != 204 {
		resp.Diagnostics.AddError("Unexpected response while calling the Metabase API for operation 'create setting'.", fmt.Sprintf("Expected status 200 or 204, got %d", updateResp.StatusCode()))
		return
	}

	// If we got 204 (No Content), we need to fetch the setting to get the current state
	if updateResp.StatusCode() == 204 {
		getResp, err := r.client.GetSettingWithResponse(ctx, data.Key.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unexpected error while calling the Metabase API for operation 'get setting after create'.", err.Error())
			return
		}
		if getResp.StatusCode() == 200 {
			if getResp.JSON200 != nil {
				resp.Diagnostics.Append(updateModelFromSetting(*getResp.JSON200, data)...)
			} else {
				// If GET returns 200 but JSON200 is nil, the setting is at its default value
				setModelToDefaultValue(data)
			}
		} else if getResp.StatusCode() == 204 {
			// If GET also returns 204, the setting is at its default value
			setModelToDefaultValue(data)
		} else {
			resp.Diagnostics.AddError("Unexpected response while calling the Metabase API for operation 'get setting after create'.", fmt.Sprintf("Expected status 200 or 204, got %d", getResp.StatusCode()))
			return
		}
	} else {
		if updateResp.JSON200 == nil {
			resp.Diagnostics.AddError("Unexpected response while calling the Metabase API for operation 'create setting'.", "Received nil JSON response")
			return
		}
		resp.Diagnostics.Append(updateModelFromSetting(*updateResp.JSON200, data)...)
	}
	if resp.Diagnostics.HasError() {
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
		resp.Diagnostics.AddError("Unexpected error while calling the Metabase API for operation 'get setting'.", err.Error())
		return
	}

	// If the setting doesn't exist (404), remove it from state
	if getResp.StatusCode() == 404 {
		resp.State.RemoveResource(ctx)
		return
	}

	if getResp.StatusCode() == 200 {
		if getResp.JSON200 != nil {
			resp.Diagnostics.Append(updateModelFromSetting(*getResp.JSON200, data)...)
		} else {
			// If GET returns 200 but JSON200 is nil, the setting is at its default value
			data.Id = data.Key
			data.DefaultValue = data.Value
			data.Description = types.StringNull()
		}
	} else if getResp.StatusCode() == 204 {
		// If GET returns 204, the setting is at its default value
		data.Id = data.Key
		data.DefaultValue = data.Value
		data.Description = types.StringNull()
	} else {
		resp.Diagnostics.AddError("Unexpected response while calling the Metabase API for operation 'get setting'.", fmt.Sprintf("Expected status 200 or 204, got %d", getResp.StatusCode()))
		return
	}
	if resp.Diagnostics.HasError() {
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

	// Update the setting with the new value
	updateResp, err := r.client.UpdateSettingWithResponse(ctx, data.Key.ValueString(), metabase.UpdateSettingBody{
		Value: data.Value.ValueString(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Unexpected error while calling the Metabase API for operation 'update setting'.", err.Error())
		return
	}

	if updateResp.StatusCode() != 200 && updateResp.StatusCode() != 204 {
		resp.Diagnostics.AddError("Unexpected response while calling the Metabase API for operation 'update setting'.", fmt.Sprintf("Expected status 200 or 204, got %d", updateResp.StatusCode()))
		return
	}

	// If we got 204 (No Content), we need to fetch the setting to get the current state
	if updateResp.StatusCode() == 204 {
		getResp, err := r.client.GetSettingWithResponse(ctx, data.Key.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unexpected error while calling the Metabase API for operation 'get setting after update'.", err.Error())
			return
		}
		if getResp.StatusCode() == 200 {
			if getResp.JSON200 != nil {
				resp.Diagnostics.Append(updateModelFromSetting(*getResp.JSON200, data)...)
			} else {
				// If GET returns 200 but JSON200 is nil, the setting is at its default value
				setModelToDefaultValue(data)
			}
		} else if getResp.StatusCode() == 204 {
			// If GET also returns 204, the setting is at its default value
			setModelToDefaultValue(data)
		} else {
			resp.Diagnostics.AddError("Unexpected response while calling the Metabase API for operation 'get setting after update'.", fmt.Sprintf("Expected status 200 or 204, got %d", getResp.StatusCode()))
			return
		}
	} else {
		if updateResp.JSON200 == nil {
			resp.Diagnostics.AddError("Unexpected response while calling the Metabase API for operation 'update setting'.", "Received nil JSON response")
			return
		}
		resp.Diagnostics.Append(updateModelFromSetting(*updateResp.JSON200, data)...)
	}
	if resp.Diagnostics.HasError() {
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
