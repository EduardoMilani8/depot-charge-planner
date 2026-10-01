package planner

import (
	"math"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }

func chargerMap(in Input) map[string]model.Charger {
	m := make(map[string]model.Charger, len(in.Chargers))
	for _, c := range in.Chargers {
		m[c.ID] = c
	}
	return m
}

// presentBuses returns buses that have arrived, sorted by ID for determinism.
func presentBuses(in Input) []model.Bus {
	out := make([]model.Bus, 0, len(in.Buses))
	for _, b := range in.Buses {
		if b.ArrivalMin <= in.Now {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AvailableKW is the power budget for controllable chargers: the site limit minus
// what offline chargers keep drawing at their last commanded power.
func AvailableKW(in Input) float64 {
	limit := in.Site.LimitKW
	if !finite(limit) || limit < 0 {
		limit = 0
	}
	for _, c := range in.Chargers {
		if c.Status == model.ChargerOffline && finite(c.LastCommandedKW) && c.LastCommandedKW > 0 {
			limit -= c.LastCommandedKW
		}
	}
	if limit < 0 {
		return 0
	}
	return limit
}

// MaxGridKW is the highest grid-side power a charger can deliver to a bus.
func MaxGridKW(c model.Charger, b model.Bus) float64 {
	return math.Min(c.MaxKW, b.MaxBatteryKW/c.Efficiency)
}
