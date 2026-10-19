package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// Returns the ID of a collection as a string. The ID can be a string because of the "root" collection, while all
// user-created collections have an integer ID.
func collectionIdToString(id metabase.Collection_Id) (*string, diag.Diagnostics) {
	var diags diag.Diagnostics

	if strId, err := id.AsCollectionId0(); err == nil {
		return &strId, diags
	}

	if intId, err := id.AsCollectionId1(); err == nil {
		strId := fmt.Sprint(intId)
		return &strId, diags
	}

	marshalled, _ := id.MarshalJSON()
	diags.AddError("Unable to parse collection ID.", string(marshalled))
	return nil, diags
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
				fmt.Sprintf("Status code: %d, failed to parse body: %s", r.StatusCode(), summarizeBody(r.BodyString())),
			),
		}
	}

	if slices.Contains(statusCodes, r.StatusCode()) {
		return diag.Diagnostics{}
	}

	return diag.Diagnostics{
		diag.NewErrorDiagnostic(
			fmt.Sprintf("Unexpected response while calling the Metabase API for operation '%s'.", operation),
			fmt.Sprintf("Status code: %d, body: %s", r.StatusCode(), summarizeBody(r.BodyString())),
		),
	}
}

const maxErrorBodyLength = 500

// Reduces a response body to what is useful in a diagnostic.
// Metabase errors can carry a very large debug trace that may include submitted values such as passwords.
func summarizeBody(body string) string {
	var parsed struct {
		Message string `json:"message"`
		Cause   string `json:"cause"`
	}
	if json.Unmarshal([]byte(body), &parsed) == nil {
		if parsed.Cause != "" {
			return parsed.Cause
		}
		if parsed.Message != "" {
			return parsed.Message
		}
	}

	if len(body) > maxErrorBodyLength {
		return strings.ToValidUTF8(body[:maxErrorBodyLength], "") + "...(truncated)"
	}

	return body
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

// The private state of a resource, as provided by the framework (e.g. `req.Private` or `resp.Private`).
type privateState interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// A key of the private state, under which a JSON value of type T is stored.
type privateStateKey[T any] string

// Returns the value stored under the given key of the private state, or `nil` if none is stored.
func readPrivateState[T any](ctx context.Context, private privateState, key privateStateKey[T]) (*T, diag.Diagnostics) {
	b, diags := private.GetKey(ctx, string(key))
	if diags.HasError() || b == nil {
		return nil, diags
	}

	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		diags.AddError(fmt.Sprintf("Unexpected error reading %q from the private state.", key), err.Error())
		return nil, diags
	}

	return &v, diags
}

// Stores the given value under the given key of the private state.
func writePrivateState[T any](ctx context.Context, private privateState, key privateStateKey[T], value T) diag.Diagnostics {
	b, err := json.Marshal(value)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError(fmt.Sprintf("Unexpected error storing %q in the private state.", key), err.Error())
		return diags
	}

	return private.SetKey(ctx, string(key), b)
}
