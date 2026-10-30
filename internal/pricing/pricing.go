package pricing

import "fmt"

type RuleType string

const (
	RatioTiers   RuleType = "RATIO_TIERS"
	TimeWindow   RuleType = "TIME_WINDOW"
	ZoneOverride RuleType = "ZONE_OVERRIDE"
	Smoothing    RuleType = "SMOOTHING"
	Clamp        RuleType = "CLAMP"
)

type Rule struct {
	Type     RuleType
	Name     string
	ValueBps int32
}

type Quote struct {
	BaseAmount      int64
	MultiplierBps   int32
	Fare            int64
	AppliedRules    []string
	RuleSetVersion  string
	ExpiresAtUnixMS int64
}

func Evaluate(baseAmount int64, multiplierBps int32, minFare int64, capBps int32, rules []Rule) Quote {
	if baseAmount < 0 {
		baseAmount = 0
	}
	if multiplierBps < 0 {
		multiplierBps = 0
	}
	if capBps > 0 && multiplierBps > capBps {
		multiplierBps = capBps
	}

	fare := (baseAmount * int64(multiplierBps)) / 10000
	if fare < minFare {
		fare = minFare
	}

	applied := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.Name != "" {
			applied = append(applied, rule.Name)
		}
	}

	return Quote{
		BaseAmount:    baseAmount,
		MultiplierBps: multiplierBps,
		Fare:          fare,
		AppliedRules:  applied,
	}
}

func (q Quote) String() string {
	return fmt.Sprintf("base=%d multiplier_bps=%d fare=%d rules=%v", q.BaseAmount, q.MultiplierBps, q.Fare, q.AppliedRules)
}
