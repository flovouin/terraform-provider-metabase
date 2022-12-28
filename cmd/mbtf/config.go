package main

import (
	"errors"
	"io/fs"
	"strings"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
)

// The prefix for all environment variables to consider when loading the configuration.
const environmentVariablesPrefix = "MBTF_"

// The default location of the configuration file.
const defaultConfigFilePath = "mbtf.yml"

// The configuration used to call the Metabase API.
type metabaseConfig struct {
	Endpoint string `koanf:"endpoint"` // The URL to the Metabase API.
	Username string `koanf:"username"` // The username (email address) to use to log in.
	Password string `koanf:"password"` // The password to use to log in.
}

// A single mapping from a database to a Terraform resource name.
type databaseMappingConfig struct {
	Id           int    `koanf:"id"`            // The ID of the database in the Metabase API. Can be omitted (0) if the name is provided.
	Name         string `koanf:"name"`          // The name of the database in the Metabase API. Can be omitted ("") if the ID is provided.
	ResourceName string `koanf:"resource_name"` // The name of the (manually defined) Terraform resource.
}

// Defines how databases references are handled and converted in the generated Terraform code.
type databasesConfig struct {
	Mapping []databaseMappingConfig `koanf:"mapping"` // The list of mappings from databases to Terraform resources.
}

// A single mapping from a collection to a Terraform resource name.
type collectionMappingConfig struct {
	Id           string `koanf:"id"`            // The ID of the collection in the Metabase API. Can be omitted (0) if the name is provided.
	Name         string `koanf:"name"`          // The name of the collection in the Metabase API. Can be omitted ("") if the ID is provided.
	ResourceName string `koanf:"resource_name"` // The name of the (manually defined) Terraform resource.
}

// Defines how collections references are handled and converted in the generated Terraform code.
type collectionsConfig struct {
	Mapping []collectionMappingConfig `koanf:"mapping"` // The list of mappings from collections to Terraform resources.
}

// Defines a reference to a collection in Metabase.
type collectionDefinition struct {
	Id   int    `koanf:"id"`   // The ID of the collection in the Metabase API. Can be omitted (0) if the name is provided.
	Name string `koanf:"name"` // The name of the collection in the Metabase API. Can be omitted ("") if the ID is provided.
}

// Defines which dashboards to include in the import.
type dashboardFilterConfig struct {
	IncludedCollections  []collectionDefinition `koanf:"included_collections"`  // The list of collections for which dashboards should be imported. All collections are imported by default.
	ExcludedCollections  []collectionDefinition `koanf:"excluded_collections"`  // The list of collections to exclude from the import.
	DashboardName        string                 `koanf:"dashboard_name"`        // A regexp that the dashboard name should match in order to be imported.
	DashboardDescription string                 `koanf:"dashboard_description"` // A regexp that the dashboard description should match in order to be imported.
	DashboardIds         []int                  `koanf:"dashboard_ids"`         // The list of IDs of the dashboards to import. If this is non-empty, all other parameters are ignored.
}

// Defines how the Terraform configuration is written to files.
type outputConfig struct {
	Path  string `koanf:"path"`  // The path where the Terraform configuration will be written.
	Clear bool   `koanf:"clear"` // Whether generated files with the right prefix should be removed from the output directory before writing.
}

// The entire configuration when importing dashboards from Metabase.
type importerConfig struct {
	Metabase        metabaseConfig        `koanf:"metabase"`         // The configuration used to call the Metabase API.
	Databases       databasesConfig       `koanf:"databases"`        // Defines how databases references are handled and converted in the generated Terraform code.
	Collections     collectionsConfig     `koanf:"collections"`      // Defines how collections references are handled and converted in the generated Terraform code.
	DashboardFilter dashboardFilterConfig `koanf:"dashboard_filter"` // Defines which dashboards to include in the import.
	Output          outputConfig          `koanf:"output"`           // Defines how the Terraform configuration is written to files.
}

// Loads the `importedConfig` from the config file and the environment.
func loadConfig() (*importerConfig, error) {
	var k = koanf.New(".")

	err := k.Load(structs.Provider(importerConfig{
		Output: outputConfig{
			Path: "./",
		},
	}, "koanf"), nil)
	if err != nil {
		return nil, err
	}

	err = k.Load(file.Provider(defaultConfigFilePath), yaml.Parser())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	err = k.Load(env.Provider(environmentVariablesPrefix, ".", func(s string) string {
		return strings.Replace(strings.ToLower(
			strings.TrimPrefix(s, environmentVariablesPrefix)), "_", ".", -1)
	}), nil)
	if err != nil {
		return nil, err
	}

	var conf importerConfig
	err = k.Unmarshal("", &conf)
	if err != nil {
		return nil, err
	}

	return &conf, nil
}
