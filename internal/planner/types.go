// Package planner decides how much power each charger delivers to each bus.
package planner

import (
	"math"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// Config holds the planner tunables.
type Config struct {
	MarginKWh             float64 // extra battery energy planned on top of the route target
	StaleAfterMin         int     // SoC readings older than this are unreliable
	MinConfidence         float64 // SoC readings below this confidence are unreliable
	UnreliablePenaltyFrac float64 // fraction of capacity subtracted from unreliable SoC
	MaxUnreliableFrac     float64 // above this fraction of unreliable buses, use the safe profile
	// SurplusLaxityMin: spare power only goes to buses with less laxity than this.
	// The default (math.MaxFloat64) spends all spare power on every connected bus in
	// laxity order (spec §6.4: readiness before peak flattening). A finite value, e.g.
	// 120, flattens the peak by keeping buses with more laxity at just-in-time power;
	// 0 gives spare power only to buses that cannot finish (negative laxity).
	SurplusLaxityMin    float64
	SwapUrgentLaxityMin float64       // waiting buses with less laxity than this get swap suggestions
	SwapDonorGapMin     float64       // donor bus must have at least this much more laxity
	SwapMoveMin         int           // minutes a recommended swap takes before the incoming bus charges
	LastPlanTTLMin      int           // how long the last valid plan may be reused
	Timeout             time.Duration // max time for the normal layer
}

func DefaultConfig() Config {
	return Config{
		MarginKWh:             10,
		StaleAfterMin:         15,
		MinConfidence:         0.5,
		UnreliablePenaltyFrac: 0.10,
		MaxUnreliableFrac:     0.5,
		SurplusLaxityMin:      math.MaxFloat64,
		SwapUrgentLaxityMin:   60,
		SwapDonorGapMin:       120,
		SwapMoveMin:           5,
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

// Setpoint is the grid-side power commanded to one charger.
//
// A Plan lists only the chargers it powers (and possibly some at 0 kW): every charger
// NOT listed in Plan.Setpoints must be at 0 kW. Whoever applies a plan (the simulator
// today, the future OCPP adapter) must therefore switch off unlisted chargers, e.g. by
// clearing any charging profile left over from a previous plan, never keep their old power.
type Setpoint struct {
	ChargerID string
	KW        float64
}

// BusStatus explains what will happen to one bus and why.
type BusStatus struct {
	BusID           string
	Assessed        bool // false when the layer does not evaluate the target (safe profile)
	WillReachTarget bool
	// ShortfallKWh is the battery energy predicted to be missing at departure when
	// WillReachTarget is false, measured against the margin-adjusted target. For a
	// connected bus it is EFFECTIVE energy (the part above the taper knee is weighted
	// by model.PlannerTailCostFactor), so it can be larger than the real kWh missing;
	// for a bus without a usable charger it is the plain kWh missing. It is not the
	// same unit as sim.Metrics.ShortfallKWh, which is real kWh against the true route need.
	ShortfallKWh float64
	LaxityMin    float64
	Reason       string
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
