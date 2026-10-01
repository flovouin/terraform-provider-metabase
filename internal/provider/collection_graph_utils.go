package provider

import (
	"context"
	"net/http"
	"sync"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Metabase records a new revision of the collection graph when the graph is updated, but also when a collection is
// created, or moved in a way that changes its permissions (e.g. out of a personal collection). Revision IDs are
// assigned sequentially, and concurrent revisions fail with a duplicate key error. This is why those requests are
// serialized.
//
// Updating the graph also requires the revision the update is based on, and Metabase rejects the update if another
// revision has been recorded since. The revision read during the plan is outdated as soon as the provider creates or
// moves a collection, so the revisions recorded as a side effect of the provider's own requests are tracked and ignored
// when updating the graph. Any other revision (e.g. a change made in the UI, or by another Terraform run) still causes the
// update to be rejected.
//
// This only covers the requests of a single provider instance: Terraform starts a separate provider process for each
// provider configuration (e.g. aliases).
type collectionGraphTracker struct {
	// Serializes the requests which can record a new revision of the collection graph. It also protects `ownRevisions`.
	mutex sync.Mutex
	// The revisions recorded by Metabase as a side effect of requests made by the provider.
	ownRevisions map[int]bool
}

var collectionGraph = collectionGraphTracker{ownRevisions: map[int]bool{}}

// Returns the current revision of the collection graph.
// The revision is shared by all collection namespaces, so only the graph for snippets is requested, which is much
// smaller than the graph for regular collections.
func getCollectionGraphRevision(ctx context.Context, client metabase.ClientWithResponsesInterface) (int, diag.Diagnostics) {
	getResp, err := client.GetCollectionPermissionsGraphWithResponse(ctx, func(ctx context.Context, req *http.Request) error {
		query := req.URL.Query()
		query.Set("namespace", "snippets")
		req.URL.RawQuery = query.Encode()
		return nil
	})

	diags := checkMetabaseResponse(getResp, err, []int{200}, "get collection graph revision")
	if diags.HasError() {
		return 0, diags
	}

	return getResp.JSON200.Revision, diags
}

// Performs a request which may record a new revision of the collection graph, e.g. creating or moving a collection.
// `request` should return whether it succeeded. If it did and exactly one revision was recorded meanwhile, this revision
// is attributed to the request. Tracking is best effort: if the revision cannot be fetched, nothing is recorded, and a
// subsequent update of the graph is rejected by Metabase as it would be without tracking.
func (t *collectionGraphTracker) trackRequest(ctx context.Context, client metabase.ClientWithResponsesInterface, request func() bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	before, beforeDiags := getCollectionGraphRevision(ctx, client)
	if !request() || beforeDiags.HasError() {
		return
	}

	after, afterDiags := getCollectionGraphRevision(ctx, client)
	if !afterDiags.HasError() && after == before+1 {
		t.ownRevisions[after] = true
	}
}

// Returns the revision an update of the collection graph should be based on, given the one known by Terraform (read
// during the plan). If all revisions recorded since then were caused by the provider, the current revision is returned.
// Otherwise the known revision is returned as is, and Metabase will reject the update.
// The mutex must be held when calling this function, and until the update has been performed.
func (t *collectionGraphTracker) baseRevision(ctx context.Context, client metabase.ClientWithResponsesInterface, known int) (int, diag.Diagnostics) {
	current, diags := getCollectionGraphRevision(ctx, client)
	if diags.HasError() {
		return 0, diags
	}

	if current <= known {
		return known, diags
	}

	for revision := known + 1; revision <= current; revision++ {
		if !t.ownRevisions[revision] {
			return known, diags
		}
	}

	return current, diags
}
