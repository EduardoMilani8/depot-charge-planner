// Package sim is a deterministic discrete-time simulator of a bus depot.
package sim

import (
	"math"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

type FaultKind string

const (
	FaultChargerFail    FaultKind = "charger_fail"     // Target: charger ID; delivers nothing
	FaultChargerOffline FaultKind = "charger_offline"  // Target: charger ID; keeps last power, cannot be commanded
	FaultLimitDrop      FaultKind = "limit_drop"       // Value: factor applied to the site limit
	FaultSoCNoise       FaultKind = "soc_noise"        // Target: bus ID or "*"; Value: std dev in kWh
	FaultSoCBias        FaultKind = "soc_bias"         // Target: bus ID or "*"; Value: kWh added to readings
	FaultSoCFreeze      FaultKind = "soc_freeze"       // Target: bus ID or "*"; reading stops updating
	FaultSoCMissing     FaultKind = "soc_missing"      // Target: bus ID or "*"; no usable reading
	FaultConsumption    FaultKind = "consumption_over" // Target: bus ID; Value: extra kWh needed at departure
	FaultLateArrival    FaultKind = "late_arrival"     // Target: bus ID; Value: minutes of delay
	FaultEarlyDeparture FaultKind = "early_departure"  // Target: bus ID; From: when announced; Value: new departure minute
	FaultPlannerPanic   FaultKind = "planner_panic"    // the normal layer panics while active
	FaultPlannerSlow    FaultKind = "planner_slow"     // the normal layer exceeds its timeout while active
)

// forever is a To value for permanent faults.
const forever = math.MaxInt32

// Fault is active during the window [From, To).
type Fault struct {
	Kind     FaultKind
	Target   string
	From, To model.Minute
	Value    float64
}

func (f Fault) Active(t int) bool { return t >= f.From && t < f.To }

// Tariff has a peak window expressed in minutes of the day.
type Tariff struct {
	PeakFromMin, PeakToMin  int
	PeakPrice, OffPeakPrice float64
}

func (t Tariff) PriceAt(clockMin int) float64 {
	m := ((clockMin % 1440) + 1440) % 1440
	if m >= t.PeakFromMin && m < t.PeakToMin {
		return t.PeakPrice
	}
	return t.OffPeakPrice
}

// BusSpec is a bus plus the truth the planner does not know.
type BusSpec struct {
	Bus           model.Bus // SoCKWh is the true initial SoC; TargetKWh is the forecast the planner sees
	TrueTargetKWh float64   // energy actually needed at departure; 0 means equal to the forecast
}

type Scenario struct {
	Name          string
	Seed          int64
	StartClockMin int // minute of the day at scenario minute 0
	Horizon       model.Minute
	BaseLimitKW   float64
	Chargers      []model.Charger
	Buses         []BusSpec
	Faults        []Fault
	Tariff        Tariff
	FollowSwaps   bool // operators execute the planner's swap recommendations
}
