package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"text/template"

	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// The template producing a `metabase_table` Terraform data source definition.
const tableTemplate = `resource "metabase_table" "{{.TerraformSlug}}" {
  {{if .DbRef}}db_id = metabase_database.{{.DbRef}}.id{{end}}
  {{if .Schema}}schema = {{.Schema}}{{end}}
  name = {{.Name}}

  forced_field_types = {{.ForcedFieldTypes}}
}
`

// The data required to produce a `metabase_table` Terraform data source definition.
type tableTemplateData struct {
	TerraformSlug    string  // The slug used as the name of the Terraform resource.
	Name             string  // The name of the table.
	Schema           *string // The schema the table is part of. If `nil`, this is not added as an attribute.
	DbRef            *string // A (Terraform) reference to the database the table is part of. If `nil`, this is not added as an attribute.
	ForcedFieldTypes string  // A map of semantic types for fields in the table.
}

// Produces the Terraform definition for a `metabase_table` data source.
func (ic *ImportContext) makeTableHcl(table metabase.TableMetadata, slug string) (*string, error) {
	tpl, err := template.New("table").Parse(tableTemplate)
	if err != nil {
		return nil, err
	}

	// Ensures special characters in the table name are escaped.
	name, err := json.Marshal(table.Name)
	if err != nil {
		return nil, err
	}

	var schema *string
	if table.Schema != nil {
		schemaBytes, err := json.Marshal(*table.Schema)
		if err != nil {
			return nil, err
		}

		schemaStr := string(schemaBytes)
		schema = &schemaStr
	}

	// If the database cannot be found in the list of imported databases, the `db_id` condition is simply not added to the
	// data source definition. It is not treated as an error because the field is optional to find the table.
	var dbRef *string
	db, err := ic.getDatabase(table.DbId)
	if err == nil {
		dbRef = &db.Slug
	}

	forcedFieldTypes := make(map[string]*string, len(table.Fields))
	for _, f := range table.Fields {
		forcedFieldTypes[f.Name] = f.SemanticType
	}
	forcedFieldTypesJson, err := json.MarshalIndent(forcedFieldTypes, "  ", "  ")
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	err = tpl.Execute(buf, tableTemplateData{
		TerraformSlug:    slug,
		DbRef:            dbRef,
		Schema:           schema,
		Name:             string(name),
		ForcedFieldTypes: string(forcedFieldTypesJson),
	})
	if err != nil {
		return nil, err
	}

	hcl := buf.String()

	return &hcl, nil
}

// Fetches a table from the Metabase API and produces the corresponding Terraform definition.
func (ic *ImportContext) importTable(ctx context.Context, tableId int) (*importedTable, error) {
	table, ok := ic.tables[tableId]
	if ok {
		return &table, nil
	}

	getResp, err := ic.client.GetTableMetadataWithResponse(ctx, tableId, &metabase.GetTableMetadataParams{})
	if err != nil {
		return nil, err
	}
	if getResp.JSON200 == nil {
		return nil, errors.New("received unexpected response when getting table")
	}

	rawTable := *getResp.JSON200
	tableName := rawTable.Name
	if rawTable.Schema != nil && len(*rawTable.Schema) > 0 {
		// Prefixing the data source name with the table's schema if it is non-empty.
		// This makes names slightly more readable than suffixing with a number in case a table name conflicts between two
		// databases.
		tableName = fmt.Sprintf("%s_%s", *rawTable.Schema, tableName)
	}
	slug := makeUniqueSlug(tableName, ic.tablesSlugs)

	hcl, err := ic.makeTableHcl(rawTable, slug)
	if err != nil {
		return nil, err
	}

	table = importedTable{
		Table: rawTable,
		Slug:  slug,
		Hcl:   *hcl,
	}

	ic.tables[tableId] = table

	return &table, nil
}
