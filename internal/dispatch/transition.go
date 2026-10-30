package dispatch

import "fmt"

type State string

const (
	Requested   State = "REQUESTED"
	Searching   State = "SEARCHING"
	Offered     State = "OFFERED"
	Accepted    State = "ACCEPTED"
	Arrived     State = "ARRIVED"
	InProgress  State = "IN_PROGRESS"
	Completed   State = "COMPLETED"
	Cancelled   State = "CANCELLED"
	NoDrivers   State = "NO_DRIVERS"
)

type Event string

const (
	EventCreate      Event = "CREATE"
	EventSearch      Event = "SEARCH"
	EventOffer       Event = "OFFER"
	EventDecline     Event = "DECLINE"
	EventExpire      Event = "EXPIRE"
	EventAccept      Event = "ACCEPT"
	EventArrive      Event = "ARRIVE"
	EventStart       Event = "START"
	EventComplete    Event = "COMPLETE"
	EventCancel      Event = "CANCEL"
	EventNoDrivers   Event = "NO_DRIVERS"
)

var transitions = map[State]map[Event]State{
	Requested: {
		EventSearch: Searching,
		EventCancel: Cancelled,
	},
	Searching: {
		EventOffer:     Offered,
		EventNoDrivers: NoDrivers,
		EventCancel:    Cancelled,
	},
	Offered: {
		EventDecline: Searching,
		EventExpire:  Searching,
		EventAccept:  Accepted,
		EventCancel:  Cancelled,
	},
	Accepted: {
		EventArrive: Arrived,
		EventCancel: Cancelled,
	},
	Arrived: {
		EventStart:  InProgress,
		EventCancel: Cancelled,
	},
	InProgress: {
		EventComplete: Completed,
	},
	NoDrivers: {},
	Completed: {},
	Cancelled: {},
}

func Transition(from State, event Event) (State, error) {
	if next, ok := transitions[from][event]; ok {
		return next, nil
	}
	return "", fmt.Errorf("illegal transition: %s -> %s", from, event)
}
