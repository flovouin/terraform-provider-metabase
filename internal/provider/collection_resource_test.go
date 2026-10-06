package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccCollectionResource(name string, collectionName string, description string, parentId string) string {
	return fmt.Sprintf(`
resource "metabase_collection" "%s" {
  name        = "%s"
	description = "%s"
	parent_id   = %s
}
`,
		name,
		collectionName,
		description,
		parentId,
	)
}

func testAccCheckCollectionExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		response, err := testAccMetabaseClient.GetCollectionWithResponse(context.Background(), rs.Primary.ID)
		if err != nil {
			return err
		}
		if response.StatusCode() != 200 {
			return fmt.Errorf("Received unexpected response from the Metabase API when getting collection.")
		}

		if rs.Primary.Attributes["name"] != response.JSON200.Name {
			return fmt.Errorf("Terraform resource and API response do not match for collection name.")
		}

		if rs.Primary.Attributes["description"] != *response.JSON200.Description {
			return fmt.Errorf("Terraform resource and API response do not match for collection description.")
		}

		return nil
	}
}

func testAccCheckCollectionDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "metabase_collection" {
			continue
		}

		response, err := testAccMetabaseClient.GetCollectionWithResponse(context.Background(), rs.Primary.ID)
		if err != nil {
			return err
		}
		if response.StatusCode() == 404 {
			return nil
		}
		if response.StatusCode() == 200 && *response.JSON200.Archived {
			return nil
		}

		return fmt.Errorf("Collection %s still exists.", rs.Primary.ID)
	}

	return nil
}

func TestAccCollectionResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccCollectionResource("test", "📚 Collection", "💡 Description", "null"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCollectionExists("metabase_collection.test"),
					resource.TestCheckResourceAttrSet("metabase_collection.test", "id"),
					resource.TestCheckResourceAttr("metabase_collection.test", "name", "📚 Collection"),
					resource.TestCheckResourceAttr("metabase_collection.test", "description", "💡 Description"),
				),
			},
			{
				ResourceName: "metabase_collection.test",
				ImportState:  true,
			},
			{
				Config: providerConfig + testAccCollectionResource("test", "🎁 Updated", "❓ Other", "null"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_collection.test", "id"),
					resource.TestCheckResourceAttr("metabase_collection.test", "name", "🎁 Updated"),
					resource.TestCheckResourceAttr("metabase_collection.test", "description", "❓ Other"),
				),
			},
			{
				Config: providerConfig +
					testAccCollectionResource("test", "🎁 Updated", "❓ Other", "null") +
					testAccCollectionResource("child", "🧒 Child", "🌴 Nested", "metabase_collection.test.id"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_collection.child", "id"),
					resource.TestCheckResourceAttr("metabase_collection.child", "name", "🧒 Child"),
					resource.TestCheckResourceAttr("metabase_collection.child", "description", "🌴 Nested"),
					resource.TestCheckResourceAttrSet("metabase_collection.child", "parent_id"),
				),
			},
			{
				// Removing the description and the parent collection should be applied, rather than leaving them unchanged.
				Config: providerConfig +
					testAccCollectionResource("test", "🎁 Updated", "❓ Other", "null") + `
resource "metabase_collection" "child" {
  name = "🧒 Child"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("metabase_collection.child", "description"),
					resource.TestCheckNoResourceAttr("metabase_collection.child", "parent_id"),
					resource.TestCheckResourceAttr("metabase_collection.child", "location", "/"),
					testAccCheckCollectionIsAtRootWithoutDescription("metabase_collection.child"),
				),
			},
		},
	})
}

// Checks that the collection has no description and belongs to the root collection in Metabase.
func testAccCheckCollectionIsAtRootWithoutDescription(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		response, err := testAccMetabaseClient.GetCollectionWithResponse(context.Background(), rs.Primary.ID)
		if err != nil {
			return err
		}
		if response.StatusCode() != 200 {
			return fmt.Errorf("Received unexpected response from the Metabase API when getting collection.")
		}

		if response.JSON200.Description != nil {
			return fmt.Errorf("Expected the collection to have no description, got %q.", *response.JSON200.Description)
		}
		if response.JSON200.Location == nil || *response.JSON200.Location != "/" {
			return fmt.Errorf("Expected the collection to belong to the root collection, got location %v.", response.JSON200.Location)
		}

		return nil
	}
}

// Creates many collections at once. Terraform applies them in parallel, and Metabase fails with a duplicate key error
// when it records concurrent revisions of the collection graph, unless the provider serializes the requests.
func TestAccCollectionResourceParallel(t *testing.T) {
	const count = 12

	config := providerConfig
	checks := []resource.TestCheckFunc{}
	for i := range count {
		name := fmt.Sprintf("parallel_%d", i)
		config += testAccCollectionResource(name, fmt.Sprintf("🏎️ Parallel %d", i), "🏁 Created in parallel", "null")
		checks = append(checks, testAccCheckCollectionExists("metabase_collection."+name))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.ComposeAggregateTestCheckFunc(checks...),
			},
		},
	})
}

// Creates an API key for a group which is not the Administrators group, with write permission on the root collection,
// and returns the corresponding provider configuration. The group and the key are deleted at the end of the test.
func testAccNonAdminProviderConfig(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	group, err := testAccMetabaseClient.CreatePermissionsGroupWithResponse(ctx, metabase.CreatePermissionsGroupBody{Name: "🧑‍🎨 Curators"})
	if err != nil || group.StatusCode() != 200 {
		t.Fatalf("Failed to create the permissions group: %v", err)
	}
	t.Cleanup(func() { _, _ = testAccMetabaseClient.DeletePermissionsGroupWithResponse(ctx, group.JSON200.Id) })

	graph, err := testAccMetabaseClient.GetCollectionPermissionsGraphWithResponse(ctx)
	if err != nil || graph.StatusCode() != 200 {
		t.Fatalf("Failed to get the collection graph: %v", err)
	}
	updated, err := testAccMetabaseClient.ReplaceCollectionPermissionsGraphWithResponse(ctx, metabase.CollectionPermissionsGraph{
		Revision: graph.JSON200.Revision,
		Groups: map[string]metabase.CollectionPermissionsGraphCollectionPermissionsMap{
			strconv.Itoa(group.JSON200.Id): {"root": metabase.CollectionPermissionLevelWrite},
		},
	})
	if err != nil || updated.StatusCode() != 200 {
		t.Fatalf("Failed to grant write permission on the root collection: %v", err)
	}

	// The API key endpoints are not part of the generated client.
	body, _ := json.Marshal(map[string]any{"group_id": group.JSON200.Id, "name": fmt.Sprintf("Curators %d", group.JSON200.Id)})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, os.Getenv("METABASE_URL")+"/api-key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", os.Getenv("METABASE_API_KEY"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to create the API key: %v", err)
	}
	defer resp.Body.Close()
	var apiKey struct {
		Id          int    `json:"id"`
		UnmaskedKey string `json:"unmasked_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiKey); err != nil || resp.StatusCode != 200 {
		t.Fatalf("Failed to create the API key (status code %d): %v", resp.StatusCode, err)
	}
	t.Cleanup(func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/api-key/%d", os.Getenv("METABASE_URL"), apiKey.Id), nil)
		req.Header.Set("X-API-KEY", os.Getenv("METABASE_API_KEY"))
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	})

	return fmt.Sprintf(`
provider "metabase" {
  endpoint = "%s"
  api_key  = "%s"
}
`,
		os.Getenv("METABASE_URL"),
		apiKey.UnmaskedKey,
	)
}

// Creates and renames a collection without admin permissions. Reading the collection graph revision requires admin
// permissions, so the requests are performed without tracking it.
func TestAccCollectionResourceWithoutAdminPermissions(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("Acceptance tests skipped unless env '%s' set", resource.EnvTfAcc)
	}

	config := testAccNonAdminProviderConfig(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionDestroy,
		Steps: []resource.TestStep{
			{
				Config: config + testAccCollectionResource("curated", "🎨 Curated", "📝 Created without admin permissions", "null"),
				Check:  testAccCheckCollectionExists("metabase_collection.curated"),
			},
			{
				Config: config + testAccCollectionResource("curated", "🎨 Renamed", "📝 Created without admin permissions", "null"),
				Check:  resource.TestCheckResourceAttr("metabase_collection.curated", "name", "🎨 Renamed"),
			},
		},
	})
}
