package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

// Returns the ID of an edge of a graph, from the IDs of its group and of its object (a database or a collection). An edge
// is identified by both of its ends, like in the graphs returned by the Metabase API.
func makeGraphEdgeId(groupId int64, objectId string) string {
	return fmt.Sprintf("%d/%s", groupId, objectId)
}

// Parses the ID of an edge of a graph (see `makeGraphEdgeId`), of the form `<group>/<object>`.
func parseGraphEdgeId(id string, objectName string) (int64, string, diag.Diagnostics) {
	var diags diag.Diagnostics

	groupId, objectId, found := strings.Cut(id, "/")
	if !found || len(groupId) == 0 || len(objectId) == 0 {
		diags.AddError("Invalid import ID.", fmt.Sprintf("Expected an ID of the form <group>/<%s>, got: %s.", objectName, id))
		return 0, "", diags
	}

	groupIdInt, err := strconv.ParseInt(groupId, 10, 64)
	if err != nil {
		diags.AddError("Unable to convert the group ID to an integer.", groupId)
		return 0, "", diags
	}

	return groupIdInt, objectId, diags
}

// Returns an error if the given group is the Administrators group, for which Metabase does not allow changing
// permissions.
func checkGroupIsNotAdministrators(groupId int64) diag.Diagnostics {
	var diags diag.Diagnostics

	if groupId == metabase.AdministratorsPermissionsGroupId {
		diags.AddAttributeError(
			path.Root("group"),
			"Permissions for the Administrators group cannot be managed.",
			fmt.Sprintf("The Administrators group (ID %d) is always granted full access by Metabase, and its permissions cannot be changed.", metabase.AdministratorsPermissionsGroupId),
		)
	}

	return diags
}

// Returns whether the given permissions group exists.
func permissionsGroupExists(ctx context.Context, client *metabase.ClientWithResponses, groupId int64) (bool, diag.Diagnostics) {
	getResp, err := client.GetPermissionsGroupWithResponse(ctx, int(groupId))

	diags := checkMetabaseResponse(getResp, err, []int{200, 404}, "get permissions group")
	if diags.HasError() {
		return false, diags
	}

	return getResp.StatusCode() == 200, diags
}

// Returns an error if the given permissions group does not exist.
func checkPermissionsGroupExists(ctx context.Context, client *metabase.ClientWithResponses, groupId int64) diag.Diagnostics {
	exists, diags := permissionsGroupExists(ctx, client, groupId)
	if !diags.HasError() && !exists {
		diags.AddAttributeError(
			path.Root("group"),
			"Group not found.",
			fmt.Sprintf("The permissions group %d does not exist.", groupId),
		)
	}

	return diags
}
