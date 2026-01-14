package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &DashboardResource{}

// Creates a new dashboard resource.
func NewDashboardResource() resource.Resource {
	return &DashboardResource{
		MetabaseBaseResource{name: "dashboard"},
	}
}

// A resource handling a Metabase dashboard.
type DashboardResource struct {
	MetabaseBaseResource
}

// The Terraform model for a dashboard.
// Basic attributes are modelled, while the (dash)cards contained in the dashboard are stored as a raw JSON string.
// Cards contain more attributes that can change depending on their type (e.g. text vs. question), and there's no point
// to trying modelling all of them.
type DashboardResourceModel struct {
	Id                 types.Int64  `tfsdk:"id"`                  // The ID of the dashboard.
	Name               types.String `tfsdk:"name"`                // The name of the dashboard.
	CacheTtl           types.Int64  `tfsdk:"cache_ttl"`           // The cache TTL.
	CollectionId       types.Int64  `tfsdk:"collection_id"`       // The ID of the collection in which the dashboard is placed.
	CollectionPosition types.Int64  `tfsdk:"collection_position"` // The position of the dashboard in the collection.
	Description        types.String `tfsdk:"description"`         // A description for the dashboard.
	ParametersJson     types.String `tfsdk:"parameters_json"`     // A list of parameters for the dashboard, that the user can tweak, as a JSON string.
	CardsJson          types.String `tfsdk:"cards_json"`          // The list of cards in the dashboard, as a JSON string.
	TabsJson           types.String `tfsdk:"tabs_json"`           // The list of tabs in the dashboard, as a JSON string.
}

// The list of JSON attributes in a dashcard that should be persisted in the state.
// Those are also the attributes that users should specify in `cards_json`.
var allowedDashcardAttributes = map[string]bool{
	"card_id":                true,
	"row":                    true,
	"col":                    true,
	"size_x":                 true,
	"size_y":                 true,
	"series":                 true,
	"parameter_mappings":     true,
	"visualization_settings": true,
	"dashboard_tab_id":       true,
}

// toInt extracts an integer from an interface{} that could be float64 (from JSON) or int.
func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// sortDashcards sorts a slice of dashcards by their position (tab_id, row, col) for stable comparison.
// This ensures that the order of cards returned by the API doesn't cause spurious diffs.
func sortDashcards(cards []any) {
	sort.Slice(cards, func(i, j int) bool {
		cardI, okI := cards[i].(map[string]any)
		cardJ, okJ := cards[j].(map[string]any)
		if !okI || !okJ {
			return false
		}

		// Sort by tab_id first
		tabI := toInt(cardI["dashboard_tab_id"])
		tabJ := toInt(cardJ["dashboard_tab_id"])
		if tabI != tabJ {
			return tabI < tabJ
		}

		// Then by row
		rowI := toInt(cardI["row"])
		rowJ := toInt(cardJ["row"])
		if rowI != rowJ {
			return rowI < rowJ
		}

		// Then by col
		colI := toInt(cardI["col"])
		colJ := toInt(cardJ["col"])
		return colI < colJ
	})
}

func (r *DashboardResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A Metabase dashboard.

Although a dashboard object is even more complex than a card (question), basic properties are exposed as Terraform attributes. The more complex ones, parameters and cards, are exposed a raw JSON strings. Similarly to cards, templatefile and jsonencode can be used to make the definition more readable.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the dashboard.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "A user-displayable name for the dashboard.",
				Required:            true,
			},
			"cache_ttl": schema.Int64Attribute{
				MarkdownDescription: "The cache TTL.",
				Optional:            true,
			},
			"collection_id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the collection in which the dashboard is placed.",
				Optional:            true,
			},
			"collection_position": schema.Int64Attribute{
				MarkdownDescription: "The position of the dashboard in the collection.",
				Optional:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description for the dashboard.",
				Optional:            true,
			},
			"parameters_json": schema.StringAttribute{
				MarkdownDescription: "A list of parameters for the dashboard, that the user can tweak, as a JSON string.",
				Optional:            true,
			},
			"cards_json": schema.StringAttribute{
				MarkdownDescription: "The list of cards in the dashboard, as a JSON string.",
				Required:            true,
			},
			"tabs_json": schema.StringAttribute{
				MarkdownDescription: "The list of tabs in the dashboard, as a JSON string. Each tab should have an `id` (positive integer, unique within the dashboard) and a `name`. Cards can reference tabs using `dashboard_tab_id` with the same ID.",
				Optional:            true,
			},
		},
	}
}

// Returns a raw unmarshalled parameters list from its JSON representation stored in Terraform.
// If the JSON string is null, an empty list is returned.
func makeOpaqueParametersFromTerraform(parametersJson types.String) ([]any, diag.Diagnostics) {
	var diags diag.Diagnostics

	if parametersJson.IsNull() {
		return []any{}, diags
	}

	var parameters []any
	err := json.Unmarshal([]byte(parametersJson.ValueString()), &parameters)
	if err != nil {
		diags.AddError("Failed to deserialize dashboard parameters list.", err.Error())
		return nil, diags
	}

	return parameters, diags
}

// Returns a raw unmarshalled parameters list and the corresponding JSON string from a list of typed parameters.
func makeOpaqueParametersFromTyped(parameters []metabase.DashboardParameter) ([]any, *string, diag.Diagnostics) {
	var diags diag.Diagnostics

	parametersBytes, err := json.Marshal(parameters)
	if err != nil {
		diags.AddError("Failed to serialize dashboard parameters.", err.Error())
		return nil, nil, diags
	}

	var opaqueParameters []any
	err = json.Unmarshal(parametersBytes, &opaqueParameters)
	if err != nil {
		diags.AddError("Failed to deserialize dashboard parameters list.", err.Error())
		return nil, nil, diags
	}

	marshalledParameters := string(parametersBytes)
	return opaqueParameters, &marshalledParameters, diags
}

// Updates the given `DashboardResourceModel` from the `Dashboard` returned by the Metabase API.
// This includes the update of the `cards_json` attribute, which requires the raw response from the Metabase API.
func updateModelFromDashboardAndRawBody(d metabase.Dashboard, body []byte, data *DashboardResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Id = types.Int64Value(int64(d.Id))
	data.Name = types.StringValue(d.Name)
	data.CacheTtl = int64ValueOrNull(d.CacheTtl)
	data.CollectionId = int64ValueOrNull(d.CollectionId)
	data.CollectionPosition = int64ValueOrNull(d.CollectionPosition)
	data.Description = stringValueOrNull(d.Description)

	// Both the state JSON string and the received typed parameters are converted to untyped parameters lists and compared
	// using `reflect.`
	existingParameters, paramDiags := makeOpaqueParametersFromTerraform(data.ParametersJson)
	diags.Append(paramDiags...)
	if diags.HasError() {
		return diags
	}

	newParameters, marshalledNewParameters, paramDiags := makeOpaqueParametersFromTyped(d.Parameters)
	diags.Append(paramDiags...)
	if diags.HasError() {
		return diags
	}

	if !reflect.DeepEqual(existingParameters, newParameters) {
		// The JSON string is only updated if "real" changes are detected, such that a diff is not detected simply because
		// the Metabase API returns attributes in a different order, or with a different indentation.
		data.ParametersJson = types.StringValue(*marshalledNewParameters)
	}

	// Build a mapping from Metabase tab IDs to user-provided tab IDs.
	// This is needed because we send negative IDs but Metabase returns positive ones.
	tabIdMapping, tabsDiag := updateTabsFromRawBody(body, data)
	diags.Append(tabsDiag...)
	if diags.HasError() {
		return diags
	}

	cardsDiag := updateCardsFromRawBody(body, data, tabIdMapping)
	diags.Append(cardsDiag...)
	if diags.HasError() {
		return diags
	}

	return diags
}

// Updates the `cards_json` attribute in the `DashboardResourceModel` using the raw response from the Metabase API.
// tabIdMapping maps Metabase tab IDs to user-provided tab IDs.
func updateCardsFromRawBody(bytes []byte, data *DashboardResourceModel, tabIdMapping map[int]int) diag.Diagnostics {
	var diags diag.Diagnostics

	var jsonResponse map[string]any
	err := json.Unmarshal(bytes, &jsonResponse)
	if err != nil {
		diags.AddError("Unable to parse get dashboard response.", err.Error())
		return diags
	}

	dashcardsAny, ok := jsonResponse["dashcards"]
	if !ok {
		diags.AddError("Unable to retrieve dashcards from get dashboard response.", string(bytes))
		return diags
	}

	// Cards must be cast as a list of `interface{}` and not directly a list of maps.
	dashcards, ok := dashcardsAny.([]any)
	if !ok {
		diags.AddError("Unable to parse ordered_cards as a list from get dashboard response.", string(bytes))
		return diags
	}

	// Parsing each card individually to remove unhandled attributes within them.
	for _, c := range dashcards {
		card, ok := c.(map[string]any)
		if !ok {
			diags.AddError("Could not parse dashcard as object.", string(bytes))
			return diags
		}

		// Removing all unhandled attributes such that the cards returned by the Metabase API can be compared with the
		// `cards_json` in the Terraform state.
		for key := range card {
			if !allowedDashcardAttributes[key] {
				delete(card, key)
			}
		}

		// Map Metabase tab ID back to user-provided tab ID, or remove if null/unmapped.
		if tabId, ok := card["dashboard_tab_id"]; ok {
			if tabId == nil {
				delete(card, "dashboard_tab_id")
			} else if tabIdFloat, ok := tabId.(float64); ok {
				if userTabId, exists := tabIdMapping[int(tabIdFloat)]; exists {
					card["dashboard_tab_id"] = userTabId
				}
			}
		}
	}

	// Unmarshalling `cards_json` from the Terraform state/plan such that it can be compared to Metabase's response.
	var existingCards []any
	if !data.CardsJson.IsNull() {
		err = json.Unmarshal([]byte(data.CardsJson.ValueString()), &existingCards)
		if err != nil {
			diags.AddError("Error deserializing existing cards JSON value.", err.Error())
			return diags
		}
	}

	// Sort both arrays by position before comparing to avoid spurious diffs due to API returning
	// cards in a different order than provided.
	// Sort cards by position for consistent ordering.
	sortDashcards(dashcards)

	// Always store sorted result so it matches the sorted plan value.
	cardsJson, err := json.Marshal(dashcards)
	if err != nil {
		diags.AddError("Error serializing new JSON value.", err.Error())
		return diags
	}

	data.CardsJson = types.StringValue(string(cardsJson))

	return diags
}

// Updates the `tabs_json` attribute in the `DashboardResourceModel` using the raw response from the Metabase API.
// Returns a mapping from Metabase tab IDs to user-provided tab IDs (based on array position).
func updateTabsFromRawBody(bytes []byte, data *DashboardResourceModel) (map[int]int, diag.Diagnostics) {
	var diags diag.Diagnostics
	tabIdMapping := make(map[int]int)

	var jsonResponse map[string]any
	err := json.Unmarshal(bytes, &jsonResponse)
	if err != nil {
		diags.AddError("Unable to parse get dashboard response.", err.Error())
		return tabIdMapping, diags
	}

	// Parse existing tabs from state to get user-provided IDs.
	var existingTabs []map[string]any
	if !data.TabsJson.IsNull() {
		err = json.Unmarshal([]byte(data.TabsJson.ValueString()), &existingTabs)
		if err != nil {
			diags.AddError("Error deserializing existing tabs JSON value.", err.Error())
			return tabIdMapping, diags
		}
	}

	tabsAny, ok := jsonResponse["tabs"]
	if !ok {
		// Tabs may not be present in older Metabase versions or if there are no tabs.
		// In this case, set tabs_json to null if it was null in the state.
		if data.TabsJson.IsNull() {
			return tabIdMapping, diags
		}
		// If tabs were expected but not returned, set to empty array.
		data.TabsJson = types.StringNull()
		return tabIdMapping, diags
	}

	// Tabs must be cast as a list of `interface{}`.
	tabs, ok := tabsAny.([]any)
	if !ok {
		diags.AddError("Unable to parse tabs as a list from get dashboard response.", string(bytes))
		return tabIdMapping, diags
	}

	// If there are no tabs, set to null.
	if len(tabs) == 0 {
		if !data.TabsJson.IsNull() {
			data.TabsJson = types.StringNull()
		}
		return tabIdMapping, diags
	}

	// Build mapping from Metabase IDs to user IDs and normalize tab data.
	for i, t := range tabs {
		tab, ok := t.(map[string]any)
		if !ok {
			diags.AddError("Could not parse tab as object.", string(bytes))
			return tabIdMapping, diags
		}

		// Build mapping: Metabase ID -> User ID (based on position).
		if metabaseId, ok := tab["id"].(float64); ok {
			if i < len(existingTabs) {
				if userId, ok := existingTabs[i]["id"].(float64); ok {
					tabIdMapping[int(metabaseId)] = int(userId)
					// Replace Metabase ID with user ID in the response.
					tab["id"] = int(userId)
				}
			}
		}

		// Removing all attributes except id and name.
		for key := range tab {
			if key != "id" && key != "name" {
				delete(tab, key)
			}
		}
	}

	// Convert existingTabs to []any for comparison.
	var existingTabsAny []any
	for _, t := range existingTabs {
		existingTabsAny = append(existingTabsAny, t)
	}

	// If the response of the Metabase API is different, update the state.
	if !reflect.DeepEqual(tabs, existingTabsAny) {
		tabsJson, err := json.Marshal(tabs)
		if err != nil {
			diags.AddError("Error serializing new tabs JSON value.", err.Error())
			return tabIdMapping, diags
		}

		data.TabsJson = types.StringValue(string(tabsJson))
	}

	return tabIdMapping, diags
}

// Makes the list of dashboard parameters that can be sent to the Metabase API from a Terraform model.
func makeParametersFromModel(ctx context.Context, model types.String) (*[]metabase.DashboardParameter, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.IsNull() {
		return &[]metabase.DashboardParameter{}, diags
	}

	var parameters []metabase.DashboardParameter
	err := json.Unmarshal([]byte(model.ValueString()), &parameters)
	if err != nil {
		diags.AddError("Failed to serialize dashboard parameters.", err.Error())
		return nil, diags
	}

	return &parameters, diags
}

// Constructs the list of dashboard cards as a type-less list of maps that can be serialized to JSON.
// The IDs of the cards are set to negative values, which will cause the Metabase API to create new cards (and replace the existing ones).
func makeCardsFromModel(model types.String) ([]map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics

	cardsJson := model.ValueString()

	var cards []map[string]any
	err := json.Unmarshal([]byte(cardsJson), &cards)
	if err != nil {
		diags.AddError("Unable to parse cards JSON.", err.Error())
		return nil, diags
	}

	// Existing IDs could be used to update existing cards.
	// For simplicity, new (negative) IDs are used, which will simply replace the existing cards.
	for id, c := range cards {
		c["id"] = -id
		// Negate dashboard_tab_id to match the negated tab IDs.
		if tabId, ok := c["dashboard_tab_id"].(float64); ok {
			c["dashboard_tab_id"] = -int(tabId)
		}
	}

	return cards, diags
}

// Constructs the list of dashboard tabs as a type-less list of maps that can be serialized to JSON.
// The IDs of the tabs are negated, which will cause the Metabase API to create new tabs.
func makeTabsFromModel(model types.String) ([]map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.IsNull() {
		return nil, diags
	}

	tabsJson := model.ValueString()

	var tabs []map[string]any
	err := json.Unmarshal([]byte(tabsJson), &tabs)
	if err != nil {
		diags.AddError("Unable to parse tabs JSON.", err.Error())
		return nil, diags
	}

	// Negate user-provided IDs to signal creation of new tabs.
	for _, t := range tabs {
		if id, ok := t["id"].(float64); ok {
			t["id"] = -int(id)
		}
	}

	return tabs, diags
}

func (r *DashboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *DashboardResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	parameters, diags := makeParametersFromModel(ctx, data.ParametersJson)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createResp, err := r.client.CreateDashboardWithResponse(ctx, metabase.CreateDashboardBody{
		Name:               data.Name.ValueString(),
		Description:        valueStringOrNull(data.Description),
		CacheTtl:           valueInt64OrNull(data.CacheTtl),
		CollectionId:       valueInt64OrNull(data.CollectionId),
		CollectionPosition: valueInt64OrNull(data.CollectionPosition),
		Parameters:         parameters,
	})
	resp.Diagnostics.Append(checkMetabaseResponse(createResp, err, []int{200}, "create dashboard")...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The create dashboard endpoint does not support setting the dashcards. Those must be set by updating the dashboard
	// afterwards.
	updateResp, updateDiags := makeUpdateFromModel(ctx, r.client, createResp.JSON200.Id, *data, "update dashboard during creation")
	resp.Diagnostics.Append(updateDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The entire model can then simply be populated from the update response.
	resp.Diagnostics.Append(updateModelFromDashboardAndRawBody(*updateResp.JSON200, updateResp.Body, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Calls the Metabase API to update a dashboard from a Terraform model.
// This constructs a "raw" payload to handle the serialization of dashcards with a unique ID.
func makeUpdateFromModel(ctx context.Context, client metabase.ClientWithResponsesInterface, dashboardId int, data DashboardResourceModel, operation string) (*metabase.UpdateDashboardResponse, diag.Diagnostics) {
	var diags diag.Diagnostics

	parameters, parametersDiags := makeParametersFromModel(context.Background(), data.ParametersJson)
	diags.Append(parametersDiags...)
	if diags.HasError() {
		return nil, diags
	}

	dashcards, cardsDiags := makeCardsFromModel(data.CardsJson)
	diags.Append(cardsDiags...)
	if diags.HasError() {
		return nil, diags
	}

	tabs, tabsDiags := makeTabsFromModel(data.TabsJson)
	diags.Append(tabsDiags...)
	if diags.HasError() {
		return nil, diags
	}

	updatePayload := map[string]any{
		"name":                valueStringOrNull(data.Name),
		"description":         valueStringOrNull(data.Description),
		"cache_ttl":           valueInt64OrNull(data.CacheTtl),
		"collection_id":       valueInt64OrNull(data.CollectionId),
		"collection_position": valueInt64OrNull(data.CollectionPosition),
		"parameters":          parameters,
		"dashcards":           dashcards,
	}
	if tabs != nil {
		updatePayload["tabs"] = tabs
	}
	updateBuffer, err := json.Marshal(updatePayload)
	if err != nil {
		diags.AddError("Error creating the payload for dashboard update.", err.Error())
		return nil, diags
	}

	updateReader := bytes.NewReader(updateBuffer)
	updateResp, err := client.UpdateDashboardWithBodyWithResponse(ctx, dashboardId, "application/json", updateReader)
	diags.Append(checkMetabaseResponse(updateResp, err, []int{200}, operation)...)
	if diags.HasError() {
		return nil, diags
	}

	return updateResp, diags
}

func (r *DashboardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *DashboardResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := r.client.GetDashboardWithResponse(ctx, int(data.Id.ValueInt64()))
	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200, 404}, "get dashboard")...)
	if resp.Diagnostics.HasError() {
		return
	}

	if getResp.StatusCode() == 404 || getResp.JSON200.Archived {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(updateModelFromDashboardAndRawBody(*getResp.JSON200, getResp.Body, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DashboardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *DashboardResourceModel
	var state *DashboardResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateResp, diags := makeUpdateFromModel(ctx, r.client, int(data.Id.ValueInt64()), *data, "update dashboard")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromDashboardAndRawBody(*updateResp.JSON200, updateResp.Body, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DashboardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *DashboardResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	archived := true
	updateResp, err := r.client.UpdateDashboardWithResponse(ctx, int(data.Id.ValueInt64()), metabase.UpdateDashboardBody{
		Archived: &archived,
	})
	resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "delete (archive) dashboard")...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *DashboardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importStatePassthroughIntegerId(ctx, req, resp)
}
