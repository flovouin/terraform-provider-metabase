package importer

import (
	"github.com/flovouin/terraform-provider-metabase/metabase"
)


// A table imported from the Metabase API and converted to HCL (as a data source).
type importedTable struct {
	Table metabase.TableMetadata // The table, as returned by the Metabase API.
	Slug  string                 // A slug attributed to the table, used as the name of the Terraform data source.
	Hcl   string                 // The HCL definition for the table.
}

// A database available as a reference for other Terraform resources.
// It is not automatically imported, but defined as an input to the importer.
type importedDatabase struct {
	Database metabase.Database // The database, as returned by the Metabase API.
	Slug     string            // A slug attributed to the database, used as the name of the Terraform resource.
}

// A collection available as a reference for other Terraform resources.
// It is not automatically imported, but defined as an input to the importer.
type importedCollection struct {
	Collection metabase.Collection // The collection, as returned by the Metabase API.
	Slug       string              // A slug attributed to the collection, used as the name of the Terraform resource.
}

// A context that can be created to import one or several dashboards from a Metabase API.
type ImportContext struct {
	client          metabase.ClientWithResponses  // The client to use to perform calls to the API.
	tables          map[int]importedTable         // The tables imported from the API.
	databases       map[int]importedDatabase      // The databases available to other Terraform resources.
	collections     map[string]importedCollection // The collections available to other Terraform resources.
	tablesSlugs     map[string]bool               // The slugs that have been assigned to tables, for which uniqueness should be guaranteed.
}

// Creates a new import context that will use the given Metabase client.
func NewImportContext(client metabase.ClientWithResponses) ImportContext {
	return ImportContext{
		client:          client,
		tables:          make(map[int]importedTable),
		databases:       make(map[int]importedDatabase),
		collections:     make(map[string]importedCollection),
		tablesSlugs:     make(map[string]bool),
	}
}
