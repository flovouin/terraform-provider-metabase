package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccCollectionResource(name string, collectionName string, description string, parentId string) string {
	return fmt.Sprintf(`
resource "metabase_collection" "%s" {
  name        = "%s"
	description = "%s"
	parent_id   = %s
}
`,
		name,
		collectionName,
		description,
		parentId,
	)
}

func testAccCheckCollectionExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		response, err := testAccMetabaseClient.GetCollectionWithResponse(context.Background(), rs.Primary.ID)
		if err != nil {
			return err
		}
		if response.StatusCode() != 200 {
			return fmt.Errorf("Received unexpected response from the Metabase API when getting collection.")
		}

		if rs.Primary.Attributes["name"] != response.JSON200.Name {
			return fmt.Errorf("Terraform resource and API response do not match for collection name.")
		}

		if rs.Primary.Attributes["description"] != *response.JSON200.Description {
			return fmt.Errorf("Terraform resource and API response do not match for collection description.")
		}

		return nil
	}
}

func testAccCheckCollectionDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "metabase_collection" {
			continue
		}

		response, err := testAccMetabaseClient.GetCollectionWithResponse(context.Background(), rs.Primary.ID)
		if err != nil {
			return err
		}
		if response.StatusCode() == 404 {
			return nil
		}
		if response.StatusCode() == 200 && *response.JSON200.Archived {
			return nil
		}

		return fmt.Errorf("Collection %s still exists.", rs.Primary.ID)
	}

	return nil
}

func TestAccCollectionResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccCollectionResource("test", "📚 Collection", "💡 Description", "null"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCollectionExists("metabase_collection.test"),
					resource.TestCheckResourceAttrSet("metabase_collection.test", "id"),
					resource.TestCheckResourceAttr("metabase_collection.test", "name", "📚 Collection"),
					resource.TestCheckResourceAttr("metabase_collection.test", "description", "💡 Description"),
				),
			},
			{
				ResourceName: "metabase_collection.test",
				ImportState:  true,
			},
			{
				Config: providerConfig + testAccCollectionResource("test", "🎁 Updated", "❓ Other", "null"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_collection.test", "id"),
					resource.TestCheckResourceAttr("metabase_collection.test", "name", "🎁 Updated"),
					resource.TestCheckResourceAttr("metabase_collection.test", "description", "❓ Other"),
				),
			},
			{
				Config: providerConfig +
					testAccCollectionResource("test", "🎁 Updated", "❓ Other", "null") +
					testAccCollectionResource("child", "🧒 Child", "🌴 Nested", "metabase_collection.test.id"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_collection.child", "id"),
					resource.TestCheckResourceAttr("metabase_collection.child", "name", "🧒 Child"),
					resource.TestCheckResourceAttr("metabase_collection.child", "description", "🌴 Nested"),
					resource.TestCheckResourceAttrSet("metabase_collection.child", "parent_id"),
				),
			},
		},
	})
}

// Creates many collections at once. Terraform applies them in parallel, and Metabase fails with a duplicate key error
// when it records concurrent revisions of the collection graph, unless the provider serializes the requests.
func TestAccCollectionResourceParallel(t *testing.T) {
	const count = 12

	config := providerConfig
	checks := []resource.TestCheckFunc{}
	for i := range count {
		name := fmt.Sprintf("parallel_%d", i)
		config += testAccCollectionResource(name, fmt.Sprintf("🏎️ Parallel %d", i), "🏁 Created in parallel", "null")
		checks = append(checks, testAccCheckCollectionExists("metabase_collection."+name))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.ComposeAggregateTestCheckFunc(checks...),
			},
		},
	})
}
