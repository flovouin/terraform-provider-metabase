package provider

import (
	"fmt"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// perTableCreateQueries builds a `create_queries` `jsonencode` expression covering every table in the sample
// database's `PUBLIC` schema (IDs 1-8). Tables listed in `noTables` are set to `"no"`, the rest to `"query-builder"`.
//
// Metabase reshapes its response in two ways the provider must absorb without producing a perpetual diff:
//   - With no `noTables`, the value is uniform across the whole (single-schema) database, so Metabase collapses the
//     response all the way to a bare top-level scalar (`"query-builder"`).
//   - When a table is set to `"no"` (the default), Metabase prunes that entry from the response, so the returned
//     object has fewer keys than the one that was applied.
//
// In both cases Metabase only echoes a value semantically equal to what was applied, so the provider must keep the
// originally-applied serialization rather than overwrite the state with the reshaped response.
func perTableCreateQueries(noTables ...int) string {
	isNo := map[int]bool{}
	for _, t := range noTables {
		isNo[t] = true
	}
	tables := ""
	for id := 1; id <= 8; id++ {
		if id > 1 {
			tables += ", "
		}
		permission := "query-builder"
		if isNo[id] {
			permission = "no"
		}
		tables += fmt.Sprintf("%q = %q", fmt.Sprintf("%d", id), permission)
	}
	return fmt.Sprintf("jsonencode({ PUBLIC = { %s } })", tables)
}

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
      create_queries = %s
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
					fmt.Sprintf("%q", string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0QueryBuilderAndNative)),
					"\"unrestricted\"",
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_permissions_graph.graph", "advanced_permissions", "false"),
					resource.TestCheckResourceAttrSet("metabase_permissions_graph.graph", "revision"),
				),
			},
			{
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					fmt.Sprintf("%q", string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No)),
					"\"unrestricted\"",
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_permissions_graph.graph", "advanced_permissions", "false"),
					resource.TestCheckResourceAttrSet("metabase_permissions_graph.graph", "revision"),
				),
			},
			{
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					fmt.Sprintf("%q", string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No)),
					"jsonencode({ public = \"unrestricted\" })",
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_permissions_graph.graph", "advanced_permissions", "false"),
					resource.TestCheckResourceAttrSet("metabase_permissions_graph.graph", "revision"),
				),
			},
			{
				// `create-queries` does NOT share `view-data`'s `{ <schema> = <scalar> }` granularity. Its only valid
				// object form is per-schema/per-table (`{ <schema> = { <table-id> = <perm> } }`), and at the table level
				// Metabase only accepts `query-builder`/`no` (never `query-builder-and-native`, which is database-wide).
				// Table 6 is `ACCOUNTS` in the bundled sample database. Metabase echoes this exact shape, so it round-trips.
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					"jsonencode({ PUBLIC = { \"6\" = \"query-builder\" } })",
					"jsonencode({ public = \"unrestricted\" })",
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_permissions_graph.graph", "advanced_permissions", "false"),
					resource.TestCheckResourceAttrSet("metabase_permissions_graph.graph", "revision"),
				),
			},
			{
				// A uniform per-table object is accepted on write, but Metabase collapses the response to a bare
				// top-level scalar. The provider must keep the applied object serialization to stay idempotent.
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					perTableCreateQueries(),
					"jsonencode({ public = \"unrestricted\" })",
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_permissions_graph.graph", "advanced_permissions", "false"),
					resource.TestCheckResourceAttrSet("metabase_permissions_graph.graph", "revision"),
				),
			},
			{
				// Setting a single table to "no" makes Metabase prune that entry from the response, so the returned
				// object has fewer keys than the applied one (an object→object reshape, not a scalar collapse). The
				// provider must keep the applied serialization, otherwise the resource shows a perpetual diff.
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					perTableCreateQueries(6),
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
