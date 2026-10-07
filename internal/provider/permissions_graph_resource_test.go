package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// perTableCreateQueries builds a `create_queries` `jsonencode` expression covering every table in the sample
// database's schema (IDs 1-8). Tables listed in `noTables` are set to `"no"`, the rest to `"query-builder"`.
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
	return fmt.Sprintf("jsonencode({ %q = { %s } })", sampleDatabaseSchema, tables)
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
					fmt.Sprintf("jsonencode({ %q = { \"6\" = \"query-builder\" } })", sampleDatabaseSchema),
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

// Returns the permissions of the given group on the given database, as returned by the Metabase API.
func testAccGetDatabasePermissions(groupId string, databaseId string) (*metabase.PermissionsGraphDatabasePermissions, error) {
	response, err := testAccMetabaseClient.GetPermissionsGraphWithResponse(context.Background())
	if err != nil {
		return nil, err
	}
	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("Received unexpected response from the Metabase API when getting the permissions graph.")
	}

	permissions, ok := response.JSON200.Groups[groupId][databaseId]
	if !ok {
		return nil, fmt.Errorf("The permissions graph contains no permissions for group %s and database %s.", groupId, databaseId)
	}

	return &permissions, nil
}

// Checks that the permissions of the given group on the given database have been revoked in Metabase.
func testAccCheckRevokedDatabasePermissions(groupId string, databaseId func(*terraform.State) (string, error)) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		dbId, err := databaseId(s)
		if err != nil {
			return err
		}

		permissions, err := testAccGetDatabasePermissions(groupId, dbId)
		if err != nil {
			return err
		}

		if !isRevokedDatabasePermissions(*permissions) {
			b, _ := json.Marshal(permissions)
			return fmt.Errorf("Expected the permissions of group %s on database %s to be revoked, got: %s.", groupId, dbId, b)
		}

		return nil
	}
}

// Returns the ID of the given resource in the state.
func testAccResourceId(resourceName string) func(*terraform.State) (string, error) {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		return rs.Primary.ID, nil
	}
}

// Creates a database in the same apply as an update of the permissions graph. Metabase grants the All Users group full
// access to the new database, which is not part of the configuration. The graph returned after the update contains this
// (group, database) pair, which is reported as drift after the apply rather than failing it, and revoked by the next
// apply.
func TestAccPermissionsGraphResourceWithNewDatabase(t *testing.T) {
	newDatabase := fmt.Sprintf(`
resource "metabase_database" "new" {
  name = "🆕 New database"

  custom_details = {
    engine = "postgres"

    details_json = jsonencode({
      host           = "%s"
      port           = 5432
      dbname         = "%s"
      user           = "%s"
      password       = "%s"
      ssl            = false
      tunnel-enabled = false
    })

    redacted_attributes = [
      "password",
    ]
  }
}
`,
		os.Getenv("PG_HOST"),
		os.Getenv("PG_DATABASE"),
		os.Getenv("PG_USER"),
		os.Getenv("PG_PASSWORD"),
	)
	// The graph is updated after the database is created, once Metabase has granted the default permissions.
	graph := `
import {
  to = metabase_permissions_graph.graph
  id = "1"
}

resource "metabase_permissions_graph" "graph" {
  advanced_permissions = false

  permissions = [
    {
      group    = 1
      database = 1 # The sample database, not the new one.
      download = {
        schemas = "full"
      }
      view_data      = "unrestricted"
      create_queries = "no" # Changed from the first step, such that the graph is updated.
    },
  ]

  depends_on = [metabase_database.new]
}
`
	config := providerApiKeyConfig + newDatabase + graph

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Sets other permissions for the All Users group on the sample database, such that the next step updates the
				// graph.
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					fmt.Sprintf("%q", string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0QueryBuilderAndNative)),
					"\"unrestricted\"",
				),
			},
			{
				// The apply succeeds, but the full access of the All Users group to the new database is reported as drift.
				Config:             config,
				ExpectNonEmptyPlan: true,
			},
			{
				// The full access of the All Users group to the new database is revoked, and no longer reported once revoked.
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckRevokedDatabasePermissions("1", testAccResourceId("metabase_database.new")),
				),
			},
		},
	})
}

func testAccPermissionsGraphResourceWithoutPermissions() string {
	return `
import {
  to = metabase_permissions_graph.graph
  id = "1"
}

resource "metabase_permissions_graph" "graph" {
  advanced_permissions = false

  permissions = []
}
`
}

// Removes a (group, database) pair from the configuration, which revokes its permissions.
func TestAccPermissionsGraphResourceRevokesRemovedPermissions(t *testing.T) {
	sampleDatabase := func(*terraform.State) (string, error) { return "1", nil }

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					fmt.Sprintf("%q", string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0QueryBuilderAndNative)),
					"\"unrestricted\"",
				),
			},
			{
				// The revoked permissions are no longer reported, otherwise the plan after the apply would not be empty.
				Config: providerApiKeyConfig + testAccPermissionsGraphResourceWithoutPermissions(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_permissions_graph.graph", "permissions.#", "0"),
					testAccCheckRevokedDatabasePermissions("1", sampleDatabase),
				),
			},
			{
				// Restores the permissions of the All Users group on the sample database for the other tests.
				Config: providerApiKeyConfig + testAccPermissionsGraphResource(
					fmt.Sprintf("%q", string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0QueryBuilderAndNative)),
					"\"unrestricted\"",
				),
			},
		},
	})
}

func TestIsRevokedDatabasePermissions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		permissions string
		expected    bool
	}{
		"revoked, as returned by the free edition": {`{"view-data":"unrestricted"}`, true},
		"revoked, with explicit values":            {`{"view-data":"unrestricted","create-queries":"no","download":{"schemas":"none"},"data-model":{"schemas":"none"},"details":"no"}`, true},
		"blocked view data":                        {`{"view-data":"blocked"}`, false},
		"granular view data":                       {`{"view-data":{"PUBLIC":"unrestricted"}}`, false},
		"create queries":                           {`{"view-data":"unrestricted","create-queries":"query-builder"}`, false},
		"download":                                 {`{"view-data":"unrestricted","download":{"schemas":"full"}}`, false},
		"data model":                               {`{"view-data":"unrestricted","data-model":{"schemas":"all"}}`, false},
		"details":                                  {`{"view-data":"unrestricted","details":"yes"}`, false},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var permissions metabase.PermissionsGraphDatabasePermissions
			if err := json.Unmarshal([]byte(test.permissions), &permissions); err != nil {
				t.Fatalf("Failed to parse permissions: %v", err)
			}

			if actual := isRevokedDatabasePermissions(permissions); actual != test.expected {
				t.Errorf("Expected %v, got %v.", test.expected, actual)
			}
		})
	}
}

func TestMakeRevokedDatabasePermissions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		advancedPermissions bool
		expected            string
	}{
		"free edition": {
			expected: `{"create-queries":"no","download":{"schemas":"none"},"view-data":"unrestricted"}`,
		},
		"advanced permissions": {
			advancedPermissions: true,
			expected:            `{"create-queries":"no","data-model":{"schemas":"none"},"details":"no","download":{"schemas":"none"},"view-data":"unrestricted"}`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			permissions := makeRevokedDatabasePermissions(test.advancedPermissions)

			actual, err := json.Marshal(permissions)
			if err != nil {
				t.Fatalf("Failed to marshal permissions: %v", err)
			}
			if string(actual) != test.expected {
				t.Errorf("Expected %s, got %s.", test.expected, actual)
			}
			if !isRevokedDatabasePermissions(permissions) {
				t.Errorf("Expected the revoked permissions to be detected as revoked.")
			}
		})
	}
}
