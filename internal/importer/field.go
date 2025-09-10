package importer

import (
	"context"
	"errors"

	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Searches a JSON object or array recursively to find references to `Field` Metabase objects. The references are
// replaced by an `importedField`, which is marshalled as a reference to the corresponding Terraform table data source
// instead.
func (ic *ImportContext) insertFieldReferencesRecursively(ctx context.Context, obj any) error {
	switch typedObj := obj.(type) {
	case map[string]any:
		for _, v := range typedObj {
			err := ic.insertFieldReferencesRecursively(ctx, v)
			if err != nil {
				return err
			}
		}

		return nil
	case []any:
		// A reference to a field is an array with the form `["field", <fieldId>, ...]`.
		// This first tries to find such a reference in the array. If it does not, the array is then searched recursively.
		inserted, err := ic.tryInsertFieldReference(ctx, typedObj)
		if err != nil {
			return err
		}
		if inserted {
			return nil
		}

		for _, v := range typedObj {
			err := ic.insertFieldReferencesRecursively(ctx, v)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Tries to replace the reference to a field ID by the corresponding `importedField`.
// If the given array is not a reference to a field, this function returns `false`. If the array is a reference to a
// field, but the field cannot be imported, the function will return an error.
func (ic *ImportContext) tryInsertFieldReference(ctx context.Context, array []any) (bool, error) {
	if len(array) < 2 {
		return false, nil
	}

	fieldLiteral, ok := array[0].(string)
	if !ok || fieldLiteral != metabase.FieldLiteral {
		return false, nil
	}

	fieldIdFloat, ok := array[1].(float64)
	if !ok {
		return false, nil
	}

	importedField, err := ic.importField(ctx, int(fieldIdFloat))
	if err != nil {
		return false, err
	}

	array[1] = importedField

	return true, nil
}

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
