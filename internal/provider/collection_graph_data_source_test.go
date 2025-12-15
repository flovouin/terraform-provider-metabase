package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccCollectionGraphDataSource() string {
	return `
data "metabase_collection_graph" "test" {}
`
}

func testAccCollectionGraphDataSourceWithIgnoredGroups() string {
	return `
data "metabase_collection_graph" "test" {
  ignored_groups = [2]
}
`
}

func TestAccCollectionGraphDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerApiKeyConfig + testAccCollectionGraphDataSource(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.metabase_collection_graph.test", "revision"),
					resource.TestCheckResourceAttrSet("data.metabase_collection_graph.test", "permissions.#"),
				),
			},
			{
				Config: providerApiKeyConfig + testAccCollectionGraphDataSourceWithIgnoredGroups(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.metabase_collection_graph.test", "revision"),
					resource.TestCheckResourceAttrSet("data.metabase_collection_graph.test", "permissions.#"),
				),
			},
		},
	})
}
