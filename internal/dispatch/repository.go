package dispatch

import (
	"fmt"
	"sync"
)

type TripEvent struct {
	TripID  string
	From    State
	To      State
	Actor   string
	Payload map[string]any
}

type Trip struct {
	ID      string
	RiderID string
	DriverID string
	Status  State
	Version int
	Events  []TripEvent
}

type Repository struct {
	mu    sync.Mutex
	trips map[string]*Trip
}

func NewRepository() *Repository {
	return &Repository{trips: make(map[string]*Trip)}
}

func (r *Repository) Add(trip *Trip) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.trips == nil {
		r.trips = make(map[string]*Trip)
	}
	r.trips[trip.ID] = trip
}

func (r *Repository) Get(id string) (*Trip, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	trip, ok := r.trips[id]
	return trip, ok
}

func (r *Repository) ApplyTransition(tripID string, expectedVersion int, actor string, payload map[string]any, event Event) (*Trip, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	trip, ok := r.trips[tripID]
	if !ok {
		return nil, fmt.Errorf("trip %s not found", tripID)
	}
	if expectedVersion != 0 && trip.Version != expectedVersion {
		return nil, fmt.Errorf("version mismatch: expected %d, got %d", expectedVersion, trip.Version)
	}

	from := trip.Status
	next, err := Transition(from, event)
	if err != nil {
		return nil, err
	}

	trip.Status = next
	trip.Version++
	trip.Events = append(trip.Events, TripEvent{
		TripID:  tripID,
		From:    from,
		To:      next,
		Actor:   actor,
		Payload: payload,
	})
	return trip, nil
}
