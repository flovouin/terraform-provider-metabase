package importer

import (
	"context"
	"errors"
)

// Fetches a field from the Metabase API and produces the corresponding Terraform definition.
// This will import the parent table if it hasn't already been imported.
func (ic *ImportContext) importField(ctx context.Context, fieldId int) (*importedField, error) {
	field, ok := ic.fields[fieldId]
	if ok {
		return &field, nil
	}

	getResp, err := ic.client.GetFieldWithResponse(ctx, fieldId)
	if err != nil {
		return nil, err
	}
	if getResp.JSON200 == nil {
		return nil, errors.New("received unexpected response when getting field")
	}

	table, err := ic.importTable(ctx, getResp.JSON200.TableId)
	if err != nil {
		return nil, err
	}

	field = importedField{
		Field:       *getResp.JSON200,
		ParentTable: table,
	}

	ic.fields[fieldId] = field

	return &field, nil
}
