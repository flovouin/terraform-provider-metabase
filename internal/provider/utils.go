package provider

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Converts a possibly `nil` string to a Terraform `String` type.
func stringValueOrNull[T ~string](v *T) types.String {
	if v == nil {
		return types.StringNull()
	}

	return types.StringValue(string(*v))
}

// Converts a possibly `nil` integer to a Terraform `Int64` type.
func int64ValueOrNull(v *int) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}

	return types.Int64Value(int64(*v))
}

// Returns the value of a Terraform `String` type, or `nil` if it is null.
func valueStringOrNull(v types.String) *string {
	if v.IsNull() {
		return nil
	}

	r := v.ValueString()
	return &r
}

// Returns the value of a Terraform `String` type, or `nil` if it is null.
func valueApproximateStringOrNull[T ~string](v types.String) *T {
	if v.IsNull() {
		return nil
	}

	r := T(v.ValueString())
	return &r
}

// Returns the value of a Terraform `Int64` type, or `nil` if it is null.
func valueInt64OrNull(v types.Int64) *int {
	if v.IsNull() {
		return nil
	}

	r := int(v.ValueInt64())
	return &r
}

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

	if r.HasExpectedStatusWithoutExpectedBody() {
		return diag.Diagnostics{
			diag.NewErrorDiagnostic(
				fmt.Sprintf("Unexpected response while calling the Metabase API for operation '%s'.", operation),
				fmt.Sprintf("Status code: %d, failed to parse body: %s", r.StatusCode(), r.BodyString()),
			),
		}
	}

	if slices.Contains(statusCodes, r.StatusCode()) {
		return diag.Diagnostics{}
	}

	return diag.Diagnostics{
		diag.NewErrorDiagnostic(
			fmt.Sprintf("Unexpected response while calling the Metabase API for operation '%s'.", operation),
			fmt.Sprintf("Status code: %d, body: %s", r.StatusCode(), string(r.BodyString())),
		),
	}
}

// Performs the import operation for a resource identified using its `id` integer attribute.
func importStatePassthroughIntegerId(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Unable to convert ID to an integer.", req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// Returns a map where keys are the IDs of the permissions groups that should be ignored when synchronizing the
// permissions graph. If the set of ignored groups in the Terraform resource is null, it will default to the
// administrators group only (the group is automatically granted access to all collections and datasets, and this cannot
// be changed).
func getIgnoredPermissionsGroups(ctx context.Context, list types.Set) (map[string]bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if list.IsNull() {
		return map[string]bool{
			fmt.Sprint(metabase.AdministratorsPermissionsGroupId): true,
		}, diags
	}

	var groupIds []int64
	diags.Append(list.ElementsAs(ctx, &groupIds, false)...)
	if diags.HasError() {
		return nil, diags
	}

	ignoredGroups := make(map[string]bool, len(groupIds))
	for _, g := range groupIds {
		ignoredGroups[fmt.Sprint(g)] = true
	}

	return ignoredGroups, diags
}
