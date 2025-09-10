package importer

import (
	"context"
	"errors"
	"fmt"

	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// A database that has already been defined in Terraform manually, and that can be referenced by resources that are
// automatically generated.
type ExistingDatabaseDefinition struct {
	Id           *int    // The ID of the database. Can be `nil` if the name is provided.
	Name         *string // The name of the database. Can be `nil` if the ID is provided.
	ResourceName string  // The name of the manually defined Terraform resource.
}

// Retrieves an imported database given its ID.
func (ic *ImportContext) getDatabase(databaseId int) (*importedDatabase, error) {
	db, ok := ic.databases[databaseId]
	if !ok {
		return nil, fmt.Errorf("database %d has not been defined in the importer configuration", databaseId)
	}

	return &db, nil
}

// Imports existing databases already defined manually in Terraform, such that they can be referenced by automatically
// generated Metabase resource.
// A database imported using its ID will be an exact match. A database can also be looked up using its name.
func (ic *ImportContext) ImportDatabasesFromDefinitions(ctx context.Context, existingDatabases []ExistingDatabaseDefinition) error {
	var databasesList *metabase.DatabaseList

	for _, existingDatabase := range existingDatabases {
		var database *metabase.Database

		if existingDatabase.Id != nil {
			getResp, err := ic.client.GetDatabaseWithResponse(ctx, *existingDatabase.Id)
			if err != nil {
				return err
			}
			if getResp.JSON200 == nil {
				return errors.New("received unexpected response from the Metabase API when getting database")
			}

			database = getResp.JSON200
		}

		if database == nil {
			if existingDatabase.Name == nil {
				return errors.New("one of ID or name should be specified when importing a database")
			}

			if databasesList == nil {
				listResp, err := ic.client.ListDatabasesWithResponse(ctx, &metabase.ListDatabasesParams{})
				if err != nil {
					return err
				}
				if listResp == nil {
					return errors.New("received unexpected response from the Metabase API when listing databases")
				}

				databasesList = listResp.JSON200
			}

			for _, db := range databasesList.Data {
				if db.Name == *existingDatabase.Name {
					database = &db
					break
				}
			}

			if database == nil {
				return fmt.Errorf("unable to find database with name %s from the Metabase API response", *existingDatabase.Name)
			}
		}

		_, exists := ic.databases[database.Id]
		if exists {
			return fmt.Errorf("database %d has already been imported", database.Id)
		}

		ic.databases[database.Id] = importedDatabase{
			Database: *database,
			Slug:     existingDatabase.ResourceName,
		}
	}

	return nil
}
