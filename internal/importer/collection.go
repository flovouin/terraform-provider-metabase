package importer

import (
	"context"
	"errors"
	"fmt"

	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// A collection that has already been defined in Terraform manually, and that can be referenced by resources that are
// automatically generated.
type ExistingCollectionDefinition struct {
	Id           *string // The ID of the collection. Can be `nil` if the name is provided.
	Name         *string // The name of the collection. Can be `nil` if the ID is provided.
	ResourceName string  // The name of the manually defined Terraform resource.
}

// Retrieves an imported collection given its ID.
func (ic *ImportContext) getCollection(collectionId string) (*importedCollection, error) {
	col, ok := ic.collections[collectionId]
	if !ok {
		return nil, fmt.Errorf("collection %s has not been defined in the importer configuration", collectionId)
	}

	return &col, nil
}

// Imports existing collections already defined manually in Terraform, such that they can be referenced by automatically
// generated Metabase resource.
// A collection imported using its ID will be an exact match. A collection can also be looked up using its name.
func (ic *ImportContext) ImportCollectionsFromDefinitions(ctx context.Context, existingCollections []ExistingCollectionDefinition) error {
	var collectionList *[]metabase.Collection

	for _, existingCollection := range existingCollections {
		var collection *metabase.Collection

		if existingCollection.Id != nil {
			getResp, err := ic.client.GetCollectionWithResponse(ctx, *existingCollection.Id)
			if err != nil {
				return err
			}
			if getResp.JSON200 == nil {
				return errors.New("received unexpected response from the Metabase API when getting collection")
			}

			collection = getResp.JSON200
		}

		if collection == nil {
			if existingCollection.Name == nil {
				return errors.New("one of ID or name should be specified when importing a collection")
			}

			if collectionList == nil {
				listResp, err := ic.client.ListCollectionsWithResponse(ctx, &metabase.ListCollectionsParams{})
				if err != nil {
					return err
				}
				if listResp == nil {
					return errors.New("received unexpected response from the Metabase API when listing databases")
				}

				collectionList = listResp.JSON200
			}

			for _, col := range *collectionList {
				if col.Name == *existingCollection.Name {
					collection = &col
					break
				}
			}

			if collection == nil {
				return fmt.Errorf("unable to find collection with name %s from the Metabase API response", *existingCollection.Name)
			}
		}

		collectionId, err := collection.Id.AsCollectionId0()
		if err != nil {
			idInt, err := collection.Id.AsCollectionId1()
			if err != nil {
				return err
			}

			collectionId = fmt.Sprint(idInt)
		}

		_, exists := ic.collections[collectionId]
		if exists {
			return fmt.Errorf("collection %s has already been imported", collectionId)
		}

		ic.collections[collectionId] = importedCollection{
			Collection: *collection,
			Slug:       existingCollection.ResourceName,
		}
	}

	return nil
}
