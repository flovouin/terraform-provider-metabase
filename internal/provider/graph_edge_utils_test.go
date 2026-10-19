package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestParseGraphEdgeId(t *testing.T) {
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
			group, object, diags := parseGraphEdgeId(test.id, "object")
			if diags.HasError() != test.expectError {
				t.Fatalf("Unexpected diagnostics: %v", diags)
			}
			if !test.expectError && (group != test.expectedGroup || object != test.expectedObject) {
				t.Errorf("Expected %d/%s, got %d/%s.", test.expectedGroup, test.expectedObject, group, object)
			}
			// The ID made from the parsed group and object is the original ID.
			if !test.expectError && makeGraphEdgeId(group, object) != test.id {
				t.Errorf("Expected the ID %s to be made again, got %s.", test.id, makeGraphEdgeId(group, object))
			}
		})
	}
}

// Checks that the diagnostics contain a single error, with the given summary, on the given attribute.
func checkAttributeError(t *testing.T, diags diag.Diagnostics, attribute string, summary string) {
	t.Helper()

	if len(diags) != 1 || !diags.HasError() {
		t.Fatalf("Expected a single error, got: %v", diags)
	}

	d, ok := diags[0].(diag.DiagnosticWithPath)
	if !ok || !d.Path().Equal(path.Root(attribute)) || d.Summary() != summary {
		t.Errorf("Expected the error %q on %s, got: %v", summary, attribute, diags[0])
	}
}

func TestPermissionsGroupExists(t *testing.T) {
	fake, client := newFakeMetabaseServer(t)
	fake.missingGroups["4"] = true

	exists, diags := permissionsGroupExists(context.Background(), client, 3)
	if diags.HasError() || !exists {
		t.Errorf("Expected group 3 to exist, got %v (%v).", exists, diags)
	}
	if diags := checkPermissionsGroupExists(context.Background(), client, 3); len(diags) != 0 {
		t.Errorf("Expected no diagnostics for group 3, got: %v", diags)
	}

	exists, diags = permissionsGroupExists(context.Background(), client, 4)
	if diags.HasError() || exists {
		t.Errorf("Expected group 4 not to exist, got %v (%v).", exists, diags)
	}
	checkAttributeError(t, checkPermissionsGroupExists(context.Background(), client, 4), "group", "Group not found.")
}

// Creates a permissions group directly using the Metabase API, to act as a group managed outside of Terraform.
// The group is deleted at the end of the test.
func testAccCreateBystanderGroup(t *testing.T, name string) int {
	t.Helper()

	response, err := testAccMetabaseClient.CreatePermissionsGroupWithResponse(context.Background(), metabase.CreatePermissionsGroupBody{
		Name: name,
	})
	if err != nil || response.StatusCode() != 200 {
		t.Fatalf("Failed to create permissions group: %v, %v", err, response)
	}

	groupId := response.JSON200.Id
	t.Cleanup(func() {
		_, _ = testAccMetabaseClient.DeletePermissionsGroupWithResponse(context.Background(), groupId)
	})

	return groupId
}

// Returns the value of an attribute of a resource in the state, to be used by checks in later steps.
func testAccStoreAttribute(resourceName string, attribute string, value *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		*value = rs.Primary.Attributes[attribute]
		return nil
	}
}

func mustParseInt64(t *testing.T, s string) int64 {
	t.Helper()

	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse integer %q: %v", s, err)
	}

	return i
}

// A fake Metabase API serving permissions groups, collections, databases and the permissions graph. Like Metabase, it
// rejects updates of the graph made from an outdated revision with a 409 status unless they are forced, only updates
// the edges present in the request, and responds with the graph of the groups present in the request.
type fakeMetabaseServer struct {
	mutex sync.Mutex
	// The IDs of the permissions groups which do not exist. All the other groups exist.
	missingGroups map[string]bool
	// The collections returned by the API, by ID. The other collections do not exist.
	collections map[string]string
	// The IDs of the databases which do not exist. All the other databases exist.
	missingDatabases map[string]bool
	// The revision and the edges of the permissions graph.
	permissionsGraphRevision int
	permissionsGraph         map[string]map[string]json.RawMessage
	// The bodies of the updates of the permissions graph that were accepted.
	acceptedUpdates []map[string]map[string]json.RawMessage
}

func newFakeMetabaseServer(t *testing.T) (*fakeMetabaseServer, *metabase.ClientWithResponses) {
	t.Helper()

	f := &fakeMetabaseServer{
		missingGroups:    map[string]bool{},
		collections:      map[string]string{},
		missingDatabases: map[string]bool{},
		permissionsGraph: map[string]map[string]json.RawMessage{},
	}

	server := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(server.Close)

	client, err := metabase.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("Failed to create the client: %v", err)
	}

	return f, client
}

func (f *fakeMetabaseServer) handle(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if r.URL.Path == "/permissions/graph" {
		f.handlePermissionsGraph(w, r)
		return
	}

	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	if groupId, ok := strings.CutPrefix(r.URL.Path, "/permissions/graph/group/"); ok {
		// Like Metabase, the graph is empty for a group which does not exist.
		groups := map[string]map[string]json.RawMessage{}
		if edges, exists := f.permissionsGraph[groupId]; exists && !f.missingGroups[groupId] {
			groups[groupId] = edges
		}
		writeFakePermissionsGraph(w, f.permissionsGraphRevision, groups)
		return
	}

	if groupId, ok := strings.CutPrefix(r.URL.Path, "/permissions/group/"); ok {
		if f.missingGroups[groupId] {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id": %s, "name": "Group %s"}`, groupId, groupId)
		return
	}

	if databaseId, ok := strings.CutPrefix(r.URL.Path, "/database/"); ok {
		if f.missingDatabases[databaseId] {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id": %s, "name": "Database %s", "engine": "postgres", "details": {}}`, databaseId, databaseId)
		return
	}

	if collectionId, ok := strings.CutPrefix(r.URL.Path, "/collection/"); ok {
		collection, exists := f.collections[collectionId]
		if !exists {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, collection)
		return
	}

	http.NotFound(w, r)
}

func (f *fakeMetabaseServer) handlePermissionsGraph(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPut:
		var body struct {
			Revision int                                   `json:"revision"`
			Groups   map[string]map[string]json.RawMessage `json:"groups"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if r.URL.Query().Get("force") != "true" && body.Revision != f.permissionsGraphRevision {
			http.Error(w, "Looks like someone else edited the permissions and your data is out of date.", http.StatusConflict)
			return
		}

		for groupId, edges := range body.Groups {
			if f.permissionsGraph[groupId] == nil {
				f.permissionsGraph[groupId] = map[string]json.RawMessage{}
			}
			for databaseId, permissions := range edges {
				f.permissionsGraph[groupId][databaseId] = permissions
			}
		}
		f.permissionsGraphRevision++
		f.acceptedUpdates = append(f.acceptedUpdates, body.Groups)

		// Like Metabase, all the edges of the groups that were sent are returned.
		groups := map[string]map[string]json.RawMessage{}
		for groupId := range body.Groups {
			groups[groupId] = f.permissionsGraph[groupId]
		}
		writeFakePermissionsGraph(w, f.permissionsGraphRevision, groups)
	default:
		http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
	}
}

func writeFakePermissionsGraph(w http.ResponseWriter, revision int, groups map[string]map[string]json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"revision": revision,
		"groups":   groups,
	})
}
