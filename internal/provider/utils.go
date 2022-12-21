package provider

import (
	"fmt"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Ensures that a Metabase response is not an error and has the expected status code. Otherwise, returns a diagnostic
// error.
func checkMetabaseResponse(r metabase.MetabaseResponse, err error, statusCodes []int, operation string) diag.Diagnostics {
	if err != nil {
		return diag.Diagnostics{
			diag.NewErrorDiagnostic(
				fmt.Sprintf("Unexpected error while calling the Metabase API for operation '%s'.", operation),
				err.Error(),
			),
		}
	}

	for _, s := range statusCodes {
		if r.StatusCode() == s {
			return diag.Diagnostics{}
		}
	}

	return diag.Diagnostics{
		diag.NewErrorDiagnostic(
			fmt.Sprintf("Unexpected response while calling the Metabase API for operation '%s'.", operation),
			fmt.Sprintf("Status code: %d, body: %s", r.StatusCode(), string(r.BodyString())),
		),
	}
}
