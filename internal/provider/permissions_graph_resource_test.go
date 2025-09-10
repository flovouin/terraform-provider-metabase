package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

func testAccPermissionsGraphResource(createQueries, viewData string) string {
	return fmt.Sprintf(`
import {
  to = metabase_permissions_graph.graph
  id = "1"
}

resource "metabase_permissions_graph" "graph" {
  advanced_permissions = false

  permissions = [
    {
      group    = 1
      database = 1
      download = {
        schemas = "full"
      }
      view_data = %s
      create_queries = "%s"
    },
  ]
}
	`,
		viewData,
		createQueries,
	)
}

func TestAccPermissionsGraphResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					string(metabase.PermissionsGraphDatabasePermissionsCreateQueriesQueryBuilderAndNative),
					"\"unrestricted\"",
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_permissions_graph.graph", "advanced_permissions", "false"),
					resource.TestCheckResourceAttrSet("metabase_permissions_graph.graph", "revision"),
				),
			},
			{
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					string(metabase.PermissionsGraphDatabasePermissionsCreateQueriesNo),
					"\"unrestricted\"",
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_permissions_graph.graph", "advanced_permissions", "false"),
					resource.TestCheckResourceAttrSet("metabase_permissions_graph.graph", "revision"),
				),
			},
			{
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					string(metabase.PermissionsGraphDatabasePermissionsCreateQueriesNo),
					"jsonencode({ public = \"unrestricted\" })",
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_permissions_graph.graph", "advanced_permissions", "false"),
					resource.TestCheckResourceAttrSet("metabase_permissions_graph.graph", "revision"),
				),
			},
		},
	})
}
