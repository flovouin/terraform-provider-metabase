package metabase

import (
	"encoding/json"
	"testing"
)

func TestIsRevokedDatabasePermissions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		permissions string
		expected    bool
	}{
		"revoked, as returned by the free edition": {`{"view-data":"unrestricted"}`, true},
		"revoked, with explicit values":            {`{"view-data":"unrestricted","create-queries":"no","download":{"schemas":"none"},"data-model":{"schemas":"none"},"details":"no"}`, true},
		"blocked view data":                        {`{"view-data":"blocked"}`, false},
		"granular view data":                       {`{"view-data":{"PUBLIC":"unrestricted"}}`, false},
		"create queries":                           {`{"view-data":"unrestricted","create-queries":"query-builder"}`, false},
		"download":                                 {`{"view-data":"unrestricted","download":{"schemas":"full"}}`, false},
		"data model":                               {`{"view-data":"unrestricted","data-model":{"schemas":"all"}}`, false},
		"details":                                  {`{"view-data":"unrestricted","details":"yes"}`, false},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var permissions PermissionsGraphDatabasePermissions
			if err := json.Unmarshal([]byte(test.permissions), &permissions); err != nil {
				t.Fatalf("Failed to parse permissions: %v", err)
			}

			if actual := permissions.IsRevoked(); actual != test.expected {
				t.Errorf("Expected %v, got %v.", test.expected, actual)
			}
		})
	}
}

func TestNewRevokedDatabasePermissions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		advancedPermissions bool
		expected            string
	}{
		"free edition": {
			expected: `{"create-queries":"no","download":{"schemas":"none"},"view-data":"unrestricted"}`,
		},
		"advanced permissions": {
			advancedPermissions: true,
			expected:            `{"create-queries":"no","data-model":{"schemas":"none"},"details":"no","download":{"schemas":"none"},"view-data":"unrestricted"}`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			permissions := NewRevokedDatabasePermissions(test.advancedPermissions)

			actual, err := json.Marshal(permissions)
			if err != nil {
				t.Fatalf("Failed to marshal permissions: %v", err)
			}
			if string(actual) != test.expected {
				t.Errorf("Expected %s, got %s.", test.expected, actual)
			}
			if !permissions.IsRevoked() {
				t.Errorf("Expected the revoked permissions to be detected as revoked.")
			}
		})
	}
}

func TestWithOmittedPermissionsRevoked(t *testing.T) {
	parse := func(s string) PermissionsGraphDatabasePermissions {
		var p PermissionsGraphDatabasePermissions
		if err := json.Unmarshal([]byte(s), &p); err != nil {
			t.Fatalf("Failed to parse permissions: %v", err)
		}
		return p
	}

	tests := map[string]struct {
		permissions string
		expected    string
	}{
		"missing": {
			permissions: `{"view-data": "unrestricted"}`,
			expected:    `{"view-data": "unrestricted", "download": {"schemas": "none"}, "data-model": {"schemas": "none"}, "details": "no"}`,
		},
		"present": {
			permissions: `{"view-data": "unrestricted", "download": {"schemas": "full"}, "data-model": {"schemas": "all"}, "details": "yes"}`,
			expected:    `{"view-data": "unrestricted", "download": {"schemas": "full"}, "data-model": {"schemas": "all"}, "details": "yes"}`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			actual, err := json.Marshal(parse(test.permissions).WithOmittedPermissionsRevoked())
			if err != nil {
				t.Fatalf("Failed to serialize the permissions: %v", err)
			}
			expected, err := json.Marshal(parse(test.expected))
			if err != nil {
				t.Fatalf("Failed to serialize the expected permissions: %v", err)
			}
			if string(actual) != string(expected) {
				t.Errorf("Expected %s, got %s.", expected, actual)
			}
		})
	}
}

func TestFindDatabasePermissions(t *testing.T) {
	t.Parallel()

	var graph PermissionsGraph
	if err := json.Unmarshal([]byte(`{"revision":1,"groups":{"3":{"1":{"view-data":"unrestricted","create-queries":"query-builder"},"2":{}}}}`), &graph); err != nil {
		t.Fatalf("Failed to parse the graph: %v", err)
	}

	tests := map[string]struct {
		groupId       int
		databaseId    int
		createQueries string
	}{
		"existing edge":          {groupId: 3, databaseId: 1, createQueries: "query-builder"},
		"edge without view data": {groupId: 3, databaseId: 2},
		"missing database":       {groupId: 3, databaseId: 9},
		"missing group":          {groupId: 4, databaseId: 1},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			permissions := graph.FindDatabasePermissions(test.groupId, test.databaseId)

			if test.createQueries == "" {
				if permissions != nil {
					t.Errorf("Expected no permissions, got %v.", permissions)
				}
				return
			}

			if permissions == nil || permissions.CreateQueries == nil {
				t.Fatalf("Expected permissions, got %v.", permissions)
			}
			createQueries, err := permissions.CreateQueries.AsPermissionsGraphDatabasePermissionsCreateQueries0()
			if err != nil || string(createQueries) != test.createQueries {
				t.Errorf("Expected create-queries %q, got %q (%v).", test.createQueries, createQueries, err)
			}
		})
	}
}
