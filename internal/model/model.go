// Package model defines the domain types shared by the planner and the simulator.
package model

import "math"

// Minute counts minutes since the start of a scenario.
type Minute = int

type ChargerStatus int

const (
	ChargerOK ChargerStatus = iota
	ChargerFaulted
	ChargerOffline
)

const (
	// TaperStartFrac is the SoC fraction where the battery starts reducing acceptance.
	TaperStartFrac = 0.8
	// TaperMinFactor is the acceptance factor reached at 100% SoC.
	TaperMinFactor = 0.2
	// PlannerTailCostFactor is the time multiplier the planner applies to energy above
	// TaperStartFrac. With acceptance falling linearly from 1 to TaperMinFactor over
	// the last 20% of SoC, the real time multiplier for charging from the knee up to a
	// fraction x of that band is ln(1/(1-0.8x))/(0.8x): about 1.53 at x = 0.75 (95%
	// SoC) and ln(5)/0.8 ≈ 2.012 for the whole band (80% -> 100%). (It is the
	// time-weighted average of 1/factor, not 1/mean(factor) = 1/0.6 ≈ 1.67.) So 2.0 is
	// conservative for targets up to about 99.9% SoC and about 0.6% optimistic for a
	// full 100% charge (≈ 17 s on a 24-minute tail); the simulator measures this in
	// TestTaperExtraTimeAgainstPlannerAssumption. When the charger, not the battery,
	// limits the power, the taper binds later and the real multiplier is smaller still.
	PlannerTailCostFactor = 2.0
)

// Site describes the depot's grid connection.
type Site struct {
	LimitKW float64 // total grid-side power available right now
	StepMin int     // planning step in minutes
}

// Charger is one charging point. Power values are grid-side kW.
type Charger struct {
	ID              string
	MaxKW           float64
	MinKW           float64 // charger cannot deliver between 0 and MinKW
	Efficiency      float64 // battery power / grid power, in (0,1]
	Status          ChargerStatus
	LastCommandedKW float64 // last power sent; offline chargers keep drawing it
}

func (c Charger) Healthy() bool { return c.Status == ChargerOK }

// Bus is a vehicle as the planner sees it (SoC is an estimate with age/confidence).
type Bus struct {
	ID            string
	CapacityKWh   float64
	SoCKWh        float64
	SoCAgeMin     int
	SoCConfidence float64 // 0..1
	TargetKWh     float64 // battery energy required at departure
	ArrivalMin    Minute
	DepartureMin  Minute
	MaxBatteryKW  float64 // max battery-side acceptance below the taper
	ChargerID     string  // "" when waiting for a charger
}

// TaperFactor is the fraction of MaxBatteryKW the battery accepts at a given SoC.
func TaperFactor(soc, capacity float64) float64 {
	if capacity <= 0 {
		return 0
	}
	f := soc / capacity
	if f <= TaperStartFrac {
		return 1
	}
	if f >= 1 {
		return 0
	}
	return 1 - (f-TaperStartFrac)/(1-TaperStartFrac)*(1-TaperMinFactor)
}

// EffectiveEnergy is the battery energy needed to go from soc to target, with the
// part above the taper knee weighted by PlannerTailCostFactor (conservative).
func EffectiveEnergy(soc, target, capacity float64) float64 {
	if target <= soc {
		return 0
	}
	knee := TaperStartFrac * capacity
	below := math.Max(0, math.Min(target, knee)-soc)
	above := (target - soc) - below
	return below + PlannerTailCostFactor*above
}
