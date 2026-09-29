package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Creates a permissions group directly using the Metabase API, to act as a group managed outside of Terraform.
// The group is deleted at the end of the test.
func testAccCreateBystanderGroup(t *testing.T, name string) int {
	t.Helper()

	response, err := testAccMetabaseClient.CreatePermissionsGroupWithResponse(context.Background(), metabase.CreatePermissionsGroupBody{
		Name: name,
	})
	if err != nil || response.StatusCode() != 200 {
		t.Fatalf("Failed to create permissions group: %v, %v", err, response)
	}

	groupId := response.JSON200.Id
	t.Cleanup(func() {
		_, _ = testAccMetabaseClient.DeletePermissionsGroupWithResponse(context.Background(), groupId)
	})

	return groupId
}

// Returns the raw permissions from the permissions graph for the given group and database, or an empty string if there
// are none.
func testAccGetDatabasePermissions(groupId string, databaseId string) (string, error) {
	response, err := testAccMetabaseClient.GetPermissionsGraphWithResponse(context.Background())
	if err != nil {
		return "", err
	}
	if response.StatusCode() != 200 {
		return "", fmt.Errorf("Received unexpected response from the Metabase API when getting the permissions graph.")
	}

	permissions, ok := response.JSON200.Groups[groupId][databaseId]
	if !ok {
		return "", nil
	}

	b, err := json.Marshal(permissions)
	return string(b), err
}

// Returns the `create-queries` permission from the permissions graph for the given group and database.
func testAccGetCreateQueries(groupId string, databaseId string) (string, error) {
	raw, err := testAccGetDatabasePermissions(groupId, databaseId)
	if err != nil || raw == "" {
		return "", err
	}

	var permissions map[string]any
	if err := json.Unmarshal([]byte(raw), &permissions); err != nil {
		return "", err
	}

	createQueries, ok := permissions["create-queries"]
	if !ok {
		// Metabase omits the permission when it is `no`.
		return string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No), nil
	}

	b, err := json.Marshal(createQueries)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return string(b), nil
	}
	return s, nil
}

// Checks that the `create-queries` permission in Metabase matches the one from the resource in the state.
func testAccCheckDatabasePermissionExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		createQueries, err := testAccGetCreateQueries(rs.Primary.Attributes["group"], rs.Primary.Attributes["database"])
		if err != nil {
			return err
		}

		if createQueries != rs.Primary.Attributes["create_queries"] {
			return fmt.Errorf("Expected create-queries to be %q in Metabase, got %q.", rs.Primary.Attributes["create_queries"], createQueries)
		}

		return nil
	}
}

// Checks that the `create-queries` permission for the given group and database has the expected value in Metabase.
func testAccCheckCreateQueries(groupId func() string, databaseId string, expected string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		createQueries, err := testAccGetCreateQueries(groupId(), databaseId)
		if err != nil {
			return err
		}

		if createQueries != expected {
			return fmt.Errorf("Expected create-queries for group %s and database %s to be %q, got %q.", groupId(), databaseId, expected, createQueries)
		}

		return nil
	}
}

// Returns the value of an attribute of a resource in the state, to be used by checks in later steps.
func testAccStoreAttribute(resourceName string, attribute string, value *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		*value = rs.Primary.Attributes[attribute]
		return nil
	}
}

func testAccDatabasePermissionResource(permissions string) string {
	return fmt.Sprintf(`
resource "metabase_permissions_group" "permission_a" {
  name = "🔑 Database permission A"
}

resource "metabase_permissions_group" "permission_b" {
  name = "🔑 Database permission B"
}

%s
`,
		permissions,
	)
}

func testAccDatabasePermission(name string, group string, createQueries string) string {
	return fmt.Sprintf(`
resource "metabase_database_permission" "%s" {
  group          = %s
  database       = 1
  view_data      = "unrestricted"
  create_queries = "%s"
}
`,
		name,
		group,
		createQueries,
	)
}

func TestAccDatabasePermissionResource(t *testing.T) {
	var bystanderGroupId string
	var groupB string
	bystanderGroup := func() string { return bystanderGroupId }
	getGroupB := func() string { return groupB }

	checkBystanderIsUntouched := testAccCheckCreateQueries(bystanderGroup, "1", string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0QueryBuilder))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckDatabasePermissionDestroy,
			checkBystanderIsUntouched,
		),
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					// A group whose permissions are managed outside of Terraform, and should be left untouched.
					bystanderGroupId = strconv.Itoa(testAccCreateBystanderGroup(t, "🧍 Database permission bystander"))
					testAccSetDatabasePermissions(t, bystanderGroupId, "1", `{"view-data":"unrestricted","create-queries":"query-builder"}`)
				},
				Config: providerApiKeyConfig + testAccDatabasePermissionResource(
					testAccDatabasePermission("a", "metabase_permissions_group.permission_a.id", "query-builder")+
						testAccDatabasePermission("b", "metabase_permissions_group.permission_b.id", "query-builder-and-native"),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatabasePermissionExists("metabase_database_permission.a"),
					testAccCheckDatabasePermissionExists("metabase_database_permission.b"),
					resource.TestCheckResourceAttrPair("metabase_database_permission.a", "group", "metabase_permissions_group.permission_a", "id"),
					resource.TestCheckResourceAttr("metabase_database_permission.a", "database", "1"),
					resource.TestCheckResourceAttr("metabase_database_permission.a", "view_data", "unrestricted"),
					resource.TestCheckResourceAttr("metabase_database_permission.a", "create_queries", "query-builder"),
					resource.TestCheckResourceAttrSet("metabase_database_permission.a", "id"),
					testAccStoreAttribute("metabase_database_permission.b", "group", &groupB),
					checkBystanderIsUntouched,
				),
			},
			{
				ResourceName:      "metabase_database_permission.a",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Removing a resource only resets its own edge. The other edges are left untouched.
				Config: providerApiKeyConfig + testAccDatabasePermissionResource(
					testAccDatabasePermission("a", "metabase_permissions_group.permission_a.id", "query-builder-and-native"),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatabasePermissionExists("metabase_database_permission.a"),
					resource.TestCheckResourceAttr("metabase_database_permission.a", "create_queries", "query-builder-and-native"),
					testAccCheckCreateQueries(getGroupB, "1", string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No)),
					checkBystanderIsUntouched,
				),
			},
		},
	})
}

// Sets the permissions for the given group and database directly using the Metabase API.
func testAccSetDatabasePermissions(t *testing.T, groupId string, databaseId string, permissions string) {
	t.Helper()

	var p metabase.PermissionsGraphDatabasePermissions
	if err := json.Unmarshal([]byte(permissions), &p); err != nil {
		t.Fatalf("Failed to parse permissions: %v", err)
	}

	diags := updateDatabasePermissions(context.Background(), testAccMetabaseClient, mustParseInt64(t, groupId), mustParseInt64(t, databaseId), func(*metabase.PermissionsGraphDatabasePermissions) *metabase.PermissionsGraphDatabasePermissions {
		return &p
	})
	if diags.HasError() {
		t.Fatalf("Failed to set permissions: %v", diags)
	}
}

func mustParseInt64(t *testing.T, s string) int64 {
	t.Helper()

	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse integer %q: %v", s, err)
	}

	return i
}

// Checks that the edges of destroyed resources no longer grant permissions to create queries.
func testAccCheckDatabasePermissionDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "metabase_database_permission" {
			continue
		}

		createQueries, err := testAccGetCreateQueries(rs.Primary.Attributes["group"], rs.Primary.Attributes["database"])
		if err != nil {
			return err
		}

		// An empty value means the group has been deleted.
		if createQueries != "" && createQueries != string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0No) {
			return fmt.Errorf("Permissions %s still allow creating queries: %s.", rs.Primary.ID, createQueries)
		}
	}

	return nil
}

func TestAccDatabasePermissionResourceAdministrators(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      providerApiKeyConfig + testAccDatabasePermission("admin", "2", "query-builder"),
				ExpectError: regexp.MustCompile("Permissions for the Administrators group cannot be managed"),
			},
		},
	})
}

func testAccGraphEdgesParallel(count int) string {
	config := ""
	for i := range count {
		config += fmt.Sprintf(`
resource "metabase_permissions_group" "parallel_%[1]d" {
  name = "🏎️ Parallel %[1]d"
}

resource "metabase_collection" "parallel_%[1]d" {
  name = "🏎️ Parallel %[1]d"
}

resource "metabase_database_permission" "parallel_%[1]d" {
  group          = metabase_permissions_group.parallel_%[1]d.id
  database       = 1
  view_data      = "unrestricted"
  create_queries = "query-builder"
}

resource "metabase_collection_permission" "parallel_%[1]d" {
  group      = metabase_permissions_group.parallel_%[1]d.id
  collection = metabase_collection.parallel_%[1]d.id
  permission = "read"
}
`,
			i,
		)
	}

	return config
}

// Creates many edges at once. Terraform applies them in parallel, which the provider should serialize.
func TestAccGraphEdgesParallel(t *testing.T) {
	const count = 8

	checks := []resource.TestCheckFunc{}
	for i := range count {
		checks = append(checks,
			testAccCheckDatabasePermissionExists(fmt.Sprintf("metabase_database_permission.parallel_%d", i)),
			testAccCheckCollectionPermissionExists(fmt.Sprintf("metabase_collection_permission.parallel_%d", i)),
		)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckDatabasePermissionDestroy,
			testAccCheckCollectionPermissionDestroy,
		),
		Steps: []resource.TestStep{
			{
				Config: providerApiKeyConfig + testAccGraphEdgesParallel(count),
				Check:  resource.ComposeAggregateTestCheckFunc(checks...),
			},
		},
	})
}
