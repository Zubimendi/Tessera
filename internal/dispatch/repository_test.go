package dispatch

import "testing"

func TestRepositoryApplyTransitionCAS(t *testing.T) {
	repo := NewRepository()
	trip := &Trip{ID: "t-1", RiderID: "r-1", Status: Requested, Version: 1}
	repo.Add(trip)

	updated, err := repo.ApplyTransition("t-1", 1, "system", map[string]any{"step": "search"}, EventSearch)
	if err != nil {
		t.Fatalf("ApplyTransition() error: %v", err)
	}
	if updated.Status != Searching {
		t.Fatalf("ApplyTransition() status = %s, want %s", updated.Status, Searching)
	}
	if updated.Version != 2 {
		t.Fatalf("ApplyTransition() version = %d, want 2", updated.Version)
	}
	if len(updated.Events) != 1 {
		t.Fatalf("ApplyTransition() events = %d, want 1", len(updated.Events))
	}

	if _, err := repo.ApplyTransition("t-1", 1, "system", nil, EventAccept); err == nil {
		t.Fatal("ApplyTransition() expected version mismatch error")
	}
}

func TestExhaustiveTransitionMatrix(t *testing.T) {
	allowed := map[State]map[Event]bool{
		Requested: {
			EventSearch: true,
			EventCancel: true,
		},
		Searching: {
			EventOffer:    true,
			EventNoDrivers: true,
			EventCancel:   true,
		},
		Offered: {
			EventDecline: true,
			EventExpire:  true,
			EventAccept:  true,
			EventCancel:  true,
		},
		Accepted: {
			EventArrive: true,
			EventCancel: true,
		},
		Arrived: {
			EventStart:  true,
			EventCancel: true,
		},
		InProgress: {
			EventComplete: true,
		},
	}

	states := []State{Requested, Searching, Offered, Accepted, Arrived, InProgress, Completed, Cancelled, NoDrivers}
	events := []Event{EventSearch, EventOffer, EventDecline, EventExpire, EventAccept, EventArrive, EventStart, EventComplete, EventCancel, EventNoDrivers}

	for _, state := range states {
		for _, event := range events {
			_, err := Transition(state, event)
			if allowed[state] != nil && allowed[state][event] {
				if err != nil {
					t.Fatalf("Transition(%s, %s) should be allowed but returned error: %v", state, event, err)
				}
				continue
			}
			if err == nil {
				t.Fatalf("Transition(%s, %s) unexpectedly succeeded for an illegal transition", state, event)
			}
		}
	}
}
