package graphlock

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPermissionsGraphTrackerSerializesUpdates(t *testing.T) {
	tracker := NewPermissionsGraphTracker()

	// Held while an update runs: failing to acquire it means two updates run at the same time.
	var running sync.Mutex
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tracker.Update(func() error {
				if !running.TryLock() {
					t.Error("Expected the updates to be serialized, but two of them ran at the same time.")
					return nil
				}
				defer running.Unlock()
				time.Sleep(5 * time.Millisecond)
				return nil
			})
		}()
	}
	wg.Wait()
}

func TestPermissionsGraphTrackerUpdateReturnsErrors(t *testing.T) {
	tracker := NewPermissionsGraphTracker()

	expected := errors.New("update failed")
	if err := tracker.Update(func() error { return expected }); !errors.Is(err, expected) {
		t.Errorf("Expected the error returned by the update, got %v.", err)
	}
}
