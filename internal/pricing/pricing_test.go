package pricing

import "testing"

func TestEvaluateAppliesCapAndMinFare(t *testing.T) {
	quote := Evaluate(1000, 15000, 1200, 12500, nil)
	if quote.Fare != 1250 {
		t.Fatalf("Evaluate() fare = %d, want 1250", quote.Fare)
	}
	if quote.MultiplierBps != 12500 {
		t.Fatalf("Evaluate() multiplier = %d, want 12500", quote.MultiplierBps)
	}
}

func TestEvaluateHonorsInputRuleNames(t *testing.T) {
	rules := []Rule{{Name: "base"}, {Name: "surge"}}
	quote := Evaluate(5000, 10000, 5000, 15000, rules)
	if len(quote.AppliedRules) != 2 {
		t.Fatalf("Evaluate() applied rules = %d, want 2", len(quote.AppliedRules))
	}
	if quote.Fare != 5000 {
		t.Fatalf("Evaluate() fare = %d, want 5000", quote.Fare)
	}
}
