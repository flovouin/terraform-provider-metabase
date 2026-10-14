package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
)

const (
	testDatabaseId   = 1
	testCollectionId = 4

	testOrdersTableId = 10
	testPeopleTableId = 20
	// The ID of a table the fake Metabase API does not know about.
	testMissingTableId = 99

	testOrdersIdFieldId        = 101
	testOrdersTotalFieldId     = 102
	testOrdersCreatedAtFieldId = 103
	testOrdersUserIdFieldId    = 104
	testPeopleIdFieldId        = 201

	testOrdersByDayCardId       = 1
	testLargeOrdersCardId       = 2
	testOrdersWithPeopleCardId  = 3
	testLegacyOrdersCardId      = 4
	testLegacyOnOrdersCardId    = 5
	testMissingTableCardId      = 6
	testMissingJoinTableCardId  = 7
	testCycleFirstCardId        = 8
	testCycleSecondCardId       = 9
	testSourceOfMissingCardId   = 10
	testMissingCardId           = 11
	testUnexpectedSourceCardId  = 12
	testUnexpectedSourceTableId = 13
)

// The cards served by the fake Metabase API, by ID.
// The queries have the shape returned by Metabase, either as MBQL 5 (`stages`) or as the legacy MBQL 4 (`query`).
var testCards = map[int]string{
	// A question on a table, which references its fields by ID after their options (MBQL 5).
	testOrdersByDayCardId: `{
		"id": 1,
		"name": "Orders by day",
		"archived": false,
		"cache_ttl": null,
		"collection_id": 4,
		"collection_position": null,
		"dataset_query": {
			"database": 1,
			"lib/type": "mbql/query",
			"stages": [
				{
					"lib/type": "mbql.stage/mbql",
					"source-table": 10,
					"aggregation": [
						["sum", {"lib/uuid": "00000000-0000-4000-8000-000000000001", "name": "revenue"},
							["field", {"base-type": "type/Float", "lib/uuid": "00000000-0000-4000-8000-000000000002"}, 102]]
					],
					"breakout": [
						["field", {"base-type": "type/DateTime", "lib/uuid": "00000000-0000-4000-8000-000000000003", "temporal-unit": "day"}, 103]
					],
					"filters": [
						[">", {"lib/uuid": "00000000-0000-4000-8000-000000000004"},
							["field", {"base-type": "type/Float", "lib/uuid": "00000000-0000-4000-8000-000000000005"}, 102],
							0]
					]
				}
			]
		},
		"description": null,
		"display": "line",
		"parameter_mappings": [],
		"parameters": [],
		"query_type": "query",
		"visualization_settings": {}
	}`,
	// A question built on another question, which references its columns by name.
	testLargeOrdersCardId: `{
		"id": 2,
		"name": "Large orders by day",
		"archived": false,
		"cache_ttl": null,
		"collection_id": 4,
		"collection_position": null,
		"dataset_query": {
			"database": 1,
			"lib/type": "mbql/query",
			"stages": [
				{
					"lib/type": "mbql.stage/mbql",
					"source-card": 1,
					"filters": [
						[">", {"lib/uuid": "00000000-0000-4000-8000-000000000011"},
							["field", {"base-type": "type/Float", "lib/uuid": "00000000-0000-4000-8000-000000000012"}, "revenue"],
							1000]
					]
				}
			]
		},
		"description": null,
		"display": "table",
		"parameter_mappings": [],
		"parameters": [],
		"query_type": "query",
		"visualization_settings": {}
	}`,
	// A question joining a table and another question, such that references are nested in the join.
	testOrdersWithPeopleCardId: `{
		"id": 3,
		"name": "Orders with people",
		"archived": false,
		"cache_ttl": null,
		"collection_id": 4,
		"collection_position": null,
		"dataset_query": {
			"database": 1,
			"lib/type": "mbql/query",
			"stages": [
				{
					"lib/type": "mbql.stage/mbql",
					"source-table": 10,
					"joins": [
						{
							"lib/type": "mbql/join",
							"alias": "People",
							"strategy": "left-join",
							"fields": "all",
							"conditions": [
								["=", {"lib/uuid": "00000000-0000-4000-8000-000000000021"},
									["field", {"base-type": "type/Integer", "lib/uuid": "00000000-0000-4000-8000-000000000022"}, 104],
									["field", {"base-type": "type/Integer", "join-alias": "People", "lib/uuid": "00000000-0000-4000-8000-000000000023"}, 201]]
							],
							"stages": [{"lib/type": "mbql.stage/mbql", "source-table": 20}]
						},
						{
							"lib/type": "mbql/join",
							"alias": "Daily",
							"strategy": "left-join",
							"fields": "none",
							"conditions": [
								["=", {"lib/uuid": "00000000-0000-4000-8000-000000000024"},
									["field", {"base-type": "type/DateTime", "lib/uuid": "00000000-0000-4000-8000-000000000025"}, 103],
									["field", {"base-type": "type/DateTime", "join-alias": "Daily", "lib/uuid": "00000000-0000-4000-8000-000000000026"}, "created_at"]]
							],
							"stages": [{"lib/type": "mbql.stage/mbql", "source-card": 1}]
						}
					]
				}
			]
		},
		"description": null,
		"display": "table",
		"parameter_mappings": [],
		"parameters": [],
		"query_type": "query",
		"visualization_settings": {}
	}`,
	// A question built on another question, as a legacy query.
	testLegacyOrdersCardId: `{
		"id": 4,
		"name": "Orders by month",
		"archived": false,
		"cache_ttl": null,
		"collection_id": 4,
		"collection_position": null,
		"dataset_query": {
			"database": 1,
			"type": "query",
			"query": {
				"source-table": "card__1",
				"aggregation": [["sum", ["field", "revenue", {"base-type": "type/Float"}]]],
				"breakout": [["field", "created_at", {"base-type": "type/DateTime", "temporal-unit": "month"}]]
			}
		},
		"description": null,
		"display": "bar",
		"parameter_mappings": [],
		"parameters": [],
		"query_type": "query",
		"visualization_settings": {}
	}`,
	// A question on a table, as a legacy query which references its fields by ID before their options.
	testLegacyOnOrdersCardId: `{
		"id": 5,
		"name": "Revenue by month",
		"archived": false,
		"cache_ttl": null,
		"collection_id": 4,
		"collection_position": null,
		"dataset_query": {
			"database": 1,
			"type": "query",
			"query": {
				"source-table": 10,
				"aggregation": [["sum", ["field", 102, null]]],
				"breakout": [["field", 103, {"base-type": "type/DateTime", "temporal-unit": "month"}]]
			}
		},
		"description": null,
		"display": "bar",
		"parameter_mappings": [],
		"parameters": [],
		"query_type": "query",
		"visualization_settings": {}
	}`,
	// A question on a table that cannot be imported.
	testMissingTableCardId: `{
		"id": 6,
		"name": "Rows of a missing table",
		"archived": false,
		"collection_id": 4,
		"dataset_query": {
			"database": 1,
			"lib/type": "mbql/query",
			"stages": [{"lib/type": "mbql.stage/mbql", "source-table": 99}]
		},
		"visualization_settings": {}
	}`,
	// A question joining a table that cannot be imported, such that the failure is nested in the join.
	testMissingJoinTableCardId: `{
		"id": 7,
		"name": "Orders with a missing table",
		"archived": false,
		"collection_id": 4,
		"dataset_query": {
			"database": 1,
			"lib/type": "mbql/query",
			"stages": [
				{
					"lib/type": "mbql.stage/mbql",
					"joins": [
						{
							"lib/type": "mbql/join",
							"alias": "Missing",
							"conditions": [],
							"stages": [{"lib/type": "mbql.stage/mbql", "source-table": 99}]
						}
					],
					"source-table": 10
				}
			]
		},
		"visualization_settings": {}
	}`,
	// Two questions built on each other. Metabase refuses to save such questions, but could still return them.
	testCycleFirstCardId: `{
		"id": 8,
		"name": "First of a cycle",
		"archived": false,
		"collection_id": 4,
		"dataset_query": {
			"database": 1,
			"lib/type": "mbql/query",
			"stages": [{"lib/type": "mbql.stage/mbql", "source-card": 9}]
		},
		"visualization_settings": {}
	}`,
	testCycleSecondCardId: `{
		"id": 9,
		"name": "Second of a cycle",
		"archived": false,
		"collection_id": 4,
		"dataset_query": {
			"database": 1,
			"lib/type": "mbql/query",
			"stages": [{"lib/type": "mbql.stage/mbql", "source-card": 8}]
		},
		"visualization_settings": {}
	}`,
	// A question built on a question that cannot be imported.
	testSourceOfMissingCardId: `{
		"id": 10,
		"name": "Rows of a missing card",
		"archived": false,
		"collection_id": 4,
		"dataset_query": {
			"database": 1,
			"lib/type": "mbql/query",
			"stages": [{"lib/type": "mbql.stage/mbql", "source-card": 11}]
		},
		"visualization_settings": {}
	}`,
	// Questions for which the source is not something the importer knows how to reference.
	testUnexpectedSourceCardId: `{
		"id": 12,
		"name": "Unexpected source card",
		"archived": false,
		"collection_id": 4,
		"dataset_query": {
			"database": 1,
			"lib/type": "mbql/query",
			"stages": [{"lib/type": "mbql.stage/mbql", "source-card": "a-card"}]
		},
		"visualization_settings": {}
	}`,
	testUnexpectedSourceTableId: `{
		"id": 13,
		"name": "Unexpected source table",
		"archived": false,
		"collection_id": 4,
		"dataset_query": {
			"database": 1,
			"type": "query",
			"query": {"source-table": "a-table"}
		},
		"visualization_settings": {}
	}`,
}

// Returns an import context for which the database, the collection, and the `orders` and `people` tables (as well as
// their fields) are already known. It uses a fake Metabase API only serving `testCards`, such that importing anything
// else fails.
func newTestImportContext(t *testing.T) *ImportContext {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cardIdStr, isCard := strings.CutPrefix(r.URL.Path, "/card/")
		cardId, err := strconv.Atoi(cardIdStr)
		card, exists := testCards[cardId]
		if r.Method != http.MethodGet || !isCard || err != nil || !exists {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(card))
	}))
	t.Cleanup(server.Close)

	client, err := metabase.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("failed to create the client: %v", err)
	}

	ic := NewImportContext(*client)

	ic.databases[testDatabaseId] = importedDatabase{
		Database:   metabase.Database{Id: testDatabaseId, Name: "Sample Database"},
		Slug:       "sample",
		DataSource: true,
	}
	ic.collections[fmt.Sprint(testCollectionId)] = importedCollection{
		Collection: metabase.Collection{Name: "Reports"},
		Slug:       "reports",
	}

	tables := []metabase.TableMetadata{
		{
			Id:   testOrdersTableId,
			DbId: testDatabaseId,
			Name: "orders",
			Fields: []metabase.Field{
				{Id: testOrdersIdFieldId, Name: "id", TableId: testOrdersTableId},
				{Id: testOrdersTotalFieldId, Name: "total", TableId: testOrdersTableId},
				{Id: testOrdersCreatedAtFieldId, Name: "created_at", TableId: testOrdersTableId},
				{Id: testOrdersUserIdFieldId, Name: "user_id", TableId: testOrdersTableId},
			},
		},
		{
			Id:   testPeopleTableId,
			DbId: testDatabaseId,
			Name: "people",
			Fields: []metabase.Field{
				{Id: testPeopleIdFieldId, Name: "id", TableId: testPeopleTableId},
			},
		},
	}
	for _, table := range tables {
		slug := makeUniqueSlug(table.Name, ic.tablesSlugs)

		hcl, err := ic.makeTableHcl(table, slug)
		if err != nil {
			t.Fatalf("failed to make the definition of table %s: %v", table.Name, err)
		}

		imported := importedTable{Table: table, Slug: slug, Hcl: *hcl}
		ic.tables[table.Id] = imported
		for _, field := range table.Fields {
			ic.fields[field.Id] = importedField{Field: field, ParentTable: &imported}
		}
	}

	return &ic
}

// Fails the test if the definition of the card does not contain all the given snippets.
func expectInCard(t *testing.T, card *importedCard, snippets ...string) {
	t.Helper()

	for _, snippet := range snippets {
		if !strings.Contains(card.Hcl, snippet) {
			t.Errorf("expected the definition of card %q to contain %s, got:\n%s", card.Card.Name, snippet, card.Hcl)
		}
	}
}

// Fails the test if the import context does not contain exactly the given cards.
func expectImportedCards(t *testing.T, ic *ImportContext, cardIds ...int) {
	t.Helper()

	if len(ic.cards) != len(cardIds) {
		t.Errorf("expected %d imported cards, got %d", len(cardIds), len(ic.cards))
	}
	for _, cardId := range cardIds {
		if _, ok := ic.cards[cardId]; !ok {
			t.Errorf("expected card %d to be imported", cardId)
		}
	}
}

func TestInsertFieldReferences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ID after the options",
			input:    `["field", {"base-type": "type/Float", "lib/uuid": "u"}, 102]`,
			expected: `["field",{"base-type":"type/Float","lib/uuid":"u"},metabase_table.orders.fields["total"]]`,
		},
		{
			name:     "ID after the options, in another clause",
			input:    `["=", {"lib/uuid": "u"}, ["field", {"join-alias": "People", "lib/uuid": "v"}, 201], 0]`,
			expected: `["=",{"lib/uuid":"u"},["field",{"join-alias":"People","lib/uuid":"v"},metabase_table.people.fields["id"]],0]`,
		},
		{
			name:     "ID before the options",
			input:    `["field", 102, {"base-type": "type/Float"}]`,
			expected: `["field",metabase_table.orders.fields["total"],{"base-type":"type/Float"}]`,
		},
		{
			name:     "ID without options",
			input:    `["field", 103, null]`,
			expected: `["field",metabase_table.orders.fields["created_at"],null]`,
		},
		{
			name:     "ID in the target of a dashboard parameter",
			input:    `["dimension", ["field", 103, {"base-type": "type/DateTime"}], {"stage-number": 0}]`,
			expected: `["dimension",["field",metabase_table.orders.fields["created_at"],{"base-type":"type/DateTime"}],{"stage-number":0}]`,
		},
		{
			name:     "column name after the options",
			input:    `["field", {"base-type": "type/Float", "lib/uuid": "u"}, "total"]`,
			expected: `["field",{"base-type":"type/Float","lib/uuid":"u"},"total"]`,
		},
		{
			name:     "column name before the options",
			input:    `["field", "total", {"base-type": "type/Float"}]`,
			expected: `["field","total",{"base-type":"type/Float"}]`,
		},
		{
			name:     "options without an ID",
			input:    `["field", {"lib/uuid": "u"}]`,
			expected: `["field",{"lib/uuid":"u"}]`,
		},
		{
			name:     "not a field",
			input:    `["aggregation", {"lib/uuid": "u"}, 102]`,
			expected: `["aggregation",{"lib/uuid":"u"},102]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ic := newTestImportContext(t)

			var obj any
			if err := json.Unmarshal([]byte(test.input), &obj); err != nil {
				t.Fatalf("failed to unmarshal the input: %v", err)
			}

			if err := ic.insertFieldReferencesRecursively(context.Background(), obj); err != nil {
				t.Fatalf("failed to insert the field references: %v", err)
			}

			output, err := json.Marshal(obj)
			if err != nil {
				t.Fatalf("failed to marshal the output: %v", err)
			}

			if got := replacePlaceholders(string(output)); got != test.expected {
				t.Errorf("expected %s, got %s", test.expected, got)
			}
		})
	}
}

func TestInsertFieldReferencesFailsWhenFieldCannotBeImported(t *testing.T) {
	ic := newTestImportContext(t)

	var obj any
	if err := json.Unmarshal([]byte(`["field", {"lib/uuid": "u"}, 999]`), &obj); err != nil {
		t.Fatalf("failed to unmarshal the input: %v", err)
	}

	if err := ic.insertFieldReferencesRecursively(context.Background(), obj); err == nil {
		t.Error("expected an error when the field cannot be imported")
	}
}

func TestImportCardReferencesFieldsAfterOptions(t *testing.T) {
	ic := newTestImportContext(t)

	card, err := ic.importCard(context.Background(), testOrdersByDayCardId)
	if err != nil {
		t.Fatalf("failed to import the card: %v", err)
	}

	expectInCard(t, card,
		`"database": data.metabase_database.sample.id`,
		`"collection_id": tonumber(metabase_collection.reports.id)`,
		`"source-table": metabase_table.orders.id`,
		`metabase_table.orders.fields["created_at"]`,
	)
	if got := strings.Count(card.Hcl, `metabase_table.orders.fields["total"]`); got != 2 {
		t.Errorf("expected 2 references to the total field, got %d in:\n%s", got, card.Hcl)
	}
	expectImportedCards(t, ic, testOrdersByDayCardId)
}

func TestImportCardReferencesFieldsBeforeOptions(t *testing.T) {
	ic := newTestImportContext(t)

	card, err := ic.importCard(context.Background(), testLegacyOnOrdersCardId)
	if err != nil {
		t.Fatalf("failed to import the card: %v", err)
	}

	expectInCard(t, card,
		`"source-table": metabase_table.orders.id`,
		`metabase_table.orders.fields["total"]`,
		`metabase_table.orders.fields["created_at"]`,
	)
}

func TestImportCardReferencesSourceCard(t *testing.T) {
	ic := newTestImportContext(t)

	card, err := ic.importCard(context.Background(), testLargeOrdersCardId)
	if err != nil {
		t.Fatalf("failed to import the card: %v", err)
	}

	expectInCard(t, card,
		`"source-card": metabase_card.orders_by_day.id`,
		// The columns of the source card are referenced by name, which should be left untouched.
		`"revenue"`,
	)
}

func TestImportCardImportsSourceCard(t *testing.T) {
	ic := newTestImportContext(t)

	// The source card is not part of any dashboard: it is only imported because the card is built on it.
	_, err := ic.importCard(context.Background(), testLargeOrdersCardId)
	if err != nil {
		t.Fatalf("failed to import the card: %v", err)
	}

	expectImportedCards(t, ic, testLargeOrdersCardId, testOrdersByDayCardId)

	path := t.TempDir()
	err = ic.Write(path, WriteOptions{DisableFormatting: true})
	if err != nil {
		t.Fatalf("failed to write the Terraform files: %v", err)
	}

	for _, fileName := range []string{"mb-gen-card-large-orders-by-day.tf", "mb-gen-card-orders-by-day.tf"} {
		content, err := os.ReadFile(filepath.Join(path, fileName))
		if err != nil {
			t.Errorf("expected %s to be written: %v", fileName, err)
			continue
		}
		if !strings.HasPrefix(string(content), `resource "metabase_card" `) {
			t.Errorf("expected %s to define a card, got:\n%s", fileName, content)
		}
	}
}

func TestImportCardReferencesSourcesInJoins(t *testing.T) {
	ic := newTestImportContext(t)

	card, err := ic.importCard(context.Background(), testOrdersWithPeopleCardId)
	if err != nil {
		t.Fatalf("failed to import the card: %v", err)
	}

	expectInCard(t, card,
		`"source-table": metabase_table.orders.id`,
		`"source-table": metabase_table.people.id`,
		`"source-card": metabase_card.orders_by_day.id`,
		`metabase_table.orders.fields["user_id"]`,
		`metabase_table.people.fields["id"]`,
		`metabase_table.orders.fields["created_at"]`,
	)
	expectImportedCards(t, ic, testOrdersWithPeopleCardId, testOrdersByDayCardId)
}

func TestImportCardReferencesLegacySourceCard(t *testing.T) {
	ic := newTestImportContext(t)

	card, err := ic.importCard(context.Background(), testLegacyOrdersCardId)
	if err != nil {
		t.Fatalf("failed to import the card: %v", err)
	}

	expectInCard(t, card, `"source-table": "card__${metabase_card.orders_by_day.id}"`)
	expectImportedCards(t, ic, testLegacyOrdersCardId, testOrdersByDayCardId)
}

func TestImportCardFailsWhenSourceCannotBeImported(t *testing.T) {
	tests := []struct {
		name     string
		cardId   int
		expected string // What the error should mention.
	}{
		{name: "table", cardId: testMissingTableCardId, expected: fmt.Sprint(testMissingTableId)},
		{name: "table in a join", cardId: testMissingJoinTableCardId, expected: fmt.Sprint(testMissingTableId)},
		{name: "card", cardId: testSourceOfMissingCardId, expected: fmt.Sprint(testMissingCardId)},
		{name: "unexpected source card", cardId: testUnexpectedSourceCardId, expected: "source-card"},
		{name: "unexpected source table", cardId: testUnexpectedSourceTableId, expected: "source-table"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ic := newTestImportContext(t)

			_, err := ic.importCard(context.Background(), test.cardId)
			if err == nil {
				t.Fatal("expected an error when the source cannot be imported")
			}
			if !strings.Contains(err.Error(), test.expected) {
				t.Errorf("expected the error to mention %s, got: %v", test.expected, err)
			}
			if _, ok := ic.cards[test.cardId]; ok {
				t.Error("expected the card not to be imported")
			}
		})
	}
}

func TestImportCardFailsOnSourceCardCycle(t *testing.T) {
	ic := newTestImportContext(t)

	_, err := ic.importCard(context.Background(), testCycleFirstCardId)
	if err == nil {
		t.Fatal("expected an error when cards are built on each other")
	}

	for _, cardId := range []int{testCycleFirstCardId, testCycleSecondCardId} {
		if !strings.Contains(err.Error(), fmt.Sprintf("card %d", cardId)) {
			t.Errorf("expected the error to mention card %d, got: %v", cardId, err)
		}
	}
	expectImportedCards(t, ic)
}
