package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/internal/graphlock"
	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Returns the raw permissions from the permissions graph for the given group and database, or an empty string if there
// are none.
func testAccGetDatabasePermissionsJson(groupId string, databaseId string) (string, error) {
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
	raw, err := testAccGetDatabasePermissionsJson(groupId, databaseId)
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
func testAccCheckDatabasePermissionMatchesState(resourceName string) resource.TestCheckFunc {
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
					testAccCheckDatabasePermissionMatchesState("metabase_database_permission.a"),
					testAccCheckDatabasePermissionMatchesState("metabase_database_permission.b"),
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
					testAccCheckDatabasePermissionMatchesState("metabase_database_permission.a"),
					resource.TestCheckResourceAttr("metabase_database_permission.a", "create_queries", "query-builder-and-native"),
					func(s *terraform.State) error {
						return testAccCheckRevokedDatabasePermissions(getGroupB(), func(*terraform.State) (string, error) { return "1", nil })(s)
					},
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

	_, diags := updateDatabasePermissions(context.Background(), testAccMetabaseClient, graphlock.NewPermissionsGraphTracker(), mustParseInt64(t, groupId), mustParseInt64(t, databaseId), p)
	if diags.HasError() {
		t.Fatalf("Failed to set permissions: %v", diags)
	}
}

// Checks that the edges of destroyed resources no longer grant permissions to create queries.
func testAccCheckDatabasePermissionDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "metabase_database_permission" {
			continue
		}

		permissions, err := testAccGetDatabasePermissions(rs.Primary.Attributes["group"], rs.Primary.Attributes["database"])
		if err != nil {
			// The group may have been deleted along with its permissions.
			if strings.Contains(err.Error(), "contains no permissions") {
				continue
			}
			return err
		}

		if !permissions.IsRevoked() {
			b, _ := json.Marshal(permissions)
			return fmt.Errorf("Expected the permissions %s to be revoked, got: %s.", rs.Primary.ID, b)
		}
	}

	return nil
}

func TestAccDatabasePermissionResourceValidation(t *testing.T) {
	missingDatabase := func(group string) string {
		return fmt.Sprintf(`
resource "metabase_database_permission" "missing_database" {
  group          = %s
  database       = 999999
  view_data      = "unrestricted"
  create_queries = "no"
}
`, group)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      providerApiKeyConfig + testAccDatabasePermission("admin", "2", "query-builder"),
				ExpectError: regexp.MustCompile("Permissions for the Administrators group cannot be managed"),
			},
			{
				// A missing group is reported by the plan, rather than failing the update with a foreign key violation.
				Config:      providerApiKeyConfig + testAccDatabasePermission("missing_group", "999999", "query-builder"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Group not found"),
			},
			{
				Config:      providerApiKeyConfig + missingDatabase("1"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Database not found"),
			},
			{
				// Metabase revokes `create-queries` and `download` when `view-data` is `blocked`.
				Config: providerApiKeyConfig + `
resource "metabase_database_permission" "blocked" {
  group          = 1
  database       = 1
  view_data      = "blocked"
  create_queries = "query-builder"
  download = {
    schemas = "full"
  }
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Invalid permissions for blocked data.*Invalid permissions for blocked data`),
			},
			{
				// Both errors are reported at once.
				Config:      providerApiKeyConfig + missingDatabase("999999"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Group not found.*Database not found`),
			},
		},
	})
}

// Deletes a group which is not managed by Terraform. Metabase drops the edges of a deleted group from the permissions
// graph. The resource is considered deleted, so the next apply tries to create it again, and fails.
func TestAccDatabasePermissionResourceDeletedGroup(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("Acceptance tests skipped unless env '%s' set", resource.EnvTfAcc)
	}

	groupId := testAccCreateBystanderGroup(t, "🗑️ Database permission deleted group")
	config := providerApiKeyConfig + testAccDatabasePermission("deleted", strconv.Itoa(groupId), "query-builder")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				PreConfig: func() {
					resp, err := testAccMetabaseClient.DeletePermissionsGroupWithResponse(context.Background(), groupId)
					if err != nil || resp.StatusCode() != 204 {
						t.Fatalf("Failed to delete the group: %v", err)
					}
				},
				Config:      config,
				ExpectError: regexp.MustCompile("Group not found"),
			},
		},
	})
}

// Deletes a database which is not managed by Terraform. Metabase drops the edges of a deleted database from the
// permissions graph. The resource is considered deleted, so the next apply tries to create it again, and fails.
func TestAccDatabasePermissionResourceDeletedDatabase(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("Acceptance tests skipped unless env '%s' set", resource.EnvTfAcc)
	}

	databaseId := testAccCreateBystanderDatabase(t, "🗑️ Database permission deleted database")
	config := providerApiKeyConfig + fmt.Sprintf(`
resource "metabase_permissions_group" "deleted_database" {
  name = "🗑️ Database permission deleted database"
}

resource "metabase_database_permission" "deleted" {
  group          = metabase_permissions_group.deleted_database.id
  database       = %d
  view_data      = "unrestricted"
  create_queries = "query-builder"
}
`, databaseId)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
			},
			{
				PreConfig: func() {
					resp, err := testAccMetabaseClient.DeleteDatabaseWithResponse(context.Background(), databaseId)
					if err != nil || resp.StatusCode() != 204 {
						t.Fatalf("Failed to delete the database: %v, %v", err, resp)
					}
				},
				Config:      config,
				ExpectError: regexp.MustCompile("Database not found"),
			},
		},
	})
}

// Creates a database directly using the Metabase API, connected to the same PostgreSQL database as Metabase, and
// returns its ID. The database is deleted at the end of the test, unless it was deleted by the test.
func testAccCreateBystanderDatabase(t *testing.T, name string) int {
	t.Helper()

	var details metabase.DatabaseDetails
	if err := details.FromDatabaseDetailsCustom(metabase.DatabaseDetailsCustom{
		"host":           os.Getenv("PG_HOST"),
		"port":           5432,
		"dbname":         os.Getenv("PG_DATABASE"),
		"user":           os.Getenv("PG_USER"),
		"password":       os.Getenv("PG_PASSWORD"),
		"ssl":            false,
		"tunnel-enabled": false,
	}); err != nil {
		t.Fatalf("Failed to build the database details: %v", err)
	}

	response, err := testAccMetabaseClient.CreateDatabaseWithResponse(context.Background(), metabase.CreateDatabaseBody{
		Name:    name,
		Engine:  "postgres",
		Details: details,
	})
	if err != nil || response.StatusCode() != 200 {
		t.Fatalf("Failed to create the database: %v, %v", err, response)
	}

	databaseId := response.JSON200.Id
	t.Cleanup(func() {
		_, _ = testAccMetabaseClient.DeleteDatabaseWithResponse(context.Background(), databaseId)
	})

	return databaseId
}

// Creates many database permissions at once. Terraform applies them in parallel, which the provider should serialize.
func TestAccDatabasePermissionResourceParallel(t *testing.T) {
	const count = 8

	config := providerApiKeyConfig
	checks := []resource.TestCheckFunc{}
	for i := range count {
		config += fmt.Sprintf(`
resource "metabase_permissions_group" "parallel_%[1]d" {
  name = "🏎️ Parallel %[1]d"
}

resource "metabase_database_permission" "parallel_%[1]d" {
  group          = metabase_permissions_group.parallel_%[1]d.id
  database       = 1
  view_data      = "unrestricted"
  create_queries = "query-builder"
}
`, i)
		checks = append(checks, testAccCheckDatabasePermissionMatchesState(fmt.Sprintf("metabase_database_permission.parallel_%d", i)))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDatabasePermissionDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.ComposeAggregateTestCheckFunc(checks...),
			},
		},
	})
}

func testAccDatabasePermissionDrift(createQueries string) string {
	return fmt.Sprintf(`
resource "metabase_permissions_group" "drift" {
  name = "🌊 Database permission drift"
}

resource "metabase_database_permission" "drift" {
  group          = metabase_permissions_group.drift.id
  database       = 1
  view_data      = "unrestricted"
  create_queries = %s
}
`,
		createQueries,
	)
}

func TestAccDatabasePermissionResourceDrift(t *testing.T) {
	var groupId string
	getGroup := func() string { return groupId }

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDatabasePermissionDestroy,
		Steps: []resource.TestStep{
			{
				// The framework fails the step if the plan after the apply is not empty, i.e. if the reshaped response
				// is reported as a change. Every table has the same value, so Metabase collapses its response to a
				// single value.
				Config: providerApiKeyConfig + testAccDatabasePermissionDrift(perTableCreateQueries()),
			},
			{
				// Metabase prunes the table set to "no" from its response, which should not be reported as a change
				// either.
				Config: providerApiKeyConfig + testAccDatabasePermissionDrift(perTableCreateQueries(6)),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccStoreAttribute("metabase_database_permission.drift", "group", &groupId),
				),
			},
			{
				// A change made outside of Terraform (e.g. in the Metabase interface) is reported.
				PreConfig: func() {
					testAccSetDatabasePermissions(t, groupId, "1", `{"view-data":"unrestricted","create-queries":"query-builder-and-native"}`)
				},
				Config:             providerApiKeyConfig + testAccDatabasePermissionDrift(perTableCreateQueries(6)),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying the configuration reverts the change.
				Config: providerApiKeyConfig + testAccDatabasePermissionDrift(perTableCreateQueries(6)),
				Check: resource.ComposeAggregateTestCheckFunc(
					func(s *terraform.State) error {
						createQueries, err := testAccGetCreateQueries(getGroup(), "1")
						if err != nil {
							return err
						}
						if createQueries == string(metabase.PermissionsGraphDatabasePermissionsCreateQueries0QueryBuilderAndNative) {
							return fmt.Errorf("Expected the change made outside of Terraform to be reverted.")
						}
						return nil
					},
				),
			},
		},
	})
}

// Reads the graph restricted to the group of the edge. Metabase drops the edges of deleted groups and databases from
// the graph, which is how the resource is considered deleted. The edges themselves are found by
// `metabase.PermissionsGraph.FindDatabasePermissions`.
func TestGetDatabasePermissions(t *testing.T) {
	fake, client := newFakeMetabaseServer(t)
	fake.permissionsGraph["3"] = map[string]json.RawMessage{
		"1": json.RawMessage(`{"view-data":"unrestricted","create-queries":"query-builder"}`),
	}
	fake.permissionsGraph["4"] = map[string]json.RawMessage{
		"1": json.RawMessage(`{"view-data":"unrestricted"}`),
	}
	fake.missingGroups["4"] = true

	tests := map[string]struct {
		group         int64
		database      int64
		createQueries string
	}{
		"existing edge":           {group: 3, database: 1, createQueries: "query-builder"},
		"group which was deleted": {group: 4, database: 1},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			permissions, diags := getDatabasePermissions(context.Background(), client, test.group, test.database)
			if diags.HasError() {
				t.Fatalf("Unexpected error: %v", diags)
			}

			if test.createQueries == "" {
				if permissions != nil {
					t.Errorf("Expected no permissions, got %v.", permissions)
				}
				return
			}

			if permissions == nil || permissions.CreateQueries == nil {
				t.Fatalf("Expected permissions, got %v.", permissions)
			}
			createQueries, err := permissions.CreateQueries.AsPermissionsGraphDatabasePermissionsCreateQueries0()
			if err != nil || string(createQueries) != test.createQueries {
				t.Errorf("Expected create-queries %q, got %q (%v).", test.createQueries, createQueries, err)
			}
		})
	}
}

func TestWriteDatabasePermissionsOnlySendsConfiguredOptionalPermissions(t *testing.T) {
	fake, client := newFakeMetabaseServer(t)
	// The update is forced, so it is accepted even though the graph has changed since revision 0.
	fake.permissionsGraphRevision = 3
	r := &DatabasePermissionResource{MetabaseBaseResource{metabaseResourceData: metabaseResourceData{
		client:           client,
		permissionsGraph: graphlock.NewPermissionsGraphTracker(),
	}}}

	previousDownload := types.ObjectValueMust(accessPermissionsObjectType.AttrTypes, map[string]attr.Value{
		"schemas": types.StringValue("full"),
	})
	plan := DatabasePermissionResourceModel{DatabasePermissions: DatabasePermissions{
		Group:         types.Int64Value(3),
		Database:      types.Int64Value(1),
		ViewData:      types.StringValue("unrestricted"),
		CreateQueries: types.StringValue("query-builder"),
		// A value from the previous state, which should not be sent back as it is not in the configuration.
		Download:  previousDownload,
		DataModel: types.ObjectUnknown(accessPermissionsObjectType.AttrTypes),
		Details:   types.StringUnknown(),
	}}
	config := plan
	config.Download = types.ObjectNull(accessPermissionsObjectType.AttrTypes)
	config.DataModel = types.ObjectNull(accessPermissionsObjectType.AttrTypes)
	config.Details = types.StringNull()

	_, diags := r.writeDatabasePermissions(context.Background(), makeRequestedDatabasePermissions(plan, config))
	if diags.HasError() {
		t.Fatalf("Unexpected error: %v", diags)
	}

	if len(fake.acceptedUpdates) != 1 {
		t.Fatalf("Expected a single update, got %d.", len(fake.acceptedUpdates))
	}
	var sent map[string]any
	if err := json.Unmarshal(fake.acceptedUpdates[0]["3"]["1"], &sent); err != nil {
		t.Fatalf("Failed to parse the sent permissions: %v", err)
	}
	for _, key := range []string{"download", "data-model", "details"} {
		if _, ok := sent[key]; ok {
			t.Errorf("Expected %s not to be sent, got: %v", key, sent)
		}
	}
}

// The permissions may be unknown when the configuration is validated, so they are checked again before the update.
func TestWriteDatabasePermissionsChecksPermissions(t *testing.T) {
	fake, client := newFakeMetabaseServer(t)
	r := &DatabasePermissionResource{MetabaseBaseResource{metabaseResourceData: metabaseResourceData{
		client:           client,
		permissionsGraph: graphlock.NewPermissionsGraphTracker(),
	}}}

	plan := DatabasePermissionResourceModel{DatabasePermissions: DatabasePermissions{
		Group:         types.Int64Value(3),
		Database:      types.Int64Value(1),
		ViewData:      types.StringValue("blocked"),
		CreateQueries: types.StringValue("query-builder"),
		Download:      types.ObjectUnknown(accessPermissionsObjectType.AttrTypes),
		DataModel:     types.ObjectUnknown(accessPermissionsObjectType.AttrTypes),
		Details:       types.StringUnknown(),
	}}
	config := plan
	config.Download = types.ObjectNull(accessPermissionsObjectType.AttrTypes)
	config.DataModel = types.ObjectNull(accessPermissionsObjectType.AttrTypes)
	config.Details = types.StringNull()

	_, diags := r.writeDatabasePermissions(context.Background(), makeRequestedDatabasePermissions(plan, config))
	checkAttributeError(t, diags, "create_queries", "Invalid permissions for blocked data.")

	if len(fake.acceptedUpdates) != 0 {
		t.Errorf("Expected no update, got %d.", len(fake.acceptedUpdates))
	}
}

func TestWrittenDatabasePermissionsEqual(t *testing.T) {
	parse := func(s string) metabase.PermissionsGraphDatabasePermissions {
		var p metabase.PermissionsGraphDatabasePermissions
		if err := json.Unmarshal([]byte(s), &p); err != nil {
			t.Fatalf("Failed to parse permissions: %v", err)
		}
		return p
	}

	tests := map[string]struct {
		a        string
		b        string
		expected bool
	}{
		"key order and other permissions ignored": {
			a:        `{"view-data":"unrestricted","create-queries":{"PUBLIC":{"1":"query-builder","2":"no"}},"download":{"schemas":"full"}}`,
			b:        `{"create-queries":{"PUBLIC":{"2":"no","1":"query-builder"}},"view-data":"unrestricted"}`,
			expected: true,
		},
		"different create queries": {
			a:        `{"view-data":"unrestricted","create-queries":"query-builder"}`,
			b:        `{"view-data":"unrestricted","create-queries":"query-builder-and-native"}`,
			expected: false,
		},
		"missing create queries": {
			a:        `{"view-data":"unrestricted","create-queries":"query-builder"}`,
			b:        `{"view-data":"unrestricted"}`,
			expected: false,
		},
		"reshaped view data": {
			a:        `{"view-data":{"PUBLIC":"unrestricted"}}`,
			b:        `{"view-data":"unrestricted"}`,
			expected: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			a := makeWrittenDatabasePermissions(parse(test.a))
			b := makeWrittenDatabasePermissions(parse(test.b))
			if actual := a.equal(b); actual != test.expected {
				t.Errorf("Expected %t, got %t.", test.expected, actual)
			}
		})
	}
}

func TestMakeWrittenDatabasePermissions(t *testing.T) {
	var p metabase.PermissionsGraphDatabasePermissions
	if err := json.Unmarshal([]byte(`{"view-data":"unrestricted","create-queries":{"PUBLIC":{"1":"query-builder"}},"download":{"schemas":"full"},"details":"yes"}`), &p); err != nil {
		t.Fatalf("Failed to parse permissions: %v", err)
	}

	written, err := json.Marshal(makeWrittenDatabasePermissions(p))
	if err != nil {
		t.Fatalf("Failed to serialize the permissions: %v", err)
	}

	// This is the JSON value stored in the private state.
	expected := `{"view-data":"unrestricted","create-queries":{"PUBLIC":{"1":"query-builder"}}}`
	if string(written) != expected {
		t.Errorf("Expected %s, got %s.", expected, written)
	}
}

// Declares an edge with the lowest permissions, i.e. the same permissions as a revoked edge. Unlike the
// `metabase_permissions_graph` resource, this resource does not consider such an edge absent, as it is its own edge.
func TestAccDatabasePermissionResourceLowestPermissions(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDatabasePermissionDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerApiKeyConfig + `
resource "metabase_permissions_group" "lowest" {
  name = "🔒 Lowest permissions"
}

resource "metabase_database_permission" "lowest" {
  group          = metabase_permissions_group.lowest.id
  database       = 1
  view_data      = "unrestricted"
  create_queries = "no"
  download = {
    schemas = "none"
  }
}
`,
				Check: func(s *terraform.State) error {
					group, err := testAccResourceAttribute(s, "metabase_database_permission.lowest", "group")
					if err != nil {
						return err
					}
					return testAccCheckRevokedDatabasePermissions(group, func(*terraform.State) (string, error) { return "1", nil })(s)
				},
			},
			{
				ResourceName:      "metabase_database_permission.lowest",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Returns the value of an attribute of a resource in the state.
func testAccResourceAttribute(s *terraform.State, resourceName string, attribute string) (string, error) {
	rs, ok := s.RootModule().Resources[resourceName]
	if !ok {
		return "", fmt.Errorf("Failed to find resource %s in state.", resourceName)
	}

	return rs.Primary.Attributes[attribute], nil
}

func TestCheckDatabasePermissions(t *testing.T) {
	download := func(schemas string) types.Object {
		return types.ObjectValueMust(accessPermissionsObjectType.AttrTypes, map[string]attr.Value{"schemas": types.StringValue(schemas)})
	}

	tests := map[string]struct {
		viewData       types.String
		createQueries  types.String
		download       types.Object
		expectedErrors []string
	}{
		"blocked with revoked permissions": {types.StringValue("blocked"), types.StringValue("no"), download("none"), nil},
		"blocked without download":         {types.StringValue("blocked"), types.StringValue("no"), types.ObjectNull(accessPermissionsObjectType.AttrTypes), nil},
		"blocked with create queries":      {types.StringValue("blocked"), types.StringValue("query-builder"), types.ObjectNull(accessPermissionsObjectType.AttrTypes), []string{"create_queries"}},
		"blocked with download":            {types.StringValue("blocked"), types.StringValue("no"), download("full"), []string{"download"}},
		"blocked with both":                {types.StringValue("blocked"), types.StringValue("query-builder"), download("full"), []string{"create_queries", "download"}},
		"blocked with unknown values":      {types.StringValue("blocked"), types.StringUnknown(), types.ObjectUnknown(accessPermissionsObjectType.AttrTypes), nil},
		"unrestricted":                     {types.StringValue("unrestricted"), types.StringValue("query-builder"), download("full"), nil},
		"unknown view data":                {types.StringUnknown(), types.StringValue("query-builder"), download("full"), nil},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			diags := checkDatabasePermissions(DatabasePermissions{
				ViewData:      test.viewData,
				CreateQueries: test.createQueries,
				Download:      test.download,
			})

			if len(diags) != len(test.expectedErrors) {
				t.Fatalf("Expected %d errors, got: %v", len(test.expectedErrors), diags)
			}
			for i, attribute := range test.expectedErrors {
				d, ok := diags[i].(diag.DiagnosticWithPath)
				if !ok || !d.Path().Equal(path.Root(attribute)) {
					t.Errorf("Expected an error on %s, got: %v", attribute, diags[i])
				}
			}
		})
	}
}
