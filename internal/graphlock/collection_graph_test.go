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
}

func newFakeCollectionGraph(t *testing.T) (*fakeCollectionGraph, *metabase.ClientWithResponses) {
	t.Helper()

	f := &fakeCollectionGraph{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/collection/graph" || r.URL.Query().Get("namespace") != "snippets" {
			http.NotFound(w, r)
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
		_ = tracker.Track(context.Background(), client, func() error {
			graph.revision.Add(1)
			return nil
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

	_ = tracker.Track(context.Background(), client, func() error {
		graph.revision.Add(1)
		return nil
	})
	// A change made by someone else, e.g. in the Metabase interface.
	graph.revision.Add(1)

	sent, err := updateRevision(tracker, client, 3)
	if err == nil || sent != -1 {
		t.Errorf("Expected an error without sending the update, got revision %d (error: %v).", sent, err)
	}
}

func TestCollectionGraphTrackerDoesNotRecordFailedRequests(t *testing.T) {
	graph, client := newFakeCollectionGraph(t)
	graph.revision.Store(3)
	tracker := NewCollectionGraphTracker()

	// The revision is advanced by someone else while the request fails.
	expected := errors.New("request failed")
	err := tracker.Track(context.Background(), client, func() error {
		graph.revision.Add(1)
		return expected
	})
	if !errors.Is(err, expected) {
		t.Errorf("Expected the error returned by the request, got %v.", err)
	}

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
	err = tracker.Track(context.Background(), client, func() error {
		performed = true
		return nil
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
			_ = tracker.Track(context.Background(), client, request)
			_ = tracker.Update(context.Background(), client, 0, func(int) error { return request() })
		}()
	}
	wg.Wait()
}
