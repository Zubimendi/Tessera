package pricing

import "testing"

func TestCalculateSurgeMultiplierFailsSafeWhenDemandIsTooLow(t *testing.T) {
	got := CalculateSurgeMultiplier(10, 2, 10000, 3, 30000)
	if got != 10000 {
		t.Fatalf("CalculateSurgeMultiplier() = %d, want 10000", got)
	}
}

func TestCalculateSurgeMultiplierUsesDemandToSupplyRatio(t *testing.T) {
	got := CalculateSurgeMultiplier(8, 12, 10000, 3, 30000)
	if got != 15000 {
		t.Fatalf("CalculateSurgeMultiplier() = %d, want 15000", got)
	}
}

func TestCalculateSurgeMultiplierCapsAtConfiguredMax(t *testing.T) {
	got := CalculateSurgeMultiplier(1, 100, 10000, 1, 15000)
	if got != 15000 {
		t.Fatalf("CalculateSurgeMultiplier() = %d, want 15000", got)
	}
}
