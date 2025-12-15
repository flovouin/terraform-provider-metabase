package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccPermissionsGraphDataSource() string {
	return `
data "metabase_permissions_graph" "test" {}
`
}

func testAccPermissionsGraphDataSourceWithOptions() string {
	return `
data "metabase_permissions_graph" "test" {
  ignored_groups       = [2]
}
`
}

func TestAccPermissionsGraphDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerApiKeyConfig + testAccPermissionsGraphDataSource(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.metabase_permissions_graph.test", "revision"),
					resource.TestCheckResourceAttrSet("data.metabase_permissions_graph.test", "permissions.#"),
				),
			},
			{
				Config: providerApiKeyConfig + testAccPermissionsGraphDataSourceWithOptions(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.metabase_permissions_graph.test", "revision"),
					resource.TestCheckResourceAttrSet("data.metabase_permissions_graph.test", "permissions.#"),
				),
			},
		},
	})
}
