package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"tessera/internal/pricing"
)

func main() {
	interval := 30 * time.Second
	if value := os.Getenv("SURGE_INTERVAL_SECONDS"); value != "" {
		if seconds, err := time.ParseDuration(value + "s"); err == nil {
			interval = seconds
		}
	}

	log.Printf("surge calculator started: interval=%s", interval)
	for {
		cell := fmt.Sprintf("d%04d", rand.Intn(1000))
		supply := 12 + rand.Intn(30)
		demand := rand.Intn(20)
		multiplier := pricing.CalculateSurgeMultiplier(supply, demand, 10000, pricing.DefaultMinDemand, pricing.DefaultMaxSurge)
		payload := map[string]any{
			"cell":          cell,
			"supply":        supply,
			"demand":        demand,
			"multiplier_bps": multiplier,
			"source":        "scheduler",
		}
		if err := emitSnapshot(payload); err != nil {
			log.Printf("surge snapshot failed: %v", err)
		}
		time.Sleep(interval)
	}
}

func emitSnapshot(payload map[string]any) error {
	addr := os.Getenv("SURGE_EXPORT_URL")
	if addr == "" {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := http.Post(addr, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("export returned status %s", resp.Status)
	}
	return nil
}
