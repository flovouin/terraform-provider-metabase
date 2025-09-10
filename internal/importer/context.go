package importer

import (
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// A card imported from the Metabase API and converted to HCL.
type importedCard struct {
	Card metabase.Card // The card, as returned by the Metabase API.
	Slug string        // A slug attributed to the card, used as the name of the Terraform resource.
	Hcl  string        // The HCL definition for the card.
}

// A table imported from the Metabase API and converted to HCL (as a data source).
type importedTable struct {
	Table metabase.TableMetadata // The table, as returned by the Metabase API.
	Slug  string                 // A slug attributed to the table, used as the name of the Terraform data source.
	Hcl   string                 // The HCL definition for the table.
}

// A dashboard imported from the Metabase API and converted to HCL.
type importedDashboard struct {
	Dashboard metabase.Dashboard // The dashboard, as returned by the Metabase API.
	Slug      string             // A slug attributed to the dashboard, used as the name of the Terraform resource.
	Hcl       string             // The HCL definition for the dashboard.
}

// A field imported from the Metabase API.
// A field is exposed in Terraform through the parent table data source.
type importedField struct {
	Field       metabase.Field // The field, as returned by the Metabase API.
	ParentTable *importedTable // The table containing the field.
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
	cards           map[int]importedCard          // The cards imported from the API.
	tables          map[int]importedTable         // The tables imported from the API.
	fields          map[int]importedField         // The fields imported from the API.
	dashboards      map[int]importedDashboard     // The dashboards imported from the API.
	databases       map[int]importedDatabase      // The databases available to other Terraform resources.
	collections     map[string]importedCollection // The collections available to other Terraform resources.
	cardsSlugs      map[string]bool               // The slugs that have been assigned to cards, for which uniqueness should be guaranteed.
	tablesSlugs     map[string]bool               // The slugs that have been assigned to tables, for which uniqueness should be guaranteed.
	dashboardsSlugs map[string]bool               // The slugs that have been assigned to dashboards, for which uniqueness should be guaranteed.
}

// Creates a new import context that will use the given Metabase client.
func NewImportContext(client metabase.ClientWithResponses) ImportContext {
	return ImportContext{
		client:          client,
		cards:           make(map[int]importedCard),
		tables:          make(map[int]importedTable),
		fields:          make(map[int]importedField),
		dashboards:      make(map[int]importedDashboard),
		databases:       make(map[int]importedDatabase),
		collections:     make(map[string]importedCollection),
		cardsSlugs:      make(map[string]bool),
		tablesSlugs:     make(map[string]bool),
		dashboardsSlugs: make(map[string]bool),
	}
}
