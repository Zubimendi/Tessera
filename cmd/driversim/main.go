package main

import (
	"fmt"
	"math/rand"
	"time"
)

type Driver struct {
	ID       string
	Lat      float64
	Lng      float64
	Online   bool
	TripID   string
	SpeedKph float64
}

type Rider struct {
	ID     string
	Lat    float64
	Lng    float64
	TripID string
}

func main() {
	seed := time.Now().UnixNano()
	rand.New(rand.NewSource(seed))

	drivers := []Driver{
		{ID: "driver-1", Lat: 40.7128, Lng: -74.0060, Online: true, SpeedKph: 18},
		{ID: "driver-2", Lat: 40.7180, Lng: -74.0100, Online: true, SpeedKph: 22},
		{ID: "driver-3", Lat: 40.7050, Lng: -74.0130, Online: false, SpeedKph: 0},
	}
	riders := []Rider{
		{ID: "rider-1", Lat: 40.7135, Lng: -74.0080, TripID: "trip-1"},
		{ID: "rider-2", Lat: 40.7195, Lng: -74.0040, TripID: "trip-2"},
	}

	fmt.Println("Tessera driver simulator starting")
	fmt.Println("Drivers:")
	for _, driver := range drivers {
		fmt.Printf("- %s online=%t position=(%.4f, %.4f)\n", driver.ID, driver.Online, driver.Lat, driver.Lng)
	}
	fmt.Println("Riders:")
	for _, rider := range riders {
		fmt.Printf("- %s trip=%s position=(%.4f, %.4f)\n", rider.ID, rider.TripID, rider.Lat, rider.Lng)
	}
	fmt.Println("Simulation ready: dispatch loop, location pings, and trip updates are expected to run under the gateway layer.")
}
