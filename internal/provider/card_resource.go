package provider

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// The list of JSON attributes in a Card object that should be persisted in the state. Those are also the attributes
// that are expected in the input JSON definition.
var allowedCardAttributes = map[string]bool{
	"cache_ttl":              true,
	"collection_id":          true,
	"collection_position":    true,
	"dataset_query":          true,
	"description":            true,
	"display":                true,
	"name":                   true,
	"parameter_mappings":     true,
	"parameters":             true,
	"query_type":             true,
	"visualization_settings": true,
}

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &CardResource{}

// Creates a new card resource.
func NewCardResource() resource.Resource {
	return &CardResource{
		MetabaseBaseResource{name: "card"},
	}
}

// A resource handling a Metabase card (question).
type CardResource struct {
	MetabaseBaseResource
}

// The Terraform model for a card.
// Because it is a complex object with many possible attributes, the entire structure is not exposed from Terraform. The
// card's definition should simply be passed as a JSON string, possibly using a template. Only the ID is exposed, as it
// is only known once the card is created.
type CardResourceModel struct {
	Id   types.Int64  `tfsdk:"id"`   // The ID of the card.
	Json types.String `tfsdk:"json"` // The entire definition of the card, as a JSON string.
}

func (r *CardResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A Metabase card (question).

Because the content of a card is complex and can vary a lot between cards, the full schema is not defined in Terraform, and a JSON string should be used instead. You can use templatefile or jsonencode to make the experience smoother.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the card.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"json": schema.StringAttribute{
				MarkdownDescription: "The full card definition as a JSON string.",
				Required:            true,
			},
		},
	}
}

// Parses the (integer) ID of the card from a raw Card JSON object returned by the Metabase API.
func getIdFromRawCard(card map[string]any, strResp string) (types.Int64, diag.Diagnostics) {
	idAny, ok := card["id"]
	if !ok {
		return types.Int64Unknown(), diag.Diagnostics{
			diag.NewErrorDiagnostic(
				"Could not find ID attribute in card response from Metabase API.", strResp,
			),
		}
	}

	idFloat, ok := idAny.(float64)
	if !ok {
		return types.Int64Unknown(), diag.Diagnostics{
			diag.NewErrorDiagnostic(
				"Could not convert card ID attribute to a number.", strResp,
			),
		}
	}

	return types.Int64Value(int64(idFloat)), diag.Diagnostics{}
}

// Removes the `dataset_query.query.aggregation-idents` and `dataset_query.query.breakout-idents` attributes from the
// card if they are not present in the existing card.
func cleanCardQuery(card map[string]any, existingCard map[string]any) {
	if existingCard == nil {
		return
	}

	if _, ok := existingCard["dataset_query"].(map[string]any)["query"].(map[string]any)["aggregation-idents"]; !ok {
		if query, ok := card["dataset_query"].(map[string]any)["query"].(map[string]any); ok {
			delete(query, "aggregation-idents")
		}
	}

	if _, ok := existingCard["dataset_query"].(map[string]any)["query"].(map[string]any)["breakout-idents"]; !ok {
		if query, ok := card["dataset_query"].(map[string]any)["query"].(map[string]any); ok {
			delete(query, "breakout-idents")
		}
	}
}

// Updates the given `CardResourceModel` from the `Card` returned by the Metabase API.
func updateModelFromCardBytes(cardBytes []byte, data *CardResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	// Unmarshalling to a map such that we can perform low-level JSON manipulation on the card.
	var card map[string]any
	err := json.Unmarshal(cardBytes, &card)
	if err != nil {
		diags.AddError("Could not deserialize card response from the Metabase API.", err.Error())
		return diags
	}

	idValue, idDiags := getIdFromRawCard(card, string(cardBytes))
	diags.Append(idDiags...)
	if diags.HasError() {
		return diags
	}
	data.Id = idValue

	// Only keeping the attributes that are expected to be found in the Terraform definition (JSON string) provided by the
	// user. This also removes the `id`, as it is not provided by the user but returned by the Metabase API.
	for key := range card {
		if !allowedCardAttributes[key] {
			delete(card, key)
		}
	}

	// Unmarshals the card from the plan or state, i.e. the known and expected configuration for the card.
	var existingCard map[string]any
	if !data.Json.IsNull() {
		err := json.Unmarshal([]byte(data.Json.ValueString()), &existingCard)
		if err != nil {
			diags.AddError("Error deserializing existing card JSON value.", err.Error())
			return diags
		}
	}

	cleanCardQuery(card, existingCard)

	// If the existing card is different from the response from the API, updates the JSON string by remarshalling the
	// "cleaned" response to a string. This should only happen:
	// - When creating the card.
	// - When reading the card, if it has been modified outside of Terraform (in which case an update will be planned).
	// Any other case (e.g. an inconsistency between the Terraform definition and the Metabase API) will result in a
	// Terraform error.
	if existingCard == nil || !reflect.DeepEqual(card, existingCard) {
		jsonCard, err := json.Marshal(card)
		if err != nil {
			diags.AddError("Error serializing new JSON value.", err.Error())
			return diags
		}

		data.Json = types.StringValue(string(jsonCard))
	}

	return diags
}

func (r *CardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *CardResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyReader := strings.NewReader(data.Json.ValueString())
	createResp, err := r.client.CreateCardWithBodyWithResponse(ctx, "application/json", bodyReader)

	resp.Diagnostics.Append(checkMetabaseResponse(createResp, err, []int{200}, "create card")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromCardBytes(createResp.Body, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *CardResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := r.client.GetCardWithResponse(ctx, int(data.Id.ValueInt64()))

	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200, 404}, "get card")...)
	if resp.Diagnostics.HasError() {
		return
	}

	if getResp.StatusCode() == 404 || getResp.JSON200.Archived {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(updateModelFromCardBytes(getResp.Body, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *CardResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyReader := strings.NewReader(data.Json.ValueString())
	updateResp, err := r.client.UpdateCardWithBodyWithResponse(ctx, int(data.Id.ValueInt64()), "application/json", bodyReader)

	resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update card")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromCardBytes(updateResp.Body, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *CardResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Deletion is deprecated, the card should be archived instead.
	archived := true
	updateResp, err := r.client.UpdateCardWithResponse(ctx, int(data.Id.ValueInt64()), metabase.UpdateCardBody{
		Archived: &archived,
	})

	resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "delete (archive) card")...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *CardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importStatePassthroughIntegerId(ctx, req, resp)
}
