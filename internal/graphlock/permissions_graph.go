package graphlock

import "sync"

// Serializes the updates of the data permissions graph.
//
// Unlike the collection graph, Metabase only records a new revision of the data permissions graph when the graph is
// updated (creating databases or groups does not record one), so there are no side effects to track. Concurrent updates
// based on the same revision are rejected with a 409, but they can also fail with a 500 error.
type PermissionsGraphTracker struct {
	mutex sync.Mutex
}

// Creates a tracker for the data permissions graph of a Metabase instance.
func NewPermissionsGraphTracker() *PermissionsGraphTracker {
	return &PermissionsGraphTracker{}
}

// Updates the data permissions graph. The error returned by `update` is returned as is.
func (t *PermissionsGraphTracker) Update(update func() error) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	return update()
}
