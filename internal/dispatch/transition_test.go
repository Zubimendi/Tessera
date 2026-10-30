package dispatch

import "testing"

func TestValidTransitions(t *testing.T) {
	cases := []struct {
		from  State
		event Event
		to    State
	}{
		{Requested, EventSearch, Searching},
		{Searching, EventOffer, Offered},
		{Offered, EventDecline, Searching},
		{Offered, EventExpire, Searching},
		{Offered, EventAccept, Accepted},
		{Accepted, EventArrive, Arrived},
		{Arrived, EventStart, InProgress},
		{InProgress, EventComplete, Completed},
		{Requested, EventCancel, Cancelled},
		{Searching, EventCancel, Cancelled},
		{Offered, EventCancel, Cancelled},
		{Accepted, EventCancel, Cancelled},
		{Arrived, EventCancel, Cancelled},
	}

	for _, tc := range cases {
		got, err := Transition(tc.from, tc.event)
		if err != nil {
			t.Fatalf("Transition(%s, %s) returned error: %v", tc.from, tc.event, err)
		}
		if got != tc.to {
			t.Fatalf("Transition(%s, %s) = %s, want %s", tc.from, tc.event, got, tc.to)
		}
	}
}

func TestIllegalTransition(t *testing.T) {
	if _, err := Transition(InProgress, EventCancel); err == nil {
		t.Fatal("expected illegal transition from IN_PROGRESS to CANCELLED")
	}
	if _, err := Transition(Requested, EventAccept); err == nil {
		t.Fatal("expected illegal transition from REQUESTED to ACCEPT")
	}
}
