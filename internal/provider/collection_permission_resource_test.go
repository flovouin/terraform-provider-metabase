package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"testing"

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
func testAccCheckCollectionPermissionExists(resourceName string) resource.TestCheckFunc {
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

	diags := updateCollectionPermission(context.Background(), testAccMetabaseClient, mustParseInt64(t, groupId), collectionId, permission)
	if diags.HasError() {
		t.Fatalf("Failed to set permission: %v", diags)
	}
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
					testAccCollectionPermission("a", "metabase_permissions_group.permission_a.id", "metabase_collection.permission.id", "write") +
						testAccCollectionPermission("b", "metabase_permissions_group.permission_b.id", "metabase_collection.permission.id", "read") +
						testAccCollectionPermission("a_root", "metabase_permissions_group.permission_a.id", `"root"`, "read"),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCollectionPermissionExists("metabase_collection_permission.a"),
					testAccCheckCollectionPermissionExists("metabase_collection_permission.b"),
					testAccCheckCollectionPermissionExists("metabase_collection_permission.a_root"),
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
				// Removing a resource only resets its own edge. The other edges are left untouched.
				Config: providerApiKeyConfig + testAccCollectionPermissionResource(
					testAccCollectionPermission("a", "metabase_permissions_group.permission_a.id", "metabase_collection.permission.id", "read") +
						testAccCollectionPermission("a_root", "metabase_permissions_group.permission_a.id", `"root"`, "read"),
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCollectionPermissionExists("metabase_collection_permission.a"),
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
				Config:      providerApiKeyConfig + testAccCollectionPermission("none", "1", `"root"`, "none"),
				ExpectError: regexp.MustCompile("Invalid collection permission"),
			},
		},
	})
}
