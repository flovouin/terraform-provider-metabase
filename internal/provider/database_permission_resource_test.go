package provider

import (
	"context"
	"encoding/json"
	"fmt"
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

	diags := updateDatabasePermissions(context.Background(), testAccMetabaseClient, graphlock.NewPermissionsGraphTracker(), mustParseInt64(t, groupId), mustParseInt64(t, databaseId), p)
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

		if !isRevokedDatabasePermissions(*permissions) {
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

func testAccDatabasePermissionDrift() string {
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
		// Metabase prunes the table set to "no" from its response, which should not be reported as a change.
		perTableCreateQueries(6),
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
				// is reported as a change.
				Config: providerApiKeyConfig + testAccDatabasePermissionDrift(),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccStoreAttribute("metabase_database_permission.drift", "group", &groupId),
				),
			},
			{
				// A change made outside of Terraform (e.g. in the Metabase interface) is reported.
				PreConfig: func() {
					testAccSetDatabasePermissions(t, groupId, "1", `{"view-data":"unrestricted","create-queries":"query-builder-and-native"}`)
				},
				Config:             providerApiKeyConfig + testAccDatabasePermissionDrift(),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying the configuration reverts the change.
				Config: providerApiKeyConfig + testAccDatabasePermissionDrift(),
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

func TestWriteDatabasePermissionsOnlySendsConfiguredOptionalPermissions(t *testing.T) {
	fake, client := newFakeMetabaseServer(t)
	r := &DatabasePermissionResource{MetabaseBaseResource{metabaseResourceData: metabaseResourceData{
		client:           client,
		permissionsGraph: graphlock.NewPermissionsGraphTracker(),
	}}}

	previousDownload := types.ObjectValueMust(accessPermissionsObjectType.AttrTypes, map[string]attr.Value{
		"schemas": types.StringValue("full"),
	})
	plan := DatabasePermissionResourceModel{
		Group:         types.Int64Value(3),
		Database:      types.Int64Value(1),
		ViewData:      types.StringValue("unrestricted"),
		CreateQueries: types.StringValue("query-builder"),
		// A value from the previous state, which should not be sent back as it is not in the configuration.
		Download:  previousDownload,
		DataModel: types.ObjectUnknown(accessPermissionsObjectType.AttrTypes),
		Details:   types.StringUnknown(),
	}
	config := plan
	config.Download = types.ObjectNull(accessPermissionsObjectType.AttrTypes)
	config.DataModel = types.ObjectNull(accessPermissionsObjectType.AttrTypes)
	config.Details = types.StringNull()

	_, diags := r.writeDatabasePermissions(context.Background(), &plan, config)
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

func TestMakeComparableDatabasePermissions(t *testing.T) {
	parse := func(s string) metabase.PermissionsGraphDatabasePermissions {
		var p metabase.PermissionsGraphDatabasePermissions
		if err := json.Unmarshal([]byte(s), &p); err != nil {
			t.Fatalf("Failed to parse permissions: %v", err)
		}
		return p
	}

	a, errA := makeComparableDatabasePermissions(parse(`{"view-data":"unrestricted","create-queries":{"PUBLIC":{"1":"query-builder","2":"no"}},"download":{"schemas":"full"}}`))
	b, errB := makeComparableDatabasePermissions(parse(`{"create-queries":{"PUBLIC":{"2":"no","1":"query-builder"}},"view-data":"unrestricted"}`))
	if errA != nil || errB != nil {
		t.Fatalf("Unexpected errors: %v, %v", errA, errB)
	}
	if string(a) != string(b) {
		t.Errorf("Expected the key order and other permissions to be ignored, got %s and %s.", a, b)
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
				// Metabase omits revoked permissions, so an imported `download` permission set to `none` is read as null.
				ImportStateVerifyIgnore: []string{"download"},
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

func TestCheckBlockedViewData(t *testing.T) {
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
			diags := checkBlockedViewData(DatabasePermissionResourceModel{
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
