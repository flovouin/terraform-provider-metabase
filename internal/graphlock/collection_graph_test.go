package graphlock

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flovouin/terraform-provider-metabase/metabase"
)

// A fake Metabase API only serving the revision of the collection graph, which tests can advance.
type fakeCollectionGraph struct {
	revision atomic.Int64
	// Whether reading the graph is forbidden, like for a user without admin permissions.
	forbidden atomic.Bool
}

// A response from the Metabase API, with only a status code.
type fakeResponse int

func (r fakeResponse) StatusCode() int                            { return int(r) }
func (r fakeResponse) BodyString() string                         { return "" }
func (r fakeResponse) HasExpectedStatusWithoutExpectedBody() bool { return false }

func newFakeCollectionGraph(t *testing.T) (*fakeCollectionGraph, *metabase.ClientWithResponses) {
	t.Helper()

	f := &fakeCollectionGraph{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/collection/graph" || r.URL.Query().Get("namespace") != "snippets" {
			http.NotFound(w, r)
			return
		}
		if f.forbidden.Load() {
			http.Error(w, "You don't have permissions to do that.", http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"revision": f.revision.Load(),
			"groups":   map[string]any{},
		})
	}))
	t.Cleanup(server.Close)

	client, err := metabase.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("Failed to create the client: %v", err)
	}

	return f, client
}

// Returns the revision passed to `update` by `Update`, or -1 if `update` was not called, along with the error returned
// by `Update`.
func updateRevision(tracker *CollectionGraphTracker, client metabase.ClientWithResponsesInterface, expectedRevision int) (int, error) {
	sent := -1
	err := tracker.Update(context.Background(), client, expectedRevision, func(revision int) error {
		sent = revision
		return nil
	})

	return sent, err
}

func TestCollectionGraphTrackerIgnoresOwnRevisions(t *testing.T) {
	graph, client := newFakeCollectionGraph(t)
	graph.revision.Store(3)
	tracker := NewCollectionGraphTracker()

	// Two tracked requests, e.g. collections created in the same apply, each recording a revision.
	for range 2 {
		_ = tracker.Track(context.Background(), client, func() (metabase.MetabaseResponse, error) {
			graph.revision.Add(1)
			return fakeResponse(http.StatusOK), nil
		})
	}

	sent, err := updateRevision(tracker, client, 3)
	if err != nil || sent != 5 {
		t.Errorf("Expected the current revision (5) to be sent, got %d (error: %v).", sent, err)
	}
}

func TestCollectionGraphTrackerSendsUnchangedRevision(t *testing.T) {
	graph, client := newFakeCollectionGraph(t)
	graph.revision.Store(3)
	tracker := NewCollectionGraphTracker()

	sent, err := updateRevision(tracker, client, 3)
	if err != nil || sent != 3 {
		t.Errorf("Expected the unchanged revision (3) to be sent, got %d (error: %v).", sent, err)
	}
}

func TestCollectionGraphTrackerRejectsUpdateAfterOtherChanges(t *testing.T) {
	graph, client := newFakeCollectionGraph(t)
	graph.revision.Store(3)
	tracker := NewCollectionGraphTracker()

	_ = tracker.Track(context.Background(), client, func() (metabase.MetabaseResponse, error) {
		graph.revision.Add(1)
		return fakeResponse(http.StatusOK), nil
	})
	// A change made by someone else, e.g. in the Metabase interface.
	graph.revision.Add(1)

	sent, err := updateRevision(tracker, client, 3)
	if err == nil || sent != -1 {
		t.Errorf("Expected an error without sending the update, got revision %d (error: %v).", sent, err)
	}
}

func TestCollectionGraphTrackerDoesNotRecordFailedRequests(t *testing.T) {
	failure := errors.New("request failed")

	tests := map[string]struct {
		resp     metabase.MetabaseResponse
		err      error
		expected error
	}{
		"transport error":      {resp: nil, err: failure, expected: failure},
		"rejected by Metabase": {resp: fakeResponse(http.StatusInternalServerError), err: nil, expected: nil},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			graph, client := newFakeCollectionGraph(t)
			graph.revision.Store(3)
			tracker := NewCollectionGraphTracker()

			// The revision is advanced by someone else while the request fails, e.g. a concurrent creation causing a
			// duplicate key error.
			err := tracker.Track(context.Background(), client, func() (metabase.MetabaseResponse, error) {
				graph.revision.Add(1)
				return test.resp, test.err
			})
			if !errors.Is(err, test.expected) {
				t.Errorf("Expected %v, got %v.", test.expected, err)
			}

			sent, err := updateRevision(tracker, client, 3)
			if err == nil || sent != -1 {
				t.Errorf("Expected an error without sending the update, got revision %d (error: %v).", sent, err)
			}
		})
	}
}

func TestCollectionGraphTrackerPerformsRequestUntrackedWithoutAdminPermissions(t *testing.T) {
	graph, client := newFakeCollectionGraph(t)
	graph.revision.Store(3)
	graph.forbidden.Store(true)
	tracker := NewCollectionGraphTracker()

	performed := false
	err := tracker.Track(context.Background(), client, func() (metabase.MetabaseResponse, error) {
		performed = true
		graph.revision.Add(1)
		return fakeResponse(http.StatusOK), nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v.", err)
	}
	if !performed {
		t.Errorf("Expected the request to be performed without admin permissions.")
	}

	// The revision recorded by the request could not be attributed to it.
	graph.forbidden.Store(false)
	sent, err := updateRevision(tracker, client, 3)
	if err == nil || sent != -1 {
		t.Errorf("Expected an error without sending the update, got revision %d (error: %v).", sent, err)
	}
}

func TestCollectionGraphTrackerDoesNotPerformRequestWithoutRevision(t *testing.T) {
	// No Metabase API is listening, so the revision cannot be fetched.
	client, err := metabase.NewClientWithResponses("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("Failed to create the client: %v", err)
	}
	tracker := NewCollectionGraphTracker()

	performed := false
	err = tracker.Track(context.Background(), client, func() (metabase.MetabaseResponse, error) {
		performed = true
		return fakeResponse(http.StatusOK), nil
	})
	if err == nil {
		t.Errorf("Expected an error when the revision cannot be fetched.")
	}
	if performed {
		t.Errorf("Expected the request not to be performed when the revision cannot be fetched.")
	}
}

func TestCollectionGraphTrackerUpdateReturnsErrors(t *testing.T) {
	_, client := newFakeCollectionGraph(t)
	tracker := NewCollectionGraphTracker()

	expected := errors.New("update failed")
	err := tracker.Update(context.Background(), client, 0, func(int) error { return expected })
	if !errors.Is(err, expected) {
		t.Errorf("Expected the error returned by the update, got %v.", err)
	}
}

func TestCollectionGraphTrackerUpdateWithoutAdminPermissions(t *testing.T) {
	graph, client := newFakeCollectionGraph(t)
	graph.forbidden.Store(true)
	tracker := NewCollectionGraphTracker()

	sent, err := updateRevision(tracker, client, 0)
	if !errors.Is(err, errCollectionGraphForbidden) || sent != -1 {
		t.Errorf("Expected %v without sending the update, got revision %d (error: %v).", errCollectionGraphForbidden, sent, err)
	}
}

func TestCollectionGraphTrackerSerializesRequests(t *testing.T) {
	_, client := newFakeCollectionGraph(t)
	tracker := NewCollectionGraphTracker()

	// Held while a request runs: failing to acquire it means two requests run at the same time.
	var running sync.Mutex
	request := func() error {
		if !running.TryLock() {
			t.Error("Expected the requests to be serialized, but two of them ran at the same time.")
			return nil
		}
		defer running.Unlock()
		time.Sleep(5 * time.Millisecond)
		return nil
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tracker.Track(context.Background(), client, func() (metabase.MetabaseResponse, error) {
				return fakeResponse(http.StatusOK), request()
			})
			_ = tracker.Update(context.Background(), client, 0, func(int) error { return request() })
		}()
	}
	wg.Wait()
}
