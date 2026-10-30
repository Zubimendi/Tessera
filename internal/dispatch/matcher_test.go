package dispatch

import (
	"sync"
	"testing"
)

func TestOfferAcceptVsExpiryRace(t *testing.T) {
	repo := NewOfferRepository()
	repo.Store(&Offer{ID: "offer-1", TripID: "trip-1", DriverID: "driver-1", Status: OfferPending})

	var wg sync.WaitGroup
	results := make(chan bool, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		ok, err := repo.ResolveOffer("offer-1", OfferAccept)
		if err != nil {
			results <- false
			return
		}
		results <- ok
	}()

	go func() {
		defer wg.Done()
		ok, err := repo.ResolveOffer("offer-1", OfferExpire)
		if err != nil {
			results <- false
			return
		}
		results <- ok
	}()

	wg.Wait()
	close(results)

	wins := 0
	for ok := range results {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly one winner in accept-vs-expiry race, got %d", wins)
	}
}

func TestOfferRepositoryReflectsFinalState(t *testing.T) {
	repo := NewOfferRepository()
	repo.Store(&Offer{ID: "offer-2", TripID: "trip-2", DriverID: "driver-2", Status: OfferPending})

	ok, err := repo.ResolveOffer("offer-2", OfferAccept)
	if err != nil {
		t.Fatalf("ResolveOffer() error: %v", err)
	}
	if !ok {
		t.Fatal("ResolveOffer() expected accept to win")
	}

	offer, ok := repo.Get("offer-2")
	if !ok {
		t.Fatal("Get() missing offer")
	}
	if offer.Status != OfferAccepted {
		t.Fatalf("offer status = %s, want %s", offer.Status, OfferAccepted)
	}
}
