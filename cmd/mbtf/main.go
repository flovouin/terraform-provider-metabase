package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/zerogachis/terraform-provider-metabase/internal/importer"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Initializes the Metabase API client using the configuration.
func makeMetabaseClient(ctx context.Context, config metabaseConfig) (*metabase.ClientWithResponses, error) {
	if len(config.Endpoint) == 0 {
		return nil, errors.New("the Metabase endpoint should be set and non-empty")
	}

	if len(config.Username) == 0 {
		return nil, errors.New("the Metabase username should be set and non-empty")
	}

	if len(config.Password) == 0 {
		return nil, errors.New("the Metabase password should be set and non-empty")
	}

	client, err := metabase.MakeAuthenticatedClientWithUsernameAndPassword(ctx, config.Endpoint, config.Username, config.Password)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// Imports databases definitions from the configuration into the importer.
func setUpDatabases(ctx context.Context, config databasesConfig, ic importer.ImportContext) error {
	definitions := make([]importer.ExistingDatabaseDefinition, 0, len(config.Mapping))
	for _, d := range config.Mapping {
		var id *int
		var name *string

		if d.Id > 0 {
			id = &d.Id
		} else if len(d.Name) > 0 {
			name = &d.Name
		} else {
			return errors.New("database ID or name should be specified")
		}

		if len(d.ResourceName) == 0 {
			return errors.New("database resource name should be specified")
		}

		definitions = append(definitions, importer.ExistingDatabaseDefinition{
			Id:           id,
			Name:         name,
			ResourceName: d.ResourceName,
		})
	}

	return ic.ImportDatabasesFromDefinitions(ctx, definitions)
}

// Imports collections definitions from the configuration into the importer.
func setUpCollections(ctx context.Context, config collectionsConfig, ic importer.ImportContext) error {
	definitions := make([]importer.ExistingCollectionDefinition, 0, len(config.Mapping))
	for _, d := range config.Mapping {
		var id *string
		var name *string

		if len(d.Id) > 0 {
			id = &d.Id
		} else if len(d.Name) > 0 {
			name = &d.Name
		} else {
			return errors.New("collection ID or name should be specified")
		}

		if len(d.ResourceName) == 0 {
			return errors.New("collection resource name should be specified")
		}

		definitions = append(definitions, importer.ExistingCollectionDefinition{
			Id:           id,
			Name:         name,
			ResourceName: d.ResourceName,
		})
	}

	return ic.ImportCollectionsFromDefinitions(ctx, definitions)
}

// Runs the command line.
func runImport() error {
	config, err := loadConfig()
	if err != nil {
		return err
	}

	ctx := context.Background()

	client, err := makeMetabaseClient(ctx, config.Metabase)
	if err != nil {
		return err
	}

	ic := importer.NewImportContext(*client)

	err = setUpDatabases(ctx, config.Databases, ic)
	if err != nil {
		return err
	}

	err = setUpCollections(ctx, config.Collections, ic)
	if err != nil {
		return err
	}

	dashboardIds, err := listDashboardsToImport(ctx, config.DashboardFilter, *client)
	if err != nil {
		return err
	}

	for _, dashboardId := range dashboardIds {
		_, err = ic.ImportDashboard(ctx, dashboardId)
		if err != nil {
			return err
		}
	}

	err = ic.Write(config.Output.Path, importer.WriteOptions{
		ClearOutput:       config.Output.Clear,
		DisableFormatting: config.Output.DisableFormatting,
	})
	if err != nil {
		return err
	}

	return nil
}

// The main entrypoint.
func main() {
	err := runImport()
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
