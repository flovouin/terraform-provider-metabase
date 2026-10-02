package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Returns a configuration managing the permissions of the `All Users` group, with the given permissions for the
// `metabase_collection.target` collection.
// New collections inherit the permissions of their parent. The root collection is not listed, such that the `All Users`
// group has no permission on it, and collections created at the root do not inherit any permission for this group.
func testAccCollectionGraphResource(targetPermission string) string {
	return fmt.Sprintf(`
import {
  to = metabase_collection_graph.graph
  id = "1"
}

resource "metabase_collection" "target" {
  name = "🔐 Graph target"
}

resource "metabase_collection_graph" "graph" {
  permissions = [
    {
      group      = 1
      collection = metabase_collection.target.id
      permission = "%s"
    },
  ]
}
`,
		targetPermission,
	)
}

func TestAccCollectionGraphResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// The collection is created in the same apply as the graph update, which records a new revision of the graph.
				Config: providerApiKeyConfig + testAccCollectionGraphResource("read"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_collection_graph.graph", "revision"),
					resource.TestCheckResourceAttr("metabase_collection_graph.graph", "permissions.#", "1"),
					resource.TestCheckTypeSetElemNestedAttrs("metabase_collection_graph.graph", "permissions.*", map[string]string{"group": "1", "permission": "read"}),
				),
			},
			{
				Config: providerApiKeyConfig + testAccCollectionGraphResource("write"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_collection_graph.graph", "permissions.#", "1"),
					resource.TestCheckTypeSetElemNestedAttrs("metabase_collection_graph.graph", "permissions.*", map[string]string{"group": "1", "permission": "write"}),
				),
			},
		},
	})
}

// Updates the graph while unrelated collections are created in parallel. Creating a collection records a new revision
// of the graph, and concurrent revisions fail with a duplicate key error unless the provider serializes the requests.
func TestAccCollectionGraphResourceParallel(t *testing.T) {
	const count = 8

	collections := ""
	for i := range count {
		collections += testAccCollectionResource(fmt.Sprintf("graph_parallel_%d", i), fmt.Sprintf("🏎️ Graph parallel %d", i), "🏁 Created in parallel", "null")
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerApiKeyConfig + testAccCollectionGraphResource("read"),
			},
			{
				Config: providerApiKeyConfig + testAccCollectionGraphResource("write") + collections,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_collection_graph.graph", "permissions.#", "1"),
					resource.TestCheckTypeSetElemNestedAttrs("metabase_collection_graph.graph", "permissions.*", map[string]string{"group": "1", "permission": "write"}),
				),
			},
		},
	})
}
