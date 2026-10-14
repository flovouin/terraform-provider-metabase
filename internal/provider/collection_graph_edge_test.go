package provider

import (
	"context"
	"testing"
)

func TestCollectionStatus(t *testing.T) {
	fake, client := newFakeMetabaseServer(t)
	fake.collections["5"] = `{"id": 5, "name": "Active", "archived": false, "is_personal": false}`
	fake.collections["6"] = `{"id": 6, "name": "Archived", "archived": true, "is_personal": false}`
	fake.collections["7"] = `{"id": 7, "name": "Personal", "personal_owner_id": 1, "is_personal": true}`
	fake.collections["8"] = `{"id": 8, "name": "Inside personal", "location": "/7/", "is_personal": true}`

	tests := map[string]struct {
		collection      string
		expectedStatus  collectionStatus
		expectedSummary string
	}{
		"root":            {collection: "root", expectedStatus: collectionStatusActive},
		"active":          {collection: "5", expectedStatus: collectionStatusActive},
		"archived":        {collection: "6", expectedStatus: collectionStatusMissing, expectedSummary: "Collection not found."},
		"missing":         {collection: "9", expectedStatus: collectionStatusMissing, expectedSummary: "Collection not found."},
		"personal":        {collection: "7", expectedStatus: collectionStatusPersonal, expectedSummary: "Personal collection."},
		"inside personal": {collection: "8", expectedStatus: collectionStatusPersonal, expectedSummary: "Personal collection."},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			status, diags := getCollectionStatus(context.Background(), client, test.collection)
			if diags.HasError() || status != test.expectedStatus {
				t.Errorf("Expected status %d, got %d (%v).", test.expectedStatus, status, diags)
			}

			diags = checkCollectionStatus(context.Background(), client, test.collection)
			if test.expectedSummary == "" {
				if len(diags) != 0 {
					t.Errorf("Expected no diagnostics, got: %v", diags)
				}
				return
			}
			checkAttributeError(t, diags, "collection", test.expectedSummary)
		})
	}
}
