package provider

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

// Serializes the read-modify-write cycles on the permissions graph made by resources managing a single edge of it.
// Metabase rejects an update made from an outdated revision, so concurrent updates from the same provider would
// otherwise conflict with one another.
var permissionsGraphMutex sync.Mutex

// Same as `permissionsGraphMutex`, for the collection graph.
var collectionGraphMutex sync.Mutex

// The maximum number of attempts made to update a graph when Metabase reports a conflicting revision.
var graphUpdateMaxAttempts = 5

// The delay before retrying the first conflicting graph update. It is doubled after each attempt.
var graphUpdateInitialRetryDelay = 500 * time.Millisecond

// Performs a read-modify-write cycle on a Metabase permissions graph, while holding the given mutex.
// `update` should fetch the graph to get its current revision, and send the update based on this revision. It should
// return `true` if Metabase rejected the update because the revision was outdated (someone else edited the graph in
// the meantime), in which case the whole cycle is retried with an exponential backoff.
func updateGraphWithRetry(ctx context.Context, mutex *sync.Mutex, operation string, update func() (bool, diag.Diagnostics)) diag.Diagnostics {
	mutex.Lock()
	defer mutex.Unlock()

	delay := graphUpdateInitialRetryDelay
	for attempt := 1; ; attempt++ {
		conflict, diags := update()
		if diags.HasError() || !conflict {
			return diags
		}

		if attempt >= graphUpdateMaxAttempts {
			diags.AddError(
				fmt.Sprintf("Failed to %s because of conflicting updates.", operation),
				fmt.Sprintf("Metabase reported that the graph was modified by someone else %d times in a row. Try again later.", attempt),
			)
			return diags
		}

		// Adding jitter to avoid retrying in lockstep with another client.
		jitteredDelay := delay/2 + rand.N(delay/2+1)
		select {
		case <-ctx.Done():
			diags.AddError(fmt.Sprintf("Failed to %s.", operation), ctx.Err().Error())
			return diags
		case <-time.After(jitteredDelay):
		}
		delay *= 2
	}
}

// Returns whether the Metabase response indicates that the update was rejected because it was made from an outdated
// revision of the graph.
func isGraphRevisionConflict(r metabase.MetabaseResponse) bool {
	return r.StatusCode() == http.StatusConflict
}

// Parses an import ID of the form `<group>/<object>`, where `<group>` is the ID of a permissions group.
func parseGraphEdgeImportId(id string, objectName string) (int64, string, diag.Diagnostics) {
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
