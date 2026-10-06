package sim

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// ChargerTrace is one charger's minute-by-minute truth.
type ChargerTrace struct {
	ID          string
	Status      []int // model.ChargerStatus
	CommandedKW []float64
	PhysicalKW  []float64
	BusID       []string // "" when no bus is plugged
}

// BusTrace is one bus's minute-by-minute truth next to what the planner was told.
type BusTrace struct {
	ID        string
	State     []int     // 0 not arrived yet, 1 present, 2 departed
	TrueSoC   []float64 // NaN while the bus is not present
	Observed  []float64 // NaN when not present or when the planner got no usable reading
	ChargerID []string  // "" when waiting
}

// BusOutcome is how a bus ended. Arrival and Departure include late arrival and early
// departure faults.
type BusOutcome struct {
	ID                                                           string
	Arrival, Departure                                           int
	CapacityKWh, InitialSoCKWh, ForecastTargetKWh, TrueTargetKWh float64
	FinalSoCKWh                                                  float64
	Departed, Ready                                              bool
	ShortfallKWh                                                 float64
}

// Decision is a planning cycle that differs from the previous one (see decisionKey).
type Decision struct {
	Minute    int
	Layer     string
	Setpoints []planner.Setpoint
	Buses     []planner.BusStatus
	Swaps     []planner.Swap
	Notes     []string
}

// Trace records the simulator's truth (what the planner never sees) for one run, one
// entry per minute 0..Horizon, plus the decisions that changed.
type Trace struct {
	Limit, Commanded, Physical []float64
	Layer                      []string
	Chargers                   []*ChargerTrace // sorted by ID
	Buses                      []*BusTrace     // sorted by ID
	Outcomes                   []BusOutcome    // sorted by ID, filled when the run ends
	Decisions                  []Decision

	started bool
	lastKey string
}

// NewTrace returns an empty trace to pass to RunTraced.
func NewTrace() *Trace { return &Trace{} }

func (tr *Trace) start(w *World) {
	tr.started = true
	for _, c := range w.chargers { // sorted by ID in newWorld
		tr.Chargers = append(tr.Chargers, &ChargerTrace{ID: c.ID})
	}
	for _, bs := range w.buses { // sorted by ID in newWorld
		tr.Buses = append(tr.Buses, &BusTrace{ID: bs.spec.Bus.ID})
	}
}

// capture records the end of minute w.t (after the physics step, before commands become
// the power in effect for the next step).
func (tr *Trace) capture(w *World, in planner.Input, plan planner.Plan) {
	if !tr.started {
		tr.start(w)
	}
	tr.Limit = append(tr.Limit, w.limitAt(w.t))
	tr.Commanded = append(tr.Commanded, w.commandedTotal())
	tr.Physical = append(tr.Physical, w.physTotal)
	tr.Layer = append(tr.Layer, plan.Layer.String())

	plugged := map[string]string{}
	for _, bs := range w.buses {
		if bs.present && !bs.departed && bs.chargerID != "" {
			plugged[bs.chargerID] = bs.spec.Bus.ID
		}
	}
	for i, c := range w.chargers {
		ct := tr.Chargers[i]
		ct.Status = append(ct.Status, int(c.Status))
		ct.CommandedKW = append(ct.CommandedKW, w.commandedBy(c))
		ct.PhysicalKW = append(ct.PhysicalKW, w.physKW[c.ID])
		ct.BusID = append(ct.BusID, plugged[c.ID])
	}

	observed := map[string]float64{}
	for _, b := range in.Buses {
		if b.SoCConfidence > 0 {
			observed[b.ID] = b.SoCKWh
		}
	}
	for i, bs := range w.buses {
		bt := tr.Buses[i]
		state, soc, obs, ch := 0, math.NaN(), math.NaN(), ""
		switch {
		case bs.departed:
			state = 2
		case bs.present:
			state, soc, ch = 1, bs.soc, bs.chargerID
			if v, ok := observed[bs.spec.Bus.ID]; ok {
				obs = v
			}
		}
		bt.State = append(bt.State, state)
		bt.TrueSoC = append(bt.TrueSoC, soc)
		bt.Observed = append(bt.Observed, obs)
		bt.ChargerID = append(bt.ChargerID, ch)
	}
}

// recordDecision keeps the plan only when it differs from the previous one.
func (tr *Trace) recordDecision(minute int, plan planner.Plan) {
	key := decisionKey(plan)
	if len(tr.Decisions) > 0 && key == tr.lastKey {
		return
	}
	tr.lastKey = key
	tr.Decisions = append(tr.Decisions, Decision{
		Minute:    minute,
		Layer:     plan.Layer.String(),
		Setpoints: append([]planner.Setpoint(nil), plan.Setpoints...),
		Buses:     append([]planner.BusStatus(nil), plan.Buses...),
		Swaps:     append([]planner.Swap(nil), plan.Swaps...),
		Notes:     append([]string(nil), plan.Notes...),
	})
}

var numbers = regexp.MustCompile(`[0-9]+(?:[.,][0-9]+)?`)

// decisionKey identifies "the same decision": the same layer, setpoints (to 1 kW),
// per-bus verdicts and swaps. Numbers inside the reasons are masked, because texts like
// "folga 252 min" change every minute without the decision changing.
func decisionKey(p planner.Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|", p.Layer)
	for _, s := range p.Setpoints {
		fmt.Fprintf(&b, "%s=%.0f;", s.ChargerID, s.KW)
	}
	b.WriteByte('|')
	for _, s := range p.Buses {
		fmt.Fprintf(&b, "%s:%t:%t:%s;", s.BusID, s.Assessed, s.WillReachTarget, numbers.ReplaceAllString(s.Reason, "#"))
	}
	b.WriteByte('|')
	for _, s := range p.Swaps {
		fmt.Fprintf(&b, "%s>%s@%s;", s.OutBusID, s.InBusID, s.ChargerID)
	}
	return b.String()
}

// finish records how every bus ended (call after World.finish).
func (tr *Trace) finish(w *World) {
	for _, bs := range w.buses {
		b := bs.spec.Bus
		ready := bs.departed && bs.soc >= bs.target-1e-6
		short := 0.0
		if bs.departed && !ready {
			short = bs.target - bs.soc
		}
		tr.Outcomes = append(tr.Outcomes, BusOutcome{
			ID: b.ID, Arrival: bs.arrival, Departure: bs.departure,
			CapacityKWh: b.CapacityKWh, InitialSoCKWh: b.SoCKWh,
			ForecastTargetKWh: b.TargetKWh, TrueTargetKWh: bs.target,
			FinalSoCKWh: bs.soc, Departed: bs.departed, Ready: ready, ShortfallKWh: short,
		})
	}
}
