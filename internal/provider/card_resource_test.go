package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccCardResource(name string, displayName string) string {
	// This references the sample database, which should always have ID 1.
	// The query is expressed in the pMBQL ("lib") format, which is what Metabase 0.61+ stores and returns.
	return fmt.Sprintf(`
resource "metabase_card" "%s" {
  json = jsonencode({
    name                = "%s"
    description         = "📚"
    collection_id       = null
    collection_position = null
    cache_ttl           = null
    query_type          = "query"
    dataset_query = {
      "lib/type" = "mbql/query"
      database   = 1
      stages = [
        {
          "lib/type"   = "mbql.stage/mbql"
          source-table = 1
        }
      ]
    }
    parameter_mappings     = []
    display                = "table"
    visualization_settings = {}
    parameters             = []
  })
}
`,
		name,
		displayName,
	)
}

func testAccNativeQueryCardResource(name string, displayName string) string {
	return fmt.Sprintf(`
resource "metabase_card" "%s" {
  json = jsonencode({
    name                = "%s"
    description         = "Native query card"
    collection_id       = null
    collection_position = null
    cache_ttl           = null
    query_type          = "native"
    dataset_query = {
      "lib/type" = "mbql/query"
      database   = 1
      stages = [
        {
          "lib/type" = "mbql.stage/native"
          native     = "SELECT 1"
        }
      ]
    }
    parameter_mappings     = []
    display                = "table"
    visualization_settings = {}
    parameters             = []
  })
}
`,
		name,
		displayName,
	)
}

func testAccCheckCardExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return err
		}

		response, err := testAccMetabaseClient.GetCardWithResponse(context.Background(), id)
		if err != nil {
			return err
		}
		if response.StatusCode() != 200 {
			return fmt.Errorf("Received unexpected response from the Metabase API when getting card.")
		}

		return nil
	}
}

func testAccCheckCardDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "metabase_card" {
			continue
		}

		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return err
		}

		response, err := testAccMetabaseClient.GetCardWithResponse(context.Background(), id)
		if err != nil {
			return err
		}
		if response.StatusCode() != 404 && !response.JSON200.Archived {
			return fmt.Errorf("Card %s still exists.", rs.Primary.ID)
		}
	}

	return nil
}

// Returns the JSON definition of a card with a native query, using the pMBQL format, and the given template tags.
func makeNativeCardJson(templateTags string) string {
	return fmt.Sprintf(`{
  "name": "Native query card",
  "query_type": "native",
  "dataset_query": {
    "lib/type": "mbql/query",
    "database": 1,
    "stages": [
      {
        "lib/type": "mbql.stage/native",
        "native": "SELECT {{id}}",
        "template-tags": %s
      }
    ]
  },
  "display": "table"
}`, templateTags)
}

// Checks that the JSON definition in the model is equivalent to the expected JSON string.
func checkCardJson(t *testing.T, data *CardResourceModel, expectedJson string) {
	t.Helper()

	var actual any
	if err := json.Unmarshal([]byte(data.Json.ValueString()), &actual); err != nil {
		t.Fatalf("Failed to deserialize the card JSON in the model: %s", err)
	}

	var expected any
	if err := json.Unmarshal([]byte(expectedJson), &expected); err != nil {
		t.Fatalf("Failed to deserialize the expected card JSON: %s", err)
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Unexpected card JSON.\nActual:   %s\nExpected: %s", data.Json.ValueString(), expectedJson)
	}
}

// Metabase 0.63+ returns template tags as a list, while existing definitions use a map indexed by the tag name.
func TestUpdateModelFromCardBytesKeepsTemplateTagsAsMap(t *testing.T) {
	existingJson := makeNativeCardJson(`{"id": {"id": "abc", "name": "id", "display-name": "Id", "type": "number"}}`)
	responseJson := makeNativeCardJson(`[{"id": "abc", "name": "id", "display-name": "Id", "type": "number"}]`)

	data := &CardResourceModel{Json: types.StringValue(existingJson)}
	diags := updateModelFromCardBytes(fmt.Appendf(nil, `{"id": 1, %s`, responseJson[1:]), data)

	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %s", diags)
	}
	checkCardJson(t, data, existingJson)
}

// The same applies to the legacy query format, where template tags are found under the `native` attribute.
func TestUpdateModelFromCardBytesKeepsLegacyTemplateTagsAsMap(t *testing.T) {
	makeCard := func(templateTags string) string {
		return fmt.Sprintf(`{
  "name": "Native query card",
  "query_type": "native",
  "dataset_query": {
    "type": "native",
    "database": 1,
    "native": {
      "query": "SELECT {{id}}",
      "template-tags": %s
    }
  },
  "display": "table"
}`, templateTags)
	}

	existingJson := makeCard(`{"id": {"id": "abc", "name": "id", "type": "number"}}`)
	responseJson := makeCard(`[{"id": "abc", "name": "id", "type": "number"}]`)

	data := &CardResourceModel{Json: types.StringValue(existingJson)}
	diags := updateModelFromCardBytes(fmt.Appendf(nil, `{"id": 1, %s`, responseJson[1:]), data)

	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %s", diags)
	}
	checkCardJson(t, data, existingJson)
}

// Definitions using a list of template tags are also supported, in which case the order of the existing list is kept.
func TestUpdateModelFromCardBytesKeepsTemplateTagsAsList(t *testing.T) {
	existingJson := makeNativeCardJson(`[{"id": "1", "name": "b"}, {"id": "2", "name": "a"}]`)
	responseJson := makeNativeCardJson(`{"a": {"id": "2", "name": "a"}, "b": {"id": "1", "name": "b"}}`)

	data := &CardResourceModel{Json: types.StringValue(existingJson)}
	diags := updateModelFromCardBytes(fmt.Appendf(nil, `{"id": 1, %s`, responseJson[1:]), data)

	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %s", diags)
	}
	checkCardJson(t, data, existingJson)
}

// The order in which the Metabase API returns the list of template tags should not cause a diff.
func TestUpdateModelFromCardBytesReordersTemplateTagsList(t *testing.T) {
	existingJson := makeNativeCardJson(`[{"id": "1", "name": "b"}, {"id": "2", "name": "a"}]`)
	responseJson := makeNativeCardJson(`[{"id": "2", "name": "a"}, {"id": "1", "name": "b"}]`)

	data := &CardResourceModel{Json: types.StringValue(existingJson)}
	diags := updateModelFromCardBytes(fmt.Appendf(nil, `{"id": 1, %s`, responseJson[1:]), data)

	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %s", diags)
	}
	checkCardJson(t, data, existingJson)
}

// An actual difference in the template tags should still be reflected in the state.
func TestUpdateModelFromCardBytesUpdatesModifiedTemplateTags(t *testing.T) {
	existingJson := makeNativeCardJson(`{"id": {"id": "abc", "name": "id", "type": "number"}}`)
	responseJson := makeNativeCardJson(`[{"id": "abc", "name": "id", "type": "text"}]`)

	data := &CardResourceModel{Json: types.StringValue(existingJson)}
	diags := updateModelFromCardBytes(fmt.Appendf(nil, `{"id": 1, %s`, responseJson[1:]), data)

	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %s", diags)
	}
	checkCardJson(t, data, makeNativeCardJson(`{"id": {"id": "abc", "name": "id", "type": "text"}}`))
}

// When the card is created, or imported, there is no existing definition to compare the template tags with.
func TestUpdateModelFromCardBytesKeepsTemplateTagsWithoutExistingCard(t *testing.T) {
	responseJson := makeNativeCardJson(`[{"id": "abc", "name": "id", "type": "number"}]`)

	data := &CardResourceModel{Json: types.StringNull()}
	diags := updateModelFromCardBytes(fmt.Appendf(nil, `{"id": 1, %s`, responseJson[1:]), data)

	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %s", diags)
	}
	checkCardJson(t, data, responseJson)
}

func TestAccCardResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCardDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccCardResource("test", "🪪"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCardExists("metabase_card.test"),
					resource.TestCheckResourceAttrSet("metabase_card.test", "id"),
					resource.TestCheckResourceAttrSet("metabase_card.test", "json"),
				),
			},
			{
				ResourceName: "metabase_card.test",
				ImportState:  true,
			},
			{
				Config: providerConfig + testAccCardResource("test", "💳"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_card.test", "id"),
					resource.TestCheckResourceAttrSet("metabase_card.test", "json"),
				),
			},
		},
	})
}

func TestAccNativeQueryCardResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCardDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccNativeQueryCardResource("test_native", "Native Query Card"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCardExists("metabase_card.test_native"),
					resource.TestCheckResourceAttrSet("metabase_card.test_native", "id"),
					resource.TestCheckResourceAttrSet("metabase_card.test_native", "json"),
				),
			},
			{
				ResourceName: "metabase_card.test_native",
				ImportState:  true,
			},
			{
				Config: providerConfig + testAccNativeQueryCardResource("test_native", "Updated Native Query"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_card.test_native", "id"),
					resource.TestCheckResourceAttrSet("metabase_card.test_native", "json"),
				),
			},
		},
	})
}
