package geo

import (
	"context"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestValidatePoint(t *testing.T) {
	if err := ValidatePoint(Point{Lat: 0, Lng: 0}); err != nil {
		t.Fatalf("ValidatePoint() unexpected error: %v", err)
	}
	if err := ValidatePoint(Point{Lat: 91, Lng: 0}); err == nil {
		t.Fatal("ValidatePoint() expected latitude validation error")
	}
	if err := ValidatePoint(Point{Lat: 0, Lng: 181}); err == nil {
		t.Fatal("ValidatePoint() expected longitude validation error")
	}
}

func TestHaversineKm(t *testing.T) {
	start := Point{Lat: 0, Lng: 0}
	end := Point{Lat: 0, Lng: 1}
	km := HaversineKm(start, end)
	if km < 110 || km > 112 {
		t.Fatalf("HaversineKm() = %f, want approx 111 km", km)
	}
}

func TestClaimDriverSingleWinner(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewRedisStore(client, "default")

	if err := store.AddAvailable(context.Background(), "driver-1", Point{Lat: 0, Lng: 0}); err != nil {
		t.Fatalf("AddAvailable() error: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan bool, 1000)
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := store.ClaimDriver(context.Background(), "driver-1", "offer-1")
			if err != nil {
				results <- false
				return
			}
			results <- claimed
		}()
	}
	wg.Wait()
	close(results)

	wins := 0
	for claimed := range results {
		if claimed {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly one claim winner, got %d", wins)
	}
}
