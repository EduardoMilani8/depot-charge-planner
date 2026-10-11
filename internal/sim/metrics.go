package sim

import (
	"math"
	"sort"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/billing"
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
	// OperatorMoves is the manual work asked of operators: planner swaps executed (only
	// when they follow them) plus buses unplugged by the fifo-unplug routine. Per run;
	// Aggregate gives the mean per run.
	OperatorMoves float64 `json:"operator_moves"`
	// Bill is the night priced with demand blocks (billing.Meter). It is nil, and absent
	// from the JSON, unless the tariff has a demand price or a contract (Tariff.HasDemand),
	// so runs without demand parameters serialise exactly as before. CostBRL stays the
	// energy-only cost of the night.
	Bill *BillMetrics `json:"bill,omitempty"`
}

// BillMetrics is the demand part of a night's bill. Demand is in kW, money in R$.
// MonthlyBRL scales the night's energy cost by Tariff.Nights() and adds the demand
// charge: an estimate, one night standing for the month.
type BillMetrics struct {
	DemandKW        float64 `json:"demand_kw"`         // largest 15-minute block
	DemandPeakKW    float64 `json:"demand_peak_kw"`    // largest block starting in the peak window
	DemandOffPeakKW float64 `json:"demand_offpeak_kw"` // largest block starting outside it
	BilledPeakKW    float64 `json:"billed_peak_kw"`
	BilledOffPeakKW float64 `json:"billed_offpeak_kw"`
	OverrunKW       float64 `json:"overrun_kw"`
	DemandBRL       float64 `json:"demand_brl"`
	OverrunBRL      float64 `json:"overrun_brl"` // included in DemandBRL
	MonthlyBRL      float64 `json:"monthly_brl"`
}

func billMetrics(b billing.Bill) *BillMetrics {
	return &BillMetrics{
		DemandKW: b.DemandKW, DemandPeakKW: b.DemandPeakKW, DemandOffPeakKW: b.DemandOffPeakKW,
		BilledPeakKW: b.BilledPeakKW, BilledOffPeakKW: b.BilledOffPeakKW,
		OverrunKW: b.OverrunKW, DemandBRL: b.DemandBRL, OverrunBRL: b.OverrunBRL, MonthlyBRL: b.MonthlyBRL,
	}
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
// Bill is the mean over the runs that have one (nil when none does).
func Aggregate(ms []Metrics) Metrics {
	var a Metrics
	if len(ms) == 0 {
		return a
	}
	n := float64(len(ms))
	nb := 0 // runs that have a Bill
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
		a.OperatorMoves += m.OperatorMoves / n
		if m.PlanP99Micros > a.PlanP99Micros {
			a.PlanP99Micros = m.PlanP99Micros
		}
		if m.Bill != nil {
			if a.Bill == nil {
				a.Bill = &BillMetrics{}
			}
			nb++
			a.Bill.DemandKW += m.Bill.DemandKW
			a.Bill.DemandPeakKW += m.Bill.DemandPeakKW
			a.Bill.DemandOffPeakKW += m.Bill.DemandOffPeakKW
			a.Bill.BilledPeakKW += m.Bill.BilledPeakKW
			a.Bill.BilledOffPeakKW += m.Bill.BilledOffPeakKW
			a.Bill.OverrunKW += m.Bill.OverrunKW
			a.Bill.DemandBRL += m.Bill.DemandBRL
			a.Bill.OverrunBRL += m.Bill.OverrunBRL
			a.Bill.MonthlyBRL += m.Bill.MonthlyBRL
		}
	}
	if a.Bill != nil {
		d := float64(nb)
		a.Bill.DemandKW /= d
		a.Bill.DemandPeakKW /= d
		a.Bill.DemandOffPeakKW /= d
		a.Bill.BilledPeakKW /= d
		a.Bill.BilledOffPeakKW /= d
		a.Bill.OverrunKW /= d
		a.Bill.DemandBRL /= d
		a.Bill.OverrunBRL /= d
		a.Bill.MonthlyBRL /= d
	}
	a.PlanChanges /= len(ms)
	return a
}
