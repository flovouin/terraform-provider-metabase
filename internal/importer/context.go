package importer

import (
	"github.com/flovouin/terraform-provider-metabase/metabase"
)

// A database available as a reference for other Terraform resources.
// It is not automatically imported, but defined as an input to the importer.
type importedDatabase struct {
	Database metabase.Database // The database, as returned by the Metabase API.
	Slug     string            // A slug attributed to the database, used as the name of the Terraform resource.
}

// A context that can be created to import one or several dashboards from a Metabase API.
type ImportContext struct {
	client          metabase.ClientWithResponses  // The client to use to perform calls to the API.
	databases       map[int]importedDatabase      // The databases available to other Terraform resources.
}

// Creates a new import context that will use the given Metabase client.
func NewImportContext(client metabase.ClientWithResponses) ImportContext {
	return ImportContext{
		client:          client,
		databases:       make(map[int]importedDatabase),
	}
}
