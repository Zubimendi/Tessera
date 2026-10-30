package pricing

const (
	DefaultMinDemand = 3
	DefaultMaxSurge  = 30000
	DefaultBaseline  = 10000
)

type SurgeSnapshot struct {
	Cell                  string
	Supply                int
	Demand                int
	MultiplierBps         int32
	PreviousMultiplierBps int32
	MinDemand             int
	MaxMultiplierBps      int32
}

func CalculateSurgeMultiplier(supply, demand int, previousMultiplierBps int32, minDemand int, maxMultiplierBps int32) int32 {
	if minDemand <= 0 {
		minDemand = DefaultMinDemand
	}
	if maxMultiplierBps <= 0 {
		maxMultiplierBps = DefaultMaxSurge
	}

	if demand < minDemand {
		return DefaultBaseline
	}
	if supply <= 0 {
		return maxMultiplierBps
	}

	ratio := (int64(demand) * 10000) / int64(supply)
	multiplier := int32(DefaultBaseline)
	switch {
	case ratio >= 15000:
		multiplier = 15000
	case ratio >= 12500:
		multiplier = 12000
	case ratio >= 10000:
		multiplier = 10000
	default:
		multiplier = 10000
	}

	if previousMultiplierBps > 0 {
		delta := int64(multiplier) - int64(previousMultiplierBps)
		limit := int64(5000)
		switch {
		case delta > limit:
			multiplier = int32(previousMultiplierBps + int32(limit))
		case delta < -limit:
			multiplier = int32(previousMultiplierBps - int32(limit))
		}
	}

	if multiplier < DefaultBaseline {
		multiplier = DefaultBaseline
	}
	if maxMultiplierBps > 0 && multiplier > maxMultiplierBps {
		multiplier = maxMultiplierBps
	}
	return multiplier
}

func SurgeMultiplierForCell(supply, demand int) int32 {
	return CalculateSurgeMultiplier(supply, demand, 0, DefaultMinDemand, DefaultMaxSurge)
}

func (s SurgeSnapshot) EffectiveMultiplier() int32 {
	if s.MinDemand <= 0 {
		s.MinDemand = DefaultMinDemand
	}
	if s.MaxMultiplierBps <= 0 {
		s.MaxMultiplierBps = DefaultMaxSurge
	}
	if s.MultiplierBps > 0 {
		return s.MultiplierBps
	}
	return CalculateSurgeMultiplier(s.Supply, s.Demand, s.PreviousMultiplierBps, s.MinDemand, int32(s.MaxMultiplierBps))
}
