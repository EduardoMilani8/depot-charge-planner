// Package planner decides how much power each charger delivers to each bus.
package planner

import (
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// Config holds the planner tunables.
type Config struct {
	MarginKWh             float64       // extra battery energy planned on top of the route target
	StaleAfterMin         int           // SoC readings older than this are unreliable
	MinConfidence         float64       // SoC readings below this confidence are unreliable
	UnreliablePenaltyFrac float64       // fraction of capacity subtracted from unreliable SoC
	MaxUnreliableFrac     float64       // above this fraction of unreliable buses, use the safe profile
	SurplusLaxityMin      float64       // spare power only goes to buses with less laxity than this
	SwapUrgentLaxityMin   float64       // waiting buses with less laxity than this get swap suggestions
	SwapDonorGapMin       float64       // donor bus must have at least this much more laxity
	LastPlanTTLMin        int           // how long the last valid plan may be reused
	Timeout               time.Duration // max time for the normal layer
}

func DefaultConfig() Config {
	return Config{
		MarginKWh:             10,
		StaleAfterMin:         15,
		MinConfidence:         0.5,
		UnreliablePenaltyFrac: 0.10,
		MaxUnreliableFrac:     0.5,
		SurplusLaxityMin:      120,
		SwapUrgentLaxityMin:   60,
		SwapDonorGapMin:       120,
		LastPlanTTLMin:        10,
		Timeout:               500 * time.Millisecond,
	}
}

// Input is everything the planner may look at. Buses with ArrivalMin > Now are ignored.
type Input struct {
	Now      model.Minute
	Site     model.Site
	Chargers []model.Charger
	Buses    []model.Bus
}

type Layer int

const (
	LayerNormal Layer = iota
	LayerLastValid
	LayerSafe
)

func (l Layer) String() string {
	switch l {
	case LayerNormal:
		return "normal"
	case LayerLastValid:
		return "last-valid"
	case LayerSafe:
		return "safe"
	}
	return "unknown"
}

// Setpoint is the grid-side power commanded to one charger. Chargers not listed are at 0.
type Setpoint struct {
	ChargerID string
	KW        float64
}

// BusStatus explains what will happen to one bus and why.
type BusStatus struct {
	BusID           string
	Assessed        bool // false when the layer does not evaluate the target (safe profile)
	WillReachTarget bool
	ShortfallKWh    float64 // effective kWh missing at departure if WillReachTarget is false
	LaxityMin       float64
	Reason          string
}

// Swap suggests a human move: InBusID takes ChargerID from OutBusID.
type Swap struct {
	ChargerID string
	OutBusID  string
	InBusID   string
	Reason    string
}

type Plan struct {
	Layer     Layer
	Setpoints []Setpoint
	Buses     []BusStatus
	Swaps     []Swap
	Notes     []string
}
