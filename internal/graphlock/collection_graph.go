// Package graphlock serializes the requests made to Metabase which record a new revision of a permissions graph.
//
// Metabase keeps two independent revisions: one for the collection graph, and one for the data permissions graph.
// Concurrent revisions of the same graph can fail with a duplicate key error, and an update of a graph is rejected
// when it is based on an outdated revision. A tracker should be created for each graph of a Metabase instance, and
// shared by everything sending requests to this instance.
//
// The trackers only serialize the requests made by a single process: Terraform starts a separate provider process for
// each provider configuration (e.g. aliases).
package graphlock

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/flovouin/terraform-provider-metabase/metabase"
)

// Serializes the requests which can record a new revision of the collection graph, and records the revisions caused by
// these requests.
//
// Metabase records a new revision of the collection graph when the graph is updated, but also when a collection is
// created, or moved in a way that changes its permissions (e.g. out of a personal collection). Revision IDs are
// assigned sequentially, and concurrent revisions fail with a duplicate key error. This is why those requests are
// serialized.
//
// Updating the graph also requires the revision the update is based on, and Metabase rejects the update if another
// revision has been recorded since. The revision read during the plan is outdated as soon as a collection is created or
// moved, so the revisions recorded as a side effect of the tracked requests are ignored when updating the graph. Other
// revisions (e.g. a change made in the UI, or by another Terraform run) still cause the update to be rejected.
//
// The attribution is a guess: a tracked request is assumed to have recorded a revision when exactly one was recorded
// while it was performed. Many tracked requests do not record any (e.g. renaming a collection), so a revision recorded
// by someone else during such a request is attributed to the provider, and a subsequent update of the graph would not
// be rejected because of it. This only matters when that change targets a permission sent by the update: for example,
// if `write` is granted to a group in the UI while the update sets `read` for the same group and collection, the update
// silently replaces `write` with `read` instead of failing. Other changes are left untouched by the update. Metabase
// records the author and a remark with each revision, so reading them, if the API exposes them, could tell the two
// apart.
type CollectionGraphTracker struct {
	// Serializes the requests which can record a new revision of the collection graph. It also protects
	// `revisionsByProvider`.
	mutex sync.Mutex
	// The revisions recorded by Metabase as a side effect of tracked requests, rather than by someone else changing the
	// graph.
	revisionsByProvider map[int]bool
}

// Creates a tracker for the collection graph of a Metabase instance.
func NewCollectionGraphTracker() *CollectionGraphTracker {
	return &CollectionGraphTracker{revisionsByProvider: map[int]bool{}}
}

// Returns the current revision of the collection graph.
// The revision is shared by all collection namespaces, so only the graph for snippets is requested, which is much
// smaller than the graph for regular collections.
func getCollectionGraphRevision(ctx context.Context, client metabase.ClientWithResponsesInterface) (int, error) {
	getResp, err := client.GetCollectionPermissionsGraphWithResponse(ctx, func(ctx context.Context, req *http.Request) error {
		query := req.URL.Query()
		query.Set("namespace", "snippets")
		req.URL.RawQuery = query.Encode()
		return nil
	})
	if err != nil {
		return 0, err
	}
	if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
		return 0, fmt.Errorf("unexpected response when getting the collection graph revision (status code %d): %s", getResp.StatusCode(), string(getResp.Body))
	}

	return getResp.JSON200.Revision, nil
}

// Performs a request which may record a new revision of the collection graph, e.g. creating or moving a collection, and
// returns the error returned by `request` as is.
// If `request` returns no error and exactly one revision was recorded meanwhile, this revision is attributed to the
// request, even if it was recorded by someone else (see `CollectionGraphTracker`). This includes requests rejected by
// Metabase (e.g. with an error status code), which do not record any revision. If the revision cannot be fetched before
// the request, the error is returned and the request is not performed.
func (t *CollectionGraphTracker) Track(ctx context.Context, client metabase.ClientWithResponsesInterface, request func() error) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	before, err := getCollectionGraphRevision(ctx, client)
	if err != nil {
		return err
	}

	if err := request(); err != nil {
		return err
	}

	// A request records at most one revision, and requests from the provider cannot record any meanwhile as they are
	// serialized. So if exactly one revision was recorded, it is attributed to the request. If more were recorded, some
	// were recorded by someone else and nothing is attributed. The request succeeded at this point, so failing to fetch
	// the revision only means nothing is attributed, rather than an error.
	after, err := getCollectionGraphRevision(ctx, client)
	if err == nil && after == before+1 {
		t.revisionsByProvider[after] = true
	}

	return nil
}

// Updates the collection graph. `expectedRevision` is the revision the update is based on (e.g. read during the plan).
// If all the revisions recorded since then were caused by tracked requests, `update` is called with the current
// revision, and the error it returns is returned as is. Otherwise someone else changed the graph, and an error is
// returned without calling `update`.
func (t *CollectionGraphTracker) Update(ctx context.Context, client metabase.ClientWithResponsesInterface, expectedRevision int, update func(revision int) error) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	currentRevision, err := getCollectionGraphRevision(ctx, client)
	if err != nil {
		return err
	}

	for revision := expectedRevision + 1; revision <= currentRevision; revision++ {
		if !t.revisionsByProvider[revision] {
			return fmt.Errorf("the collection graph was changed by someone else since revision %d (current revision: %d), read it again and retry", expectedRevision, currentRevision)
		}
	}

	return update(currentRevision)
}
