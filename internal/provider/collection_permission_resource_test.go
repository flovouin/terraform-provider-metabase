package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/internal/graphlock"
	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Returns the permission from the collection graph for the given group and collection, or `none` if there is none.
func testAccGetCollectionPermission(groupId string, collectionId string) (string, error) {
	response, err := testAccMetabaseClient.GetCollectionPermissionsGraphWithResponse(context.Background())
	if err != nil {
		return "", err
	}
	if response.StatusCode() != 200 {
		return "", fmt.Errorf("Received unexpected response from the Metabase API when getting the collection graph.")
	}

	permission, ok := response.JSON200.Groups[groupId][collectionId]
	if !ok {
		return string(metabase.CollectionPermissionLevelNone), nil
	}

	return string(permission), nil
}

// Checks that the permission in Metabase matches the one from the resource in the state.
func testAccCheckCollectionPermissionMatchesState(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		permission, err := testAccGetCollectionPermission(rs.Primary.Attributes["group"], rs.Primary.Attributes["collection"])
		if err != nil {
			return err
		}

		if permission != rs.Primary.Attributes["permission"] {
			return fmt.Errorf("Expected permission to be %q in Metabase, got %q.", rs.Primary.Attributes["permission"], permission)
		}

		return nil
	}
}

// Checks that the permission for the given group and collection has the expected value in Metabase.
func testAccCheckCollectionPermission(groupId func() string, collectionId func() string, expected string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		permission, err := testAccGetCollectionPermission(groupId(), collectionId())
		if err != nil {
			return err
		}

		if permission != expected {
			return fmt.Errorf("Expected permission for group %s and collection %s to be %q, got %q.", groupId(), collectionId(), expected, permission)
		}

		return nil
	}
}

// Checks that the edges of destroyed resources no longer grant any permission.
func testAccCheckCollectionPermissionDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "metabase_collection_permission" {
			continue
		}

		permission, err := testAccGetCollectionPermission(rs.Primary.Attributes["group"], rs.Primary.Attributes["collection"])
		if err != nil {
			return err
		}

		if permission != string(metabase.CollectionPermissionLevelNone) {
			return fmt.Errorf("Permission %s still exists: %s.", rs.Primary.ID, permission)
		}
	}

	return nil
}

// Sets the permission for the given group and collection directly using the Metabase API.
func testAccSetCollectionPermission(t *testing.T, groupId string, collectionId string, permission metabase.CollectionPermissionLevel) {
	t.Helper()

	diags := updateCollectionPermission(context.Background(), testAccMetabaseClient, graphlock.NewCollectionGraphTracker(), mustParseInt64(t, groupId), collectionId, permission)
	if diags.HasError() {
		t.Fatalf("Failed to set permission: %v", diags)
	}
}

// Returns the ID of a personal collection, from the collections visible to the test user.
func testAccGetPersonalCollectionId(t *testing.T) string {
	t.Helper()

	resp, err := testAccMetabaseClient.ListCollectionsWithResponse(context.Background(), &metabase.ListCollectionsParams{})
	if err != nil || resp.StatusCode() != 200 {
		t.Fatalf("Failed to list the collections: %v", err)
	}

	for _, c := range *resp.JSON200 {
		if c.PersonalOwnerId != nil {
			id, err := c.Id.AsCollectionId1()
			if err == nil {
				return strconv.Itoa(id)
			}
		}
	}

	t.Fatalf("No personal collection found.")
	return ""
}

func testAccCollectionPermissionResource(permissions string) string {
	return fmt.Sprintf(`
resource "metabase_permissions_group" "permission_a" {
  name = "🔑 Collection permission A"
}

resource "metabase_permissions_group" "permission_b" {
  name = "🔑 Collection permission B"
}

resource "metabase_collection" "permission" {
  name = "🔑 Collection permission"
}

%s
`,
		permissions,
	)
}

func testAccCollectionPermission(name string, group string, collection string, permission string) string {
	return fmt.Sprintf(`
resource "metabase_collection_permission" "%s" {
  group      = %s
  collection = %s
  permission = "%s"
}
`,
		name,
		group,
		collection,
		permission,
	)
}

func TestAccCollectionPermissionResource(t *testing.T) {
	var bystanderGroupId string
	var groupB string
	var collectionId string
	bystanderGroup := func() string { return bystanderGroupId }
	getGroupB := func() string { return groupB }
	getCollection := func() string { return collectionId }
	root := func() string { return "root" }

	checkBystanderIsUntouched := resource.ComposeAggregateTestCheckFunc(
		testAccCheckCollectionPermission(bystanderGroup, root, string(metabase.CollectionPermissionLevelRead)),
		testAccCheckCollectionPermission(bystanderGroup, getCollection, string(metabase.CollectionPermissionLevelWrite)),
	)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckCollectionPermissionDestroy,
			// Archived collections are no longer part of the graph, so only the root collection can be checked.
			testAccCheckCollectionPermission(bystanderGroup, root, string(metabase.CollectionPermissionLevelRead)),
		),
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					// A group whose permissions are managed outside of Terraform, and should be left untouched.
					bystanderGroupId = strconv.Itoa(testAccCreateBystanderGroup(t, "🧍 Collection permission bystander"))
					testAccSetCollectionPermission(t, bystanderGroupId, "root", metabase.CollectionPermissionLevelRead)
				},
				Config: providerApiKeyConfig + testAccCollectionPermissionResource(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccStoreAttribute("metabase_collection.permission", "id", &collectionId),
				),
			},
			{
				PreConfig: func() {
					testAccSetCollectionPermission(t, bystanderGroupId, collectionId, metabase.CollectionPermissionLevelWrite)
				},
				Config: providerApiKeyConfig + testAccCollectionPermissionResource(
					testAccCollectionPermission("a", "metabase_permissions_group.permission_a.id", "metabase_collection.permission.id", "write")+
						testAccCollectionPermission("b", "metabase_permissions_group.permission_b.id", "metabase_collection.permission.id", "read")+
						testAccCollectionPermission("a_root", "metabase_permissions_group.permission_a.id", `"root"`, "read"),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCollectionPermissionMatchesState("metabase_collection_permission.a"),
					testAccCheckCollectionPermissionMatchesState("metabase_collection_permission.b"),
					testAccCheckCollectionPermissionMatchesState("metabase_collection_permission.a_root"),
					resource.TestCheckResourceAttrPair("metabase_collection_permission.a", "group", "metabase_permissions_group.permission_a", "id"),
					resource.TestCheckResourceAttrPair("metabase_collection_permission.a", "collection", "metabase_collection.permission", "id"),
					resource.TestCheckResourceAttr("metabase_collection_permission.a", "permission", "write"),
					resource.TestCheckResourceAttr("metabase_collection_permission.a_root", "collection", "root"),
					resource.TestCheckResourceAttrSet("metabase_collection_permission.a", "id"),
					testAccStoreAttribute("metabase_collection_permission.b", "group", &groupB),
					checkBystanderIsUntouched,
				),
			},
			{
				ResourceName:      "metabase_collection_permission.a",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "metabase_collection_permission.a_root",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// `none` is omitted from the collection graph, and read as such.
				Config: providerApiKeyConfig + testAccCollectionPermissionResource(
					testAccCollectionPermission("a", "metabase_permissions_group.permission_a.id", "metabase_collection.permission.id", "write")+
						testAccCollectionPermission("b", "metabase_permissions_group.permission_b.id", "metabase_collection.permission.id", "read")+
						testAccCollectionPermission("a_root", "metabase_permissions_group.permission_a.id", `"root"`, "none"),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCollectionPermissionMatchesState("metabase_collection_permission.a_root"),
					resource.TestCheckResourceAttr("metabase_collection_permission.a_root", "permission", "none"),
					checkBystanderIsUntouched,
				),
			},
			{
				ResourceName:      "metabase_collection_permission.a_root",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Removing a resource only resets its own edge, which grants `read` until then. The other edges are left
				// untouched.
				Config: providerApiKeyConfig + testAccCollectionPermissionResource(
					testAccCollectionPermission("a", "metabase_permissions_group.permission_a.id", "metabase_collection.permission.id", "read")+
						testAccCollectionPermission("a_root", "metabase_permissions_group.permission_a.id", `"root"`, "none"),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCollectionPermissionMatchesState("metabase_collection_permission.a"),
					resource.TestCheckResourceAttr("metabase_collection_permission.a", "permission", "read"),
					testAccCheckCollectionPermission(getGroupB, getCollection, string(metabase.CollectionPermissionLevelNone)),
					checkBystanderIsUntouched,
				),
			},
		},
	})
}

func TestAccCollectionPermissionResourceValidation(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      providerApiKeyConfig + testAccCollectionPermission("admin", "2", `"root"`, "read"),
				ExpectError: regexp.MustCompile("Permissions for the Administrators group cannot be managed"),
			},
			{
				Config:      providerApiKeyConfig + testAccCollectionPermission("invalid", "1", `"root"`, "owner"),
				ExpectError: regexp.MustCompile("Invalid collection permission"),
			},
			{
				// Missing IDs are reported by the plan.
				Config:      providerApiKeyConfig + testAccCollectionPermission("missing", "1", `"999999"`, "read"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Collection not found"),
			},
			{
				Config:      providerApiKeyConfig + testAccCollectionPermission("missing_group", "999999", `"root"`, "read"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Group not found"),
			},
			{
				// Both errors are reported at once.
				Config:      providerApiKeyConfig + testAccCollectionPermission("missing_both", "999999", `"999999"`, "read"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Group not found.*Collection not found`),
			},
			{
				Config: providerApiKeyConfig + `
import {
  to = metabase_collection_permission.missing
  id = "1/999999"
}
` + testAccCollectionPermission("missing", "1", `"999999"`, "none"),
				ExpectError: regexp.MustCompile("Collection not found"),
			},
		},
	})
}

// Archives the collection outside of Terraform. The permission is considered deleted, like the collection, and both are
// created again.
func TestAccCollectionPermissionResourceArchivedCollection(t *testing.T) {
	var collectionId string
	config := providerApiKeyConfig + testAccCollectionPermissionResource(
		testAccCollectionPermission("a", "metabase_permissions_group.permission_a.id", "metabase_collection.permission.id", "read"),
	)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionPermissionDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccStoreAttribute("metabase_collection.permission", "id", &collectionId),
				),
			},
			{
				PreConfig: func() {
					resp, err := testAccMetabaseClient.UpdateCollectionWithBodyWithResponse(context.Background(), collectionId, "application/json", strings.NewReader(`{"archived":true}`))
					if err != nil || resp.StatusCode() != 200 {
						t.Fatalf("Failed to archive the collection: %v", err)
					}
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCollectionPermissionMatchesState("metabase_collection_permission.a"),
					resource.TestCheckResourceAttr("metabase_collection_permission.a", "permission", "read"),
				),
			},
		},
	})
}

// The permissions of personal collections are fixed, and Metabase ignores them when updating the collection graph.
func TestAccCollectionPermissionResourcePersonalCollection(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("Acceptance tests skipped unless env '%s' set", resource.EnvTfAcc)
	}

	personalCollectionId := testAccGetPersonalCollectionId(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      providerApiKeyConfig + testAccCollectionPermission("personal", "1", fmt.Sprintf("%q", personalCollectionId), "read"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Personal collection"),
			},
		},
	})
}

// Deletes a group which is not managed by Terraform. Metabase omits the pairs of a deleted group from the collection
// graph, like the pairs set to `none`. The resource is considered deleted rather than read as `none`, so the next apply
// tries to create it again, and fails.
func TestAccCollectionPermissionResourceDeletedGroup(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("Acceptance tests skipped unless env '%s' set", resource.EnvTfAcc)
	}

	groupId := testAccCreateBystanderGroup(t, "🗑️ Collection permission deleted group")
	config := providerApiKeyConfig + testAccCollectionPermission("deleted", strconv.Itoa(groupId), `"root"`, "none")

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

// Creates a collection directly using the Metabase API, and returns its ID. The collection is archived at the end of
// the test.
func testAccCreateCollection(t *testing.T, name string) string {
	t.Helper()

	response, err := testAccMetabaseClient.CreateCollectionWithResponse(context.Background(), metabase.CreateCollectionBody{
		Name: name,
	})
	if err != nil || response.StatusCode() != 200 {
		t.Fatalf("Failed to create collection: %v, %v", err, response)
	}

	id, err := response.JSON200.Id.MarshalJSON()
	if err != nil {
		t.Fatalf("Failed to read the collection ID: %v", err)
	}
	collectionId := strings.Trim(string(id), `"`)

	t.Cleanup(func() {
		archived := true
		_, _ = testAccMetabaseClient.UpdateCollectionWithResponse(context.Background(), collectionId, metabase.UpdateCollectionBody{
			Archived: &archived,
		})
	})

	return collectionId
}

// Creates many collection permissions at once. Terraform applies them in parallel, which the provider should serialize.
func TestAccCollectionPermissionResourceParallel(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("Acceptance tests skipped unless env '%s' set", resource.EnvTfAcc)
	}

	const count = 8

	// The collections are created one at a time beforehand, such that only the permissions are applied in parallel.
	config := providerApiKeyConfig
	checks := []resource.TestCheckFunc{}
	for i := range count {
		collectionId := testAccCreateCollection(t, fmt.Sprintf("🏎️ Parallel %d", i))
		config += fmt.Sprintf(`
resource "metabase_permissions_group" "parallel_%[1]d" {
  name = "🏎️ Parallel %[1]d"
}

resource "metabase_collection_permission" "parallel_%[1]d" {
  group      = metabase_permissions_group.parallel_%[1]d.id
  collection = "%[2]s"
  permission = "read"
}
`, i, collectionId)
		checks = append(checks, testAccCheckCollectionPermissionMatchesState(fmt.Sprintf("metabase_collection_permission.parallel_%d", i)))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionPermissionDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.ComposeAggregateTestCheckFunc(checks...),
			},
		},
	})
}
