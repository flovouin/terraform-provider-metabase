package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"text/template"

	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// The template producing a `metabase_dashboard` Terraform resource definition.
const dashboardTemplate = `resource "metabase_dashboard" "{{.TerraformSlug}}" {
  name                = {{.Name}}
  description         = {{if .Description}}{{.Description}}{{else}}null{{end}}
  cache_ttl           = {{if .CacheTtl}}{{.CacheTtl}}{{else}}null{{end}}
  collection_id       = {{if .CollectionRef}}metabase_collection.{{.CollectionRef}}.id{{else}}null{{end}}
  collection_position = {{if .CollectionPosition}}{{.CollectionPosition}}{{else}}null{{end}}

  parameters_json = jsonencode({{.ParametersHcl}})

  cards_json = jsonencode({{.CardsHcl}})
}
`

// The data required to produce a `metabase_dashboard` Terraform resource definition.
type dashboardTemplateData struct {
	TerraformSlug      string  // The slug used as the name of the Terraform resource.
	Name               string  // The name of the dashboard.
	Description        *string // The description of the dashboard.
	CacheTtl           *int    // The TTL for the cache.
	CollectionRef      *string // The reference to the collection where the dashboard is located.
	CollectionPosition *int    // The position in the collection.
	ParametersHcl      string  // The dashboard parameters, as an HCL string.
	CardsHcl           string  // The dashboard cards, as an HCL string, possibly referencing cards.
}

// Replaces the reference to a card in a single "dashcard" by an `importedCard`.
func (ic *ImportContext) insertCardReference(ctx context.Context, obj map[string]any) error {
	cardIdAny, ok := obj[metabase.CardIdAttribute]
	if !ok {
		return errors.New("unable to find card_id in object")
	}

	if cardIdAny == nil {
		// A `null` `card_id` is acceptable, e.g. for "virtual cards" displaying markdown text.
		return nil
	}

	cardIdFloat, ok := cardIdAny.(float64)
	if !ok {
		return errors.New("unable to convert card_id to number")
	}

	cardId := int(cardIdFloat)
	importedCard, err := ic.importCard(ctx, cardId)
	if err != nil {
		return err
	}

	obj[metabase.CardIdAttribute] = importedCard

	return nil
}

// Replaces all references to cards and fields in a "dashcard" by their `imported*` counterpart.
func (ic *ImportContext) insertReferencesInCard(ctx context.Context, card map[string]any) error {
	// The dashcard has a `card_id` at its root that should be replaced.
	err := ic.insertCardReference(ctx, card)
	if err != nil {
		return err
	}

	mappingsAny, ok := card[metabase.ParameterMappingsAttribute]
	if !ok || mappingsAny == nil {
		// `parameters_mappings` should be present, but we can tolerate it not being there or being `null`.
		return nil
	}

	mappings, ok := mappingsAny.([]any)
	if !ok {
		return errors.New("unable to convert parameter_mappings to array in dashboard card")
	}

	for _, m := range mappings {
		mapping, ok := m.(map[string]any)
		if !ok {
			return errors.New("unable to convert parameter mapping to object in dashboard card")
		}

		// Each mapping has a reference to the same card as the dashcard.
		err := ic.insertCardReference(ctx, mapping)
		if err != nil {
			return err
		}

		// The target contains a reference to the field (column) the dashboard parameter applies to.
		targetAny, ok := mapping[metabase.TargetAttribute]
		if !ok {
			continue
		}

		target, ok := targetAny.([]any)
		if !ok {
			// For all we know, `target` might have a different structure.
			continue
		}

		err = ic.insertFieldReferencesRecursively(ctx, target)
		if err != nil {
			return err
		}
	}

	return nil
}

// Converts the list of "dashcards" to HCL, and replaces the references to card IDs by their corresponding Terraform
// resources.
func (ic *ImportContext) makeDashboardCardsHcl(ctx context.Context, cards []metabase.DashboardCard) (*string, error) {
	cardsJson, err := json.Marshal(cards)
	if err != nil {
		return nil, err
	}

	// Using the base unmarshalling without typing actually makes it easier to replace card IDs with `importedCard`s.
	var cardsUntyped []any
	err = json.Unmarshal(cardsJson, &cardsUntyped)
	if err != nil {
		return nil, err
	}

	for _, c := range cardsUntyped {
		card, ok := c.(map[string]any)
		if !ok {
			return nil, errors.New("unable to parse dashboard card")
		}

		err = ic.insertReferencesInCard(ctx, card)
		if err != nil {
			return nil, err
		}

		delete(card, "id")
	}

	cardsJson, err = json.MarshalIndent(cardsUntyped, "  ", "  ")
	if err != nil {
		return nil, err
	}

	hcl := replacePlaceholders(string(cardsJson))

	return &hcl, nil
}

// Produces the Terraform definition for a `metabase_dashboard` resource.
func (ic *ImportContext) makeDashboardHcl(ctx context.Context, dashboard metabase.Dashboard, slug string) (*string, error) {
	tpl, err := template.New("dashboard").Parse(dashboardTemplate)
	if err != nil {
		return nil, err
	}

	// Parameters should not contain references to tables or fields, and can be converted to JSON/HCL as is.
	// Their ID is only used within the dashboard itself, and it is not the ID of an object in the Metabase API / DB.
	parametersStr, err := json.MarshalIndent(dashboard.Parameters, "  ", "  ")
	if err != nil {
		return nil, err
	}

	cardsHcl, err := ic.makeDashboardCardsHcl(ctx, dashboard.Dashcards)
	if err != nil {
		return nil, err
	}

	// Converting strings to JSON ensures special characters are escaped.
	name, err := json.Marshal(dashboard.Name)
	if err != nil {
		return nil, err
	}

	var description *string
	if dashboard.Description != nil {
		descriptionBytes, err := json.Marshal(*dashboard.Description)
		if err != nil {
			return nil, err
		}

		descriptionStr := string(descriptionBytes)
		description = &descriptionStr
	}

	var collectionRef *string
	if dashboard.CollectionId != nil {
		collectionId := fmt.Sprint(*dashboard.CollectionId)
		collection, err := ic.getCollection(collectionId)
		if err != nil {
			return nil, err
		}

		collectionRef = &collection.Slug
	}

	buf := new(bytes.Buffer)
	err = tpl.Execute(buf, dashboardTemplateData{
		TerraformSlug:      slug,
		Name:               string(name),
		Description:        description,
		CacheTtl:           dashboard.CacheTtl,
		CollectionRef:      collectionRef,
		CollectionPosition: dashboard.CollectionPosition,
		ParametersHcl:      string(parametersStr),
		CardsHcl:           *cardsHcl,
	})
	if err != nil {
		return nil, err
	}

	hcl := buf.String()

	return &hcl, nil
}

// Fetches a dashboard from the Metabase API and produces the corresponding Terraform definition.
func (ic *ImportContext) ImportDashboard(ctx context.Context, dashboardId int) (*importedDashboard, error) {
	dashboard, ok := ic.dashboards[dashboardId]
	if ok {
		return &dashboard, nil
	}

	getResp, err := ic.client.GetDashboardWithResponse(ctx, dashboardId)
	if err != nil {
		return nil, err
	}
	if getResp.JSON200 == nil {
		return nil, errors.New("unexpected response from the Metabase API when fetching dashboard")
	}

	slug := makeUniqueSlug(getResp.JSON200.Name, ic.dashboardsSlugs)

	hcl, err := ic.makeDashboardHcl(ctx, *getResp.JSON200, slug)
	if err != nil {
		return nil, err
	}

	dashboard = importedDashboard{
		Dashboard: *getResp.JSON200,
		Slug:      slug,
		Hcl:       *hcl,
	}

	ic.dashboards[dashboardId] = dashboard

	return &dashboard, nil
}
