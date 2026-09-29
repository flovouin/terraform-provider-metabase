package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// A fake Metabase API serving the permissions and collection graphs. Like Metabase, it rejects updates made from an
// outdated revision with a 409 status, and only updates the edges present in the request.
type fakeGraphServer struct {
	mutex sync.Mutex
	// The graphs, by API path.
	graphs map[string]*fakeGraph
	// The number of upcoming updates that should be rejected as conflicts, simulating edits made by someone else.
	forcedConflicts int
	// The number of updates rejected because of a conflict, whether forced or not.
	conflicts int
	// The bodies of the updates that were accepted.
	acceptedUpdates []map[string]map[string]json.RawMessage
}

type fakeGraph struct {
	revision int
	groups   map[string]map[string]json.RawMessage
}

func newFakeGraphServer(t *testing.T) (*fakeGraphServer, *metabase.ClientWithResponses) {
	t.Helper()

	f := &fakeGraphServer{
		graphs: map[string]*fakeGraph{
			"/permissions/graph": {groups: map[string]map[string]json.RawMessage{}},
			"/collection/graph":  {groups: map[string]map[string]json.RawMessage{}},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(server.Close)

	client, err := metabase.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("Failed to create the client: %v", err)
	}

	return f, client
}

func (f *fakeGraphServer) handle(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	graph, ok := f.graphs[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		f.writeGraph(w, graph.revision, graph.groups)
	case http.MethodPut:
		var body struct {
			Revision int                                   `json:"revision"`
			Groups   map[string]map[string]json.RawMessage `json:"groups"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if f.forcedConflicts > 0 {
			f.forcedConflicts--
			// Someone else edited the graph in the meantime.
			graph.revision++
		}
		if body.Revision != graph.revision {
			f.conflicts++
			http.Error(w, "Looks like someone else edited the permissions and your data is out of date.", http.StatusConflict)
			return
		}

		for groupId, edges := range body.Groups {
			if graph.groups[groupId] == nil {
				graph.groups[groupId] = map[string]json.RawMessage{}
			}
			for objectId, permissions := range edges {
				graph.groups[groupId][objectId] = permissions
			}
		}
		graph.revision++
		f.acceptedUpdates = append(f.acceptedUpdates, body.Groups)

		// Like Metabase, only the edges that were sent are returned.
		f.writeGraph(w, graph.revision, body.Groups)
	default:
		http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
	}
}

func (f *fakeGraphServer) writeGraph(w http.ResponseWriter, revision int, groups map[string]map[string]json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"revision": revision,
		"groups":   groups,
	})
}

// Makes retries immediate for the duration of the test.
func useFastGraphUpdateRetries(t *testing.T) {
	initialDelay := graphUpdateInitialRetryDelay
	graphUpdateInitialRetryDelay = time.Millisecond
	t.Cleanup(func() { graphUpdateInitialRetryDelay = initialDelay })
}

func TestUpdateDatabasePermissionsRetriesOnConflict(t *testing.T) {
	useFastGraphUpdateRetries(t)
	fake, client := newFakeGraphServer(t)
	fake.graphs["/permissions/graph"].groups["1"] = map[string]json.RawMessage{
		"1": json.RawMessage(`{"view-data":"unrestricted","create-queries":"query-builder-and-native"}`),
	}
	fake.forcedConflicts = graphUpdateMaxAttempts - 1

	permissions, diags := makeDatabasePermissionsFromModel(context.Background(), DatabasePermissionResourceModel{
		ViewData:      types.StringValue("unrestricted"),
		CreateQueries: types.StringValue("query-builder"),
	}.toEdge(), false)
	if diags.HasError() {
		t.Fatalf("Failed to make permissions: %v", diags)
	}

	diags = updateDatabasePermissions(context.Background(), client, 3, 1, func(*metabase.PermissionsGraphDatabasePermissions) *metabase.PermissionsGraphDatabasePermissions {
		return permissions
	})
	if diags.HasError() {
		t.Fatalf("Expected the update to succeed after retrying, got: %v", diags)
	}

	if fake.conflicts != graphUpdateMaxAttempts-1 {
		t.Errorf("Expected %d conflicts, got %d.", graphUpdateMaxAttempts-1, fake.conflicts)
	}
	if len(fake.acceptedUpdates) != 1 {
		t.Fatalf("Expected a single accepted update, got %d.", len(fake.acceptedUpdates))
	}
	update := fake.acceptedUpdates[0]
	if len(update) != 1 || len(update["3"]) != 1 {
		t.Fatalf("Expected the update to only contain the edge for group 3 and database 1, got: %v", update)
	}
	var sent map[string]any
	if err := json.Unmarshal(update["3"]["1"], &sent); err != nil {
		t.Fatalf("Failed to parse the sent permissions: %v", err)
	}
	if sent["view-data"] != "unrestricted" || sent["create-queries"] != "query-builder" {
		t.Errorf("Unexpected permissions sent: %v", sent)
	}
	if _, ok := fake.graphs["/permissions/graph"].groups["1"]["1"]; !ok {
		t.Errorf("The permissions of other groups should be left untouched.")
	}
}

func TestUpdateCollectionPermissionGivesUpAfterMaxAttempts(t *testing.T) {
	useFastGraphUpdateRetries(t)
	fake, client := newFakeGraphServer(t)
	fake.forcedConflicts = graphUpdateMaxAttempts

	diags := updateCollectionPermission(context.Background(), client, 3, "root", metabase.CollectionPermissionLevelRead)
	if !diags.HasError() {
		t.Fatalf("Expected the update to fail after %d conflicts.", graphUpdateMaxAttempts)
	}
	if fake.conflicts != graphUpdateMaxAttempts {
		t.Errorf("Expected %d attempts, got %d.", graphUpdateMaxAttempts, fake.conflicts)
	}
	if len(fake.acceptedUpdates) != 0 {
		t.Errorf("Expected no accepted update, got %d.", len(fake.acceptedUpdates))
	}
}

func TestUpdateGraphWithRetryStopsWhenContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var mutex sync.Mutex
	attempts := 0
	diags := updateGraphWithRetry(ctx, &mutex, "update graph", func() (bool, diag.Diagnostics) {
		attempts++
		return true, nil
	})
	if !diags.HasError() {
		t.Fatalf("Expected an error when the context is done.")
	}
	if attempts != 1 {
		t.Errorf("Expected a single attempt, got %d.", attempts)
	}
}

func TestConcurrentCollectionPermissionUpdatesAreSerialized(t *testing.T) {
	useFastGraphUpdateRetries(t)
	fake, client := newFakeGraphServer(t)

	const numUpdates = 20
	var wg sync.WaitGroup
	errors := make(chan string, numUpdates)
	for i := range numUpdates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			diags := updateCollectionPermission(context.Background(), client, int64(3+i%4), fmt.Sprint(10+i), metabase.CollectionPermissionLevelWrite)
			if diags.HasError() {
				errors <- fmt.Sprint(diags)
			}
		}()
	}
	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Unexpected error: %s", err)
	}
	// Because updates are serialized by the provider, none of them should have been made from an outdated revision.
	if fake.conflicts != 0 {
		t.Errorf("Expected no conflict, got %d.", fake.conflicts)
	}
	edges := 0
	for _, g := range fake.graphs["/collection/graph"].groups {
		edges += len(g)
	}
	if edges != numUpdates {
		t.Errorf("Expected %d edges in the graph, got %d.", numUpdates, edges)
	}
}

func TestMakeDeletedDatabasePermissions(t *testing.T) {
	if makeDeletedDatabasePermissions(nil) != nil {
		t.Errorf("Expected no update when the permissions do not exist.")
	}

	var current metabase.PermissionsGraphDatabasePermissions
	if err := json.Unmarshal([]byte(`{"view-data":"unrestricted","create-queries":"query-builder","download":{"schemas":"full"}}`), &current); err != nil {
		t.Fatalf("Failed to parse permissions: %v", err)
	}

	body, err := json.Marshal(makeDeletedDatabasePermissions(&current))
	if err != nil {
		t.Fatalf("Failed to marshal permissions: %v", err)
	}
	// Metabase rejects a null `view-data`, so the current value must be sent back.
	if string(body) != `{"create-queries":"no","view-data":"unrestricted"}` {
		t.Errorf("Unexpected permissions: %s", body)
	}
}

func TestParseGraphEdgeImportId(t *testing.T) {
	tests := map[string]struct {
		id             string
		expectedGroup  int64
		expectedObject string
		expectError    bool
	}{
		"database":          {id: "3/1", expectedGroup: 3, expectedObject: "1"},
		"root collection":   {id: "3/root", expectedGroup: 3, expectedObject: "root"},
		"missing object":    {id: "3/", expectError: true},
		"missing separator": {id: "3", expectError: true},
		"invalid group":     {id: "all/1", expectError: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			group, object, diags := parseGraphEdgeImportId(test.id, "object")
			if diags.HasError() != test.expectError {
				t.Fatalf("Unexpected diagnostics: %v", diags)
			}
			if !test.expectError && (group != test.expectedGroup || object != test.expectedObject) {
				t.Errorf("Expected %d/%s, got %d/%s.", test.expectedGroup, test.expectedObject, group, object)
			}
		})
	}
}
