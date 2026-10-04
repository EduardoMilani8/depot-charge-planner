package sim

import (
	"math"
	"sort"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// Controller is anything that turns an observation into a plan.
type Controller interface {
	Plan(in planner.Input) planner.Plan
}

// greedyController serves buses in a fixed order at full power until the power budget runs out.
type greedyController struct {
	less func(a, b model.Bus) bool
}

// NewFIFO serves buses by arrival time (charge on arrival, capped by the site limit).
func NewFIFO() Controller {
	return greedyController{less: func(a, b model.Bus) bool {
		if a.ArrivalMin != b.ArrivalMin {
			return a.ArrivalMin < b.ArrivalMin
		}
		return a.ID < b.ID
	}}
}

// NewEDF serves buses by earliest departure first.
func NewEDF() Controller {
	return greedyController{less: func(a, b model.Bus) bool {
		if a.DepartureMin != b.DepartureMin {
			return a.DepartureMin < b.DepartureMin
		}
		return a.ID < b.ID
	}}
}

func (g greedyController) Plan(in planner.Input) planner.Plan {
	chargers := map[string]model.Charger{}
	for _, c := range in.Chargers {
		chargers[c.ID] = c
	}
	var buses []model.Bus
	for _, b := range in.Buses {
		c, ok := chargers[b.ChargerID]
		if ok && c.Healthy() && b.ArrivalMin <= in.Now && b.SoCKWh < b.TargetKWh {
			buses = append(buses, b)
		}
	}
	sort.SliceStable(buses, func(i, j int) bool { return g.less(buses[i], buses[j]) })
	budget := planner.AvailableKW(in)
	var p planner.Plan
	for _, b := range buses {
		c := chargers[b.ChargerID]
		kw := math.Min(planner.MaxGridKW(c, b), budget)
		if kw < c.MinKW {
			kw = 0
		}
		p.Setpoints = append(p.Setpoints, planner.Setpoint{ChargerID: c.ID, KW: kw})
		budget -= kw
	}
	return p
}

type safeController struct{}

// NewSafeOnly runs only layer 0 (equal split), to measure the fallback on its own.
func NewSafeOnly() Controller { return safeController{} }

func (safeController) Plan(in planner.Input) planner.Plan {
	return planner.Enforce(in, planner.PlanSafe(in))
}

// NewPlannerController wraps the real planner, injecting the scenario's planner faults
// (panic, slowness or invalid output) into its normal layer.
func NewPlannerController(cfg planner.Config, sc Scenario) Controller {
	normal := func(c planner.Config, in planner.Input) planner.Plan {
		garbage := -1
		for _, f := range sc.Faults {
			if !f.Active(in.Now) {
				continue
			}
			switch f.Kind {
			case FaultPlannerPanic:
				panic("injected planner fault")
			case FaultPlannerSlow:
				time.Sleep(2 * c.Timeout)
			case FaultPlannerGarbage:
				garbage = in.Now + int(f.Value)
			}
		}
		p := planner.PlanNormal(c, in)
		if garbage >= 0 {
			p = garbagePlan(p, in, garbage%4)
		}
		return p
	}
	return planner.New(cfg).WithNormal(normal)
}

// garbagePlan corrupts a plan the way a buggy normal layer could, so that the
// verifier (planner.Enforce) has something to correct in simulator runs. Every kind
// breaks at least one invariant whatever the input:
//
//	0: every listed charger (faulted and offline ones too) at twice its ceiling plus
//	   1 kW, and the site limit plus 1 kW on an unknown charger;
//	1: an unknown charger and a NaN setpoint;
//	2: every setpoint listed twice at double power, and an unknown charger;
//	3: +Inf and negative setpoints, and an unknown charger at +Inf.
func garbagePlan(p planner.Plan, in planner.Input, kind int) planner.Plan {
	ghost := planner.Setpoint{ChargerID: "GHOST", KW: in.Site.LimitKW + 1}
	var sps []planner.Setpoint
	switch kind {
	case 0:
		for _, c := range in.Chargers {
			sps = append(sps, planner.Setpoint{ChargerID: c.ID, KW: 2*c.MaxKW + 1})
		}
		sps = append(sps, ghost)
	case 1:
		sps = append(append(sps, p.Setpoints...), ghost, planner.Setpoint{ChargerID: "NAN", KW: math.NaN()})
		if len(p.Setpoints) > 0 {
			sps[0].KW = math.NaN()
		}
	case 2:
		for _, s := range p.Setpoints {
			s.KW *= 2
			sps = append(sps, s, s)
		}
		sps = append(sps, ghost)
	default:
		for i, s := range p.Setpoints {
			if i%2 == 0 {
				s.KW = math.Inf(1)
			} else {
				s.KW = -5
			}
			sps = append(sps, s)
		}
		sps = append(sps, planner.Setpoint{ChargerID: "GHOST", KW: math.Inf(1)})
	}
	p.Setpoints = sps
	return p
}
