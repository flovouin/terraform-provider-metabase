package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"text/template"

	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// The template producing a `metabase_card` Terraform resource definition.
const cardTemplate = `resource "metabase_card" "{{.TerraformSlug}}" {
  json = jsonencode({{.Json}})
}
`

// The data required to produce a `metabase_card` Terraform resource definition.
type cardTemplateData struct {
	TerraformSlug string // The slug used as the name of the Terraform resource.
	Json          string // The content of the card, as a JSON string.
}

// Replaces table integer IDs by references to Terraform `metabase_table` data sources.
// A card may contain `source-table` attributes with a value which is a (integer) table ID.
// For each of those attributes, the table is looked up, imported, and referenced by replacing the value with an
// `importedTable`.
func (ic *ImportContext) insertCardTableReferenceRecursively(ctx context.Context, obj any) error {
	switch typedObj := obj.(type) {
	case map[string]any:
		for k, i := range typedObj {
			if k == metabase.SourceTableAttribute {
				tableIdFloat, ok := i.(float64)
				if !ok {
					return errors.New("failed to unmarshal \"source-table\" field to float")
				}

				importedTable, err := ic.importTable(ctx, int(tableIdFloat))
				if err != nil {
					return nil
				}

				typedObj[k] = importedTable
				continue
			}

			err := ic.insertCardTableReferenceRecursively(ctx, i)
			if err != nil {
				return nil
			}
		}

		return nil
	case []any:
		for _, item := range typedObj {
			err := ic.insertCardTableReferenceRecursively(ctx, item)
			if err != nil {
				return err
			}
		}

		return nil
	}

	return nil
}

// Replaces database integer IDs by references to Terraform `metabase_database` resources.
// In a card, the database is usually referenced by the query in `dataset_query.database`.
func (ic *ImportContext) insertCardDatabaseReference(ctx context.Context, card map[string]any) error {
	queryAny, ok := card[metabase.DatasetQueryAttribute]
	if !ok {
		return errors.New("unable to find database_query field in card")
	}

	queryMap, ok := queryAny.(map[string]any)
	if !ok {
		return errors.New("unable to unmarshal database_query field as map")
	}

	databaseAny, ok := queryMap[metabase.DatabaseAttribute]
	if !ok {
		return errors.New("unable to find database field in database_query map")
	}

	databaseId, ok := databaseAny.(float64)
	if !ok {
		return errors.New("unable to unmarshal database field as number")
	}

	database, err := ic.getDatabase(int(databaseId))
	if err != nil {
		return err
	}

	queryMap[metabase.DatabaseAttribute] = database

	return nil
}

// Replaces the references to fields by `importedField`s in a card's column settings.
// This is especially tricky because the referenced IDs have been marshalled twice and are actually part of more complex
// JSON strings used as keys in the column settings.
func (ic *ImportContext) insertFieldReferenceInCardColumnSettings(ctx context.Context, card map[string]any) error {
	visualizationSettingsAny, ok := card[metabase.VisualizationSettingsAttribute]
	if !ok {
		return nil
	}

	visualizationSettings, ok := visualizationSettingsAny.(map[string]any)
	if !ok {
		return errors.New("unable to unmarshal visualization_settings to a JSON object")
	}

	columnSettingsAny, ok := visualizationSettings[metabase.ColumnSettingsAttribute]
	if !ok {
		return nil
	}

	columnSettings, ok := columnSettingsAny.(map[string]any)
	if !ok {
		return errors.New("unable to unmarshal column_settings to a JSON object")
	}

	// The references converted to `importedField`s will be added after iterating over the column settings, to avoid
	// iterating over the new entries.
	entriesToAdd := make(map[string]any, 0)

	for k, v := range columnSettings {
		// The key is itself an array serialized as JSON.
		var keyArray []any
		err := json.Unmarshal([]byte(k), &keyArray)
		if err != nil || len(keyArray) < 2 {
			continue
		}

		firstStringElement, ok := keyArray[0].(string)
		if !ok || firstStringElement != metabase.FieldReferenceLiteral {
			continue
		}

		fieldArrayElement, ok := keyArray[1].([]any)
		if !ok {
			continue
		}

		inserted, err := ic.tryInsertFieldReference(ctx, fieldArrayElement)
		if err != nil {
			return nil
		}

		if inserted {
			// The replaced reference is marshalled back into JSON. `replacePlaceholders` will take care of ensuring the
			// Terraform data source is correctly referenced, even inside a string (there is a dedicated regexp for that).
			newKey, err := json.Marshal(keyArray)
			if err != nil {
				return nil
			}

			entriesToAdd[string(newKey)] = v
			delete(columnSettings, k)
		}
	}

	maps.Copy(columnSettings, entriesToAdd)

	return nil
}

// Replaces the reference to the parent collection in a card.
func (ic *ImportContext) insertCardCollectionReference(ctx context.Context, card map[string]any) error {
	collectionIdAny, ok := card[metabase.CollectionIdAttribute]
	if !ok {
		return errors.New("unable to find collection_id field in card")
	}

	if collectionIdAny == nil {
		return nil
	}

	// Although the collection ID can be a string, it is never the case in cards. If the card is part of the `root`
	// collection, the `collection_id` will simply be `null`.
	collectionId, ok := collectionIdAny.(float64)
	if !ok {
		return errors.New("unable to unmarshal collection_id field as number")
	}

	collection, err := ic.getCollection(fmt.Sprint(collectionId))
	if err != nil {
		return err
	}

	card[metabase.CollectionIdAttribute] = collection

	return nil
}

// Converts a raw JSON card to its HCL representation, including references to other Terraform resources and data
// sources. Only known attributes are kept.
func (ic *ImportContext) makeCardJson(ctx context.Context, card []byte) (*string, error) {
	var cardMap map[string]any
	err := json.Unmarshal(card, &cardMap)
	if err != nil {
		return nil, err
	}

	for key := range cardMap {
		if !metabase.DefiningCardAttributes[key] {
			delete(cardMap, key)
		}
	}

	err = ic.insertCardDatabaseReference(ctx, cardMap)
	if err != nil {
		return nil, err
	}

	err = ic.insertCardCollectionReference(ctx, cardMap)
	if err != nil {
		return nil, err
	}

	err = ic.insertFieldReferencesRecursively(ctx, cardMap)
	if err != nil {
		return nil, err
	}

	err = ic.insertCardTableReferenceRecursively(ctx, cardMap)
	if err != nil {
		return nil, err
	}

	err = ic.insertFieldReferenceInCardColumnSettings(ctx, cardMap)
	if err != nil {
		return nil, err
	}

	cardJson, err := json.MarshalIndent(cardMap, "  ", "  ")
	if err != nil {
		return nil, err
	}

	hcl := replacePlaceholders(string(cardJson))

	return &hcl, nil
}

// Produces the Terraform definition for a `metabase_card` resource.
func (ic *ImportContext) makeCardHcl(ctx context.Context, card []byte, slug string) (*string, error) {
	tpl, err := template.New("card").Parse(cardTemplate)
	if err != nil {
		return nil, err
	}

	cardJson, err := ic.makeCardJson(ctx, card)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	err = tpl.Execute(buf, cardTemplateData{
		TerraformSlug: slug,
		Json:          *cardJson,
	})
	if err != nil {
		return nil, err
	}

	hcl := buf.String()

	return &hcl, nil
}

// Fetches a card from the Metabase API and produces the corresponding Terraform definition.
func (ic *ImportContext) importCard(ctx context.Context, cardId int) (*importedCard, error) {
	card, ok := ic.cards[cardId]
	if ok {
		return &card, nil
	}

	getResp, err := ic.client.GetCardWithResponse(ctx, cardId)
	if err != nil {
		return nil, err
	}
	if getResp.JSON200 == nil {
		return nil, errors.New("received unexpected response when getting card")
	}

	slug := makeUniqueSlug(getResp.JSON200.Name, ic.cardsSlugs)

	hcl, err := ic.makeCardHcl(ctx, getResp.Body, slug)
	if err != nil {
		return nil, err
	}

	card = importedCard{
		Card: *getResp.JSON200,
		Slug: slug,
		Hcl:  *hcl,
	}

	ic.cards[cardId] = card

	return &card, nil
}
