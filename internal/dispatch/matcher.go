package dispatch

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"tessera/internal/geo"
)

type OfferStatus string

const (
	OfferPending OfferStatus = "PENDING"
	OfferAccepted OfferStatus = "ACCEPTED"
	OfferDeclined OfferStatus = "DECLINED"
	OfferExpired OfferStatus = "EXPIRED"
)

type OfferDecision string

const (
	OfferAccept OfferDecision = "ACCEPT"
	OfferDecline OfferDecision = "DECLINE"
	OfferExpire OfferDecision = "EXPIRE"
)

type Strategy interface {
	RankCandidates([]geo.Candidate) []geo.Candidate
}

type DefaultStrategy struct{}

func (DefaultStrategy) RankCandidates(candidates []geo.Candidate) []geo.Candidate {
	out := append([]geo.Candidate(nil), candidates...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Distance == out[j].Distance {
			return out[i].DriverID < out[j].DriverID
		}
		return out[i].Distance < out[j].Distance
	})
	return out
}

type Offer struct {
	ID        string
	TripID    string
	DriverID  string
	Status    OfferStatus
	FareMinor int64
	ExpiresAt time.Time
	Version   int
}

type OfferRepository struct {
	mu     sync.Mutex
	offers map[string]*Offer
}

func NewOfferRepository() *OfferRepository {
	return &OfferRepository{offers: make(map[string]*Offer)}
}

func (r *OfferRepository) Store(offer *Offer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.offers == nil {
		r.offers = make(map[string]*Offer)
	}
	r.offers[offer.ID] = offer
}

func (r *OfferRepository) Get(id string) (*Offer, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	offer, ok := r.offers[id]
	return offer, ok
}

func (r *OfferRepository) ResolveOffer(id string, decision OfferDecision) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	offer, ok := r.offers[id]
	if !ok {
		return false, fmt.Errorf("offer %s not found", id)
	}
	if offer.Status != OfferPending {
		return false, nil
	}

	switch decision {
	case OfferAccept:
		offer.Status = OfferAccepted
	case OfferDecline:
		offer.Status = OfferDeclined
	case OfferExpire:
		offer.Status = OfferExpired
	default:
		return false, fmt.Errorf("unsupported offer decision: %s", decision)
	}

	offer.Version++
	return true, nil
}

type SearchBudget struct {
	MaxOffers int
	MaxWait   time.Duration
}

type Matcher struct {
	Strategy Strategy
	Budget   SearchBudget
	Offers   *OfferRepository
}

func NewMatcher() *Matcher {
	return &Matcher{
		Strategy: DefaultStrategy{},
		Budget: SearchBudget{
			MaxOffers: 8,
			MaxWait:   90 * time.Second,
		},
		Offers: NewOfferRepository(),
	}
}

func (m *Matcher) CreateOffer(tripID, driverID string, fareMinor int64) *Offer {
	offer := &Offer{
		ID:        fmt.Sprintf("offer-%s-%s", tripID, driverID),
		TripID:    tripID,
		DriverID:  driverID,
		Status:    OfferPending,
		FareMinor: fareMinor,
		ExpiresAt: time.Now().Add(10 * time.Second),
	}
	m.Offers.Store(offer)
	return offer
}

func (m *Matcher) ResolveOffer(id string, decision OfferDecision) (bool, error) {
	return m.Offers.ResolveOffer(id, decision)
}
