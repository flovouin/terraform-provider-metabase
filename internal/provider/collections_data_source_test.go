package provider

import (
	"context"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccCollectionsDataSource() string {
	return `
resource "metabase_collection" "parent" {
  name = "DataSourceParent"
}

resource "metabase_collection" "child" {
  name      = "DataSourceChild"
  parent_id = metabase_collection.parent.id
}

data "metabase_collections" "test" {
  depends_on = [metabase_collection.child]
}
`
}

func testAccCollectionsDataSourceArchived() string {
	return `
data "metabase_collections" "test" {
  archived = true
}
`
}

func TestAccCollectionsDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerApiKeyConfig + testAccCollectionsDataSource(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.metabase_collections.test", "collections.#"),
					resource.TestCheckResourceAttrPair("data.metabase_collections.test", "ids_by_path.DataSourceParent", "metabase_collection.parent", "id"),
					resource.TestCheckResourceAttrPair("data.metabase_collections.test", "ids_by_path.DataSourceParent/DataSourceChild", "metabase_collection.child", "id"),
				),
			},
			{
				Config: providerApiKeyConfig + testAccCollectionsDataSourceArchived(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.metabase_collections.test", "collections.#"),
				),
			},
		},
	})
}

// Makes a collection with an integer ID, as returned by the Metabase API.
func makeTestCollection(t *testing.T, id int, name string, location string) metabase.Collection {
	var collectionId metabase.Collection_Id
	if err := collectionId.FromCollectionId1(id); err != nil {
		t.Fatalf("Failed to create the ID for collection %s: %s", name, err)
	}

	return metabase.Collection{
		Id:       collectionId,
		Name:     name,
		Location: &location,
	}
}

func TestCollectionsDataSourceModelPaths(t *testing.T) {
	var rootId metabase.Collection_Id
	if err := rootId.FromCollectionId0("root"); err != nil {
		t.Fatalf("Failed to create the ID for the root collection: %s", err)
	}

	collections := []metabase.Collection{
		makeTestCollection(t, 3, "Campaigns", "/1/"),
		makeTestCollection(t, 1, "Marketing", "/"),
		// The parent of this collection is not part of the response, and is expected to fall back to its ID.
		makeTestCollection(t, 4, "Orphan", "/12/"),
		makeTestCollection(t, 2, "Finance", "/"),
		makeTestCollection(t, 5, "Reports", "/1/3/"),
		{Id: rootId, Name: "Our analytics"},
	}

	var data CollectionsDataSourceModel
	if diags := updateModelFromCollections(context.Background(), collections, &data); diags.HasError() {
		t.Fatalf("Failed to build the model from the collections: %s", diags.Errors())
	}

	var models []CollectionsDataSourceCollectionModel
	if diags := data.Collections.ElementsAs(context.Background(), &models, false); diags.HasError() {
		t.Fatalf("Failed to read back the list of collections: %s", diags.Errors())
	}

	expectedPaths := []string{
		"12/Orphan",
		"Finance",
		"Marketing",
		"Marketing/Campaigns",
		"Marketing/Campaigns/Reports",
		"Our analytics",
	}

	if len(models) != len(expectedPaths) {
		t.Fatalf("Expected %d collections, got %d.", len(expectedPaths), len(models))
	}

	for i, expectedPath := range expectedPaths {
		if models[i].Path.ValueString() != expectedPath {
			t.Errorf("Expected collection %d to have path %s, got %s.", i, expectedPath, models[i].Path.ValueString())
		}
	}

	parentIds := map[string]interface{}{
		"Marketing":                   nil,
		"Marketing/Campaigns":         int64(1),
		"Marketing/Campaigns/Reports": int64(3),
		"12/Orphan":                   int64(12),
		"Our analytics":               nil,
	}

	for _, m := range models {
		expectedParentId, ok := parentIds[m.Path.ValueString()]
		if !ok {
			continue
		}

		if expectedParentId == nil {
			if !m.ParentId.IsNull() {
				t.Errorf("Expected collection %s to have no parent, got %d.", m.Path.ValueString(), m.ParentId.ValueInt64())
			}
			continue
		}

		if m.ParentId.ValueInt64() != expectedParentId.(int64) {
			t.Errorf("Expected collection %s to have parent %d, got %d.", m.Path.ValueString(), expectedParentId, m.ParentId.ValueInt64())
		}
	}

	idsByPath := data.IdsByPath.Elements()
	if len(idsByPath) != len(expectedPaths) {
		t.Errorf("Expected %d paths in the map of IDs, got %d.", len(expectedPaths), len(idsByPath))
	}
}

func TestCollectionsDataSourceModelDuplicatePaths(t *testing.T) {
	var rootId metabase.Collection_Id
	if err := rootId.FromCollectionId0("root"); err != nil {
		t.Fatalf("Failed to create the ID for the root collection: %s", err)
	}

	collections := []metabase.Collection{
		// A top-level collection sharing its name with the root collection.
		makeTestCollection(t, 100, "Our analytics", "/"),
		// Comparing IDs as strings would put the most recent collection first.
		makeTestCollection(t, 10, "Marketing", "/"),
		makeTestCollection(t, 9, "Marketing", "/"),
		{Id: rootId, Name: "Our analytics"},
	}

	var data CollectionsDataSourceModel
	diags := updateModelFromCollections(context.Background(), collections, &data)
	if diags.HasError() {
		t.Fatalf("Failed to build the model from the collections: %s", diags.Errors())
	}

	if diags.WarningsCount() != 2 {
		t.Errorf("Expected 2 warnings about duplicate paths, got %d.", diags.WarningsCount())
	}

	var models []CollectionsDataSourceCollectionModel
	if diags := data.Collections.ElementsAs(context.Background(), &models, false); diags.HasError() {
		t.Fatalf("Failed to read back the list of collections: %s", diags.Errors())
	}

	expectedIds := []string{"9", "10", "root", "100"}

	if len(models) != len(expectedIds) {
		t.Fatalf("Expected %d collections, got %d.", len(expectedIds), len(models))
	}

	for i, expectedId := range expectedIds {
		if models[i].Id.ValueString() != expectedId {
			t.Errorf("Expected collection %d to have ID %s, got %s.", i, expectedId, models[i].Id.ValueString())
		}
	}

	var idsByPath map[string]string
	if diags := data.IdsByPath.ElementsAs(context.Background(), &idsByPath, false); diags.HasError() {
		t.Fatalf("Failed to read back the map of IDs: %s", diags.Errors())
	}

	expectedIdsByPath := map[string]string{
		"Marketing":     "9",
		"Our analytics": "root",
	}

	for path, expectedId := range expectedIdsByPath {
		if idsByPath[path] != expectedId {
			t.Errorf("Expected path %s to reference collection %s, got %s.", path, expectedId, idsByPath[path])
		}
	}
}
