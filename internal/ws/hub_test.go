package ws

import (
	"testing"
	"time"
)

func TestHubBroadcastsOnlyToTargetRoom(t *testing.T) {
	hub := NewHub()
	rider := hub.RegisterClient("r1", "rider")
	driver := hub.RegisterClient("d1", "driver")
	hub.JoinRoom("trip-42", rider)
	hub.JoinRoom("trip-99", driver)

	ctx := map[string]any{"trip_id": "trip-42", "status": "accepted"}
	hub.Publish("trip-42", ctx)

	select {
	case msg := <-rider.Send:
		if msg.Payload["trip_id"] != "trip-42" {
			t.Fatalf("unexpected trip_id: %v", msg.Payload["trip_id"])
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("missing rider broadcast")
	}

	select {
	case <-driver.Send:
		t.Fatal("driver received a message for the wrong trip")
	default:
	}
}
