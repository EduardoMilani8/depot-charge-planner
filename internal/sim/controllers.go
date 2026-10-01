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
// (panic or slowness) into its normal layer.
func NewPlannerController(cfg planner.Config, sc Scenario) Controller {
	normal := func(c planner.Config, in planner.Input) planner.Plan {
		for _, f := range sc.Faults {
			if !f.Active(in.Now) {
				continue
			}
			switch f.Kind {
			case FaultPlannerPanic:
				panic("injected planner fault")
			case FaultPlannerSlow:
				time.Sleep(2 * c.Timeout)
			}
		}
		return planner.PlanNormal(c, in)
	}
	return planner.New(cfg).WithNormal(normal)
}
