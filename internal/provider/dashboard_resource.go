package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"github.com/flovouin/terraform-provider-metabase/internal/planmodifiers"
	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithSchema = &DashboardResource{}
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
	CardsIds           types.List   `tfsdk:"cards_ids"`           // The list of IDs for the cards within the dashboard.
}

// The list of JSON attributes in a dashcard that should be persisted in the state.
// Those are also the attributes that users should specify in `cards_json`.
var allowedDashcardAttributes = map[string]bool{
	"card_id":                true,
	"row":                    true,
	"col":                    true,
	"sizeX":                  true,
	"sizeY":                  true,
	"series":                 true,
	"parameter_mappings":     true,
	"visualization_settings": true,
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
			"cards_ids": schema.ListAttribute{
				MarkdownDescription: "The list of IDs for the cards within the dashboard.",
				ElementType:         types.Int64Type,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					// The cards IDs don't change if `cards_json` hasn't changed. Otherwise, cards are recreated.
					planmodifiers.UseStateForUnknownIfAttributeUnchanged[types.String](path.Root("cards_json")),
				},
			},
		},
	}
}

// Returns a raw unmarshalled parameters list from its JSON representation stored in Terraform.
// If the JSON string is null, an empty list is returned.
func makeOpaqueParametersFromTerraform(parametersJson types.String) ([]interface{}, diag.Diagnostics) {
	var diags diag.Diagnostics

	if parametersJson.IsNull() {
		return []interface{}{}, diags
	}

	var parameters []interface{}
	err := json.Unmarshal([]byte(parametersJson.ValueString()), &parameters)
	if err != nil {
		diags.AddError("Failed to deserialize dashboard parameters list.", err.Error())
		return nil, diags
	}

	return parameters, diags
}

// Returns a raw unmarshalled parameters list and the corresponding JSON string from a list of typed parameters.
func makeOpaqueParametersFromTyped(parameters []metabase.DashboardParameter) ([]interface{}, *string, diag.Diagnostics) {
	var diags diag.Diagnostics

	parametersBytes, err := json.Marshal(parameters)
	if err != nil {
		diags.AddError("Failed to serialize dashboard parameters.", err.Error())
		return nil, nil, diags
	}

	var opaqueParameters []interface{}
	err = json.Unmarshal(parametersBytes, &opaqueParameters)
	if err != nil {
		diags.AddError("Failed to deserialize dashboard parameters list.", err.Error())
		return nil, nil, diags
	}

	marshalledParameters := string(parametersBytes)
	return opaqueParameters, &marshalledParameters, diags
}

// Updates the given `DashboardResourceModel` from the `Dashboard` returned by the Metabase API.
// This function updates all basic attributes, but does not handle the `cards_json` definition for cards.
func updateModelFromDashboard(d metabase.Dashboard, data *DashboardResourceModel) diag.Diagnostics {
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

	return diags
}

// Updates the `cards_json` and `cards_ids` attributes in the `DashboardResourceModel` using the raw response from the
// Metabase API.
func updateCardsFromGetDashboardResponse(bytes []byte, data *DashboardResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var jsonResponse map[string]interface{}
	err := json.Unmarshal(bytes, &jsonResponse)
	if err != nil {
		diags.AddError("Unable to parse get dashboard response.", err.Error())
		return diags
	}

	orderedCardsAny, ok := jsonResponse["ordered_cards"]
	if !ok {
		diags.AddError("Unable to retrieve ordered_cards from get dashboard response.", string(bytes))
		return diags
	}

	// Cards must be cast as a list of `interface{}` and not directly a list of maps.
	orderedCards, ok := orderedCardsAny.([]interface{})
	if !ok {
		diags.AddError("Unable to parse ordered_cards as a list from get dashboard response.", string(bytes))
		return diags
	}

	// Parsing each card individually to retrieve their ID and remove unhandled attributes within them.
	cardsIdsList := make([]attr.Value, 0, len(orderedCards))
	for _, c := range orderedCards {
		card, ok := c.(map[string]interface{})
		if !ok {
			diags.AddError("Could not parse ordered card as object.", string(bytes))
			return diags
		}

		cardIdAny, ok := card["id"]
		if !ok {
			diags.AddError("Could not find id in ordered card.", string(bytes))
			return diags
		}

		cardIdFloat, ok := cardIdAny.(float64)
		if !ok {
			diags.AddError("Unable to parse id as number in ordered card.", string(bytes))
			return diags
		}

		cardsIdsList = append(cardsIdsList, types.Int64Value(int64(cardIdFloat)))

		// Removing all unhandled attributes such that the cards returned by the Metabase API can be compared with the
		// `cards_json` in the Terraform state.
		for key := range card {
			if !allowedDashcardAttributes[key] {
				delete(card, key)
			}
		}
	}

	cardsIds, listDiags := types.ListValue(types.Int64Type, cardsIdsList)
	diags.Append(listDiags...)
	if diags.HasError() {
		return diags
	}

	data.CardsIds = cardsIds

	// Unmarshalling `cards_json` from the Terraform state/plan such that it can be compared to Metabase's response.
	var existingCards []interface{}
	if !data.CardsJson.IsNull() {
		err = json.Unmarshal([]byte(data.CardsJson.ValueString()), &existingCards)
		if err != nil {
			diags.AddError("Error deserializing existing cards JSON value.", err.Error())
			return diags
		}
	}

	// If the response of the Metabase API is different, the processed list of cards is marshalled and stored in the
	// state. There is a high chance this will cause an error in Terraform because `cards_json` should not be modified by
	// create / update operations (as it is specified by the user). However this error will make it clear what has
	// happened.
	if !reflect.DeepEqual(orderedCards, existingCards) {
		cardsJson, err := json.Marshal(orderedCards)
		if err != nil {
			diags.AddError("Error serializing new JSON value.", err.Error())
			return diags
		}

		data.CardsJson = types.StringValue(string(cardsJson))
	}

	return diags
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

// Deletes all existing dashcards in the dashboard, based on the cards IDs in the Terraform model.
// This does not delete the referenced cards.
func deleteDashboardCards(ctx context.Context, client metabase.ClientWithResponses, data DashboardResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	dashboardId := int(data.Id.ValueInt64())

	cardsIds := make([]int64, 0, len(data.CardsIds.Elements()))
	listDiags := data.CardsIds.ElementsAs(ctx, &cardsIds, false)
	diags.Append(listDiags...)
	if diags.HasError() {
		return diags
	}

	for _, c := range cardsIds {
		deleteResp, err := client.DeleteDashboardCardWithResponse(ctx, dashboardId, &metabase.DeleteDashboardCardParams{
			DashcardId: int(c),
		})

		diags.Append(checkMetabaseResponse(deleteResp, err, []int{204}, "delete dashcard")...)
		if diags.HasError() {
			return diags
		}
	}

	return diags
}

// Creates all the dashcards in the dashboard using the raw cards JSON string in the Terraform model.
// This assumes that the dashboard does not currently contain any card.
func createDashboardCards(ctx context.Context, client metabase.ClientWithResponses, data *DashboardResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	dashboardId := int(data.Id.ValueInt64())
	cardsJson := data.CardsJson.ValueString()

	var cards []map[string]interface{}
	err := json.Unmarshal([]byte(cardsJson), &cards)
	if err != nil {
		diags.AddError("Unable to parse cards JSON.", err.Error())
		return diags
	}

	// Creates the dashcard for each card in the raw JSON string.
	// IDs returned by the Metabase API are saved such that they can be used when updating the dashboard configuration.
	cardsIdsElements := make([]attr.Value, 0, len(cards))
	for _, c := range cards {
		cardIdAny, ok := c["card_id"]
		if !ok {
			diags.AddError("Failed to get card_id attribute from card.", cardsJson)
			return diags
		}

		var cardId *int

		// A card ID can be null (for example for a text dashcard). However it should always be defined.
		if cardIdAny != nil {
			cardIdFloat, ok := cardIdAny.(float64)
			if !ok {
				diags.AddError("Failed to convert card_id to number.", cardsJson)
				return diags
			}

			cardIdInt := int(cardIdFloat)
			cardId = &cardIdInt
		}

		createResp, err := client.CreateDashboardCardWithResponse(ctx, dashboardId, metabase.CreateDashboardCardBody{
			CardId: cardId,
		})
		diags.Append(checkMetabaseResponse(createResp, err, []int{200}, "create dashcard")...)
		if diags.HasError() {
			return diags
		}

		cardsIdsElements = append(cardsIdsElements, types.Int64Value(int64(createResp.JSON200.Id)))
		// The ID is added to the dashcard definition, which will be used in the next step to configure the dashboard.
		c["id"] = createResp.JSON200.Id
	}

	cardsIds, listDiags := types.ListValue(types.Int64Type, cardsIdsElements)
	diags.Append(listDiags...)
	if diags.HasError() {
		return diags
	}

	data.CardsIds = cardsIds

	// The Metabase API is called with the dashcards definition from the raw JSON string, to which the IDs of the
	// dashcards have been added.
	updatePayload := map[string]interface{}{
		"cards": cards,
	}
	updateBuffer, err := json.Marshal(updatePayload)
	if err != nil {
		diags.AddError("Error creating the payload for dashboard cards update.", err.Error())
		return diags
	}
	updateReader := bytes.NewReader(updateBuffer)
	updateResp, err := client.UpdateDashboardCardsWithBodyWithResponse(ctx, dashboardId, "application/json", updateReader)
	diags.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update dashcards")...)
	if diags.HasError() {
		return diags
	}

	// The API does not return the dashcards definition, so there's nothing to process here.

	return diags
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

	resp.Diagnostics.Append(updateModelFromDashboard(*createResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(createDashboardCards(ctx, *r.client, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
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

	// The API's response contains the dashcards (used in the next operation). However the `updateModelFromDashboard`
	// function receives a dashboard without cards, such that it is returned by other API operations on the dashboard.
	resp.Diagnostics.Append(updateModelFromDashboard(metabase.Dashboard{
		Id:                 getResp.JSON200.Id,
		Name:               getResp.JSON200.Name,
		Description:        getResp.JSON200.Description,
		CollectionId:       getResp.JSON200.CollectionId,
		CollectionPosition: getResp.JSON200.CollectionPosition,
		CacheTtl:           getResp.JSON200.CacheTtl,
		Parameters:         getResp.JSON200.Parameters,
	}, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Cards require more low-level processing and are updated using the raw API response.
	resp.Diagnostics.Append(updateCardsFromGetDashboardResponse(getResp.Body, data)...)
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

	parameters, diags := makeParametersFromModel(ctx, data.ParametersJson)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateResp, err := r.client.UpdateDashboardWithResponse(ctx, int(data.Id.ValueInt64()), metabase.UpdateDashboardBody{
		Name:               valueStringOrNull(data.Name),
		Description:        valueStringOrNull(data.Description),
		CacheTtl:           valueInt64OrNull(data.CacheTtl),
		CollectionId:       valueInt64OrNull(data.CollectionId),
		CollectionPosition: valueInt64OrNull(data.CollectionPosition),
		Parameters:         parameters,
	})
	resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update dashboard")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromDashboard(*updateResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// As updating the dashcards requires several API calls (including deleting existing dashcards), it is only performed
	// if necessary.
	if !data.CardsJson.Equal(state.CardsJson) {
		resp.Diagnostics.Append(deleteDashboardCards(ctx, *r.client, *state)...)
		if resp.Diagnostics.HasError() {
			return
		}

		resp.Diagnostics.Append(createDashboardCards(ctx, *r.client, data)...)
		if resp.Diagnostics.HasError() {
			return
		}
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
