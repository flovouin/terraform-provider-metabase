package provider

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccCardResource(name string, displayName string, queryOptions string) string {
	// This references the sample database, which should always have ID 1.
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
      %s
      database = 1
      type     = "query"
      query = {
        source-table = 1
      }
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
		queryOptions,
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
      database = 1
      type     = "native"
      native = {
        query = "SELECT 1"
      }
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

func TestAccCardResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCardDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccCardResource("test", "🪪", ""),
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
				Config: providerConfig + testAccCardResource("test", "💳", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_card.test", "id"),
					resource.TestCheckResourceAttrSet("metabase_card.test", "json"),
				),
			},
			{
				Config: providerConfig + testAccCardResource("test", "💳", "breakout-idents = { 0 = \"ABCD\" }"),
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
