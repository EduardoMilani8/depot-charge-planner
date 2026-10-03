package sim

import (
	"math"
	"sort"
	"time"
)

type Metrics struct {
	Buses          int            `json:"buses"`
	Ready          int            `json:"ready"`
	ReadyPct       float64        `json:"ready_pct"`
	ShortfallKWh   float64        `json:"shortfall_kwh"` // real battery kWh missing vs the TRUE route need (not the planner's effective kWh)
	PeakKW         float64        `json:"peak_kw"`
	PlanViolations int            `json:"plan_violations"` // commanded power above the limit (must be 0)
	OvershootMin   int            `json:"overshoot_min"`   // minutes of physical power above the limit
	EnergyKWh      float64        `json:"energy_kwh"`
	CostBRL        float64        `json:"cost_brl"`
	PlanChanges    int            `json:"plan_changes"`
	LayerTicks     map[string]int `json:"layer_ticks"`
	PlanP99Micros  int64          `json:"plan_p99_micros"`
}

func p99Micros(d []time.Duration) int64 {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(math.Ceil(0.99*float64(len(s)))) - 1
	if idx < 0 {
		idx = 0
	}
	return s[idx].Microseconds()
}

// Aggregate averages rates and costs, sums counts of violations, and keeps the worst p99.
func Aggregate(ms []Metrics) Metrics {
	var a Metrics
	if len(ms) == 0 {
		return a
	}
	n := float64(len(ms))
	for _, m := range ms {
		a.Buses += m.Buses
		a.Ready += m.Ready
		a.ReadyPct += m.ReadyPct / n
		a.ShortfallKWh += m.ShortfallKWh / n
		a.PeakKW += m.PeakKW / n
		a.PlanViolations += m.PlanViolations
		a.OvershootMin += m.OvershootMin
		a.EnergyKWh += m.EnergyKWh / n
		a.CostBRL += m.CostBRL / n
		a.PlanChanges += m.PlanChanges
		if m.PlanP99Micros > a.PlanP99Micros {
			a.PlanP99Micros = m.PlanP99Micros
		}
	}
	a.PlanChanges /= len(ms)
	return a
}
