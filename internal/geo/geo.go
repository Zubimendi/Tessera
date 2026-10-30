package geo

import (
	"fmt"
	"math"
)

type Point struct {
	Lat float64
	Lng float64
}

type Candidate struct {
	DriverID string
	Point    Point
	Distance float64
}

type Store interface {
	AddAvailable(driverID string, point Point) error
	RemoveAvailable(driverID string) (bool, error)
	SearchNearby(center Point, radiusKm float64, limit int) ([]Candidate, error)
}

func ValidatePoint(p Point) error {
	if p.Lat < -90 || p.Lat > 90 {
		return fmt.Errorf("latitude out of range: %f", p.Lat)
	}
	if p.Lng < -180 || p.Lng > 180 {
		return fmt.Errorf("longitude out of range: %f", p.Lng)
	}
	return nil
}

func HaversineKm(a, b Point) float64 {
	const earthRadiusKm = 6371.0

	phi1 := a.Lat * math.Pi / 180.0
	phi2 := b.Lat * math.Pi / 180.0
	deltaPhi := (b.Lat - a.Lat) * math.Pi / 180.0
	deltaLambda := (b.Lng - a.Lng) * math.Pi / 180.0

	aTerm := math.Sin(deltaPhi/2)*math.Sin(deltaPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(deltaLambda/2)*math.Sin(deltaLambda/2)
	c := 2 * math.Atan2(math.Sqrt(aTerm), math.Sqrt(1-aTerm))
	return earthRadiusKm * c
}
