package planner

import (
	"math"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }

// chargerMap indexes chargers by ID. If an ID is listed more than once the last entry
// wins here; callers that must not trust such IDs use duplicateChargerIDs.
func chargerMap(in Input) map[string]model.Charger {
	m := make(map[string]model.Charger, len(in.Chargers))
	for _, c := range in.Chargers {
		m[c.ID] = c
	}
	return m
}

// duplicateChargerIDs returns the IDs listed by more than one charger entry.
func duplicateChargerIDs(in Input) map[string]bool {
	seen := make(map[string]bool, len(in.Chargers))
	dup := map[string]bool{}
	for _, c := range in.Chargers {
		if seen[c.ID] {
			dup[c.ID] = true
		}
		seen[c.ID] = true
	}
	return dup
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
