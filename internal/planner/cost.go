package planner

import (
	"fmt"
	"math"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/billing"
	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// Cost-aware planning (Config.Cost): defer charging past the peak window.
//
// What it does. While the clock is inside the peak window (tp = minutes until it ends),
// PlanNormal calls deferPastPeak, which marks some buses as deferred. A deferred bus gets
// 0 kW, is left out of the allocation and of the spare-power pass, and its BusStatus says
// it WILL reach the target after the peak ("adiado para depois da ponta: termina com
// folga de N min"). Every other bus is allocated as usual with the FULL budget. Outside
// the peak, or with an invalid policy, nothing changes. The planner re-plans every minute,
// so everything below is re-evaluated with the time left; when the peak ends tp is 0 and
// every deferred bus starts at once.
//
// When it is allowed. Only with the schedule known (CostPolicy.ScheduleKnown: Input.Buses
// lists the buses still to arrive, ArrivalMin > Now) and only when plugs are not scarce:
// buses present + buses announced <= healthy chargers. Then every bus can have its own
// plug, so a deferred bus, which keeps its plug without drawing power, cannot keep another
// bus off a charger. If a charger fails and the condition stops holding, deferral stops and
// every bus is planned plainly.
//
// Without the schedule (ScheduleKnown false) the policy does nothing. The planner cannot
// tell then whether the fleet fits the plugs, and guessing it failed: with more buses than
// chargers the buses that arrive later found the plugs held by deferred ones and were lost
// (40 buses on 30 chargers of 50 kW, 80 on 72, 12 on 8, 4 on 3, among others), whatever
// reserve of free chargers was required. Likewise in depots with fewer plugs than buses
// (buses are swapped between chargers, the project's swap scenario) the policy does nothing
// even with the schedule: with them, deferring lost buses even with clean data and power
// not binding, and the loss did not follow the buses/chargers ratio, so no tuning of the
// rest was trusted.
//
// Which buses. A bus is deferrable when it needs charge, its SoC reading is reliable
// (isReliable), its charger is OK and usable, its numbers are finite, its laxity is not
// negative and
//
//	(availMin - tp - DeferMarginMin) * maxKW / 60 >= gridNeed
//
// i.e. at full power it still finishes after the peak with the margin. The deferrable
// buses must also fit together after the peak: admit (Moore-Hodgson) on the budget, with
// availMin - tp - margin minutes each; the largest set that fits is deferred, the rest
// charges now. On its own that rule lost up to 24 points of ready% in the simulator (the
// buses present when the peak starts look feasible, all wait, and the later arrivals no
// longer fit), so the same test also counts what stays on charge and the buses still to
// arrive, and only a fraction of the budget counts:
//   - the connected buses that stay on charge, the buses waiting for a charger and the
//     announced ones (window from their arrival) must fit after the peak together with the
//     deferred ones; the deferred bus needing the most grid energy is dropped first, and
//     if the others alone do not fit nothing is deferred;
//   - only deferBudgetFrac (0.6) of the budget counts. The constant is empirical: 0.7 lost
//     a bus on one geometry of the earlier tests;
//   - saturation: nothing is deferred when the buses that stay on charge could absorb the
//     whole budget anyway. The commanded peak power is then the same, and deferring only
//     reorders: measured, tapering buses no longer took part of the budget and the peak
//     energy rose 1-2%.
//
// What was measured (internal/sim/cost_property_test.go, COST_SWEEP=1 for the wide random
// sweep): with fault-free or mild data the ready% with the policy was never lower than
// without it on any seed; the energy cost was never more than 0.2% higher (tolerance, not
// "never higher": a deferral that ends inside the peak can move a little energy into it,
// up to about 0.14% was seen). Under severe or random faults this does NOT hold per seed:
// SoC noise and consumption faults (a bus charged early keeps being topped up by noisy
// readings, a deferred one has had less time) and failures after the peak (sensors
// freezing, chargers failing) hit energy not yet delivered. Those profiles have pinned
// bounds in the tests, not a guarantee.
//
// With the schedule announced (sim.Scenario.AnnounceArrivals, which sim.ForController turns
// on for a planner with a cost policy) every Input the simulator hands out, and so every
// record of the decision log, also carries the buses that have not arrived yet.
//
// Safety does not depend on this file: a deferred plan only lowers power, and the
// last-valid layer, the safe layer and Enforce still check every plan against the limit.

const (
	// defaultDeferMarginMin is CostPolicy.DeferMarginMin when it is 0.
	defaultDeferMarginMin = 45
	// Share of the site budget the post-peak feasibility test may count on; the rest is
	// the reserve for faults and noise (see the comment at the top).
	deferBudgetFrac = 0.6
)

// validate reports why the policy cannot be used, or nil.
func (p CostPolicy) validate() error {
	switch {
	case p.PeakFromMin < 0 || p.PeakFromMin > 1440 || p.PeakToMin < 0 || p.PeakToMin > 1440:
		return fmt.Errorf("janela de ponta fora de 0..1440 (%d..%d)", p.PeakFromMin, p.PeakToMin)
	case !finite(p.DeferMarginMin) || p.DeferMarginMin < 0:
		return fmt.Errorf("folga de adiamento inválida (%v)", p.DeferMarginMin)
	case !finite(p.DemandTargetKW) || p.DemandTargetKW < 0:
		return fmt.Errorf("alvo de demanda inválido (%v)", p.DemandTargetKW)
	}
	return nil
}

func (p CostPolicy) margin() float64 {
	if p.DeferMarginMin == 0 {
		return defaultDeferMarginMin
	}
	return p.DeferMarginMin
}

// peakEndsIn returns the minutes until the peak window ends if input minute now is
// inside it, else 0. Also 0 when the window never ends (it covers the whole day).
func (p CostPolicy) peakEndsIn(now int) int {
	t := billing.Tariff{PeakFromMin: p.PeakFromMin, PeakToMin: p.PeakToMin}
	clock := ((p.ClockAtZeroMin % 1440) + (now % 1440) + 2880) % 1440
	if !t.InPeak(clock) {
		return 0
	}
	tp := (p.PeakToMin%1440 - clock + 1440) % 1440
	if tp == 0 || t.InPeak(clock+tp) { // a window that covers the whole day never ends
		return 0
	}
	return tp
}

// costPolicy returns the policy to apply: nil when Config.Cost is nil or invalid. For
// an invalid policy the note explains why it is ignored.
func (cfg Config) costPolicy() (*CostPolicy, string) {
	if cfg.Cost == nil {
		return nil, ""
	}
	if err := cfg.Cost.validate(); err != nil {
		return nil, "política de custo ignorada: " + err.Error()
	}
	return cfg.Cost, ""
}

// deferPastPeak marks in cands the buses to defer (see the comment at the top). idles
// are the present buses that cannot charge now.
func deferPastPeak(cfg Config, p CostPolicy, in Input, budget float64, cands []*candidate, idles []idleBus) {
	tp := float64(p.peakEndsIn(in.Now))
	if tp <= 0 {
		return
	}
	if !p.ScheduleKnown || !plugsNotScarce(in) {
		return
	}
	margin := p.margin()
	fullBudget := budget
	budget *= deferBudgetFrac

	// Deferrable buses, as they look from the end of the peak.
	var deferrable []*candidate
	shadow := map[*candidate]*candidate{} // shadow -> original
	for _, cd := range cands {
		if !deferrableNow(cd, tp, margin) {
			continue
		}
		sh := *cd
		sh.availMin = cd.availMin - tp - margin
		shadow[&sh] = cd
		deferrable = append(deferrable, &sh)
	}
	if len(deferrable) == 0 {
		return
	}

	// Buses that cannot start before the peak ends: waiting ones and expected ones.
	var known, late []*candidate
	for _, cd := range cands {
		if needsCharge(cd) {
			known = append(known, cd)
		}
	}
	ref, haveRef := bestCharger(in)
	var future []scheduled
	if haveRef {
		future = scheduledBuses(cfg, in, ref)
		for _, f := range future {
			c := f.cand
			known = append(known, &c)
			l := f.cand
			l.availMin = f.cand.availMin - math.Max(tp, f.ready) - margin
			if l.availMin > 0 {
				late = append(late, &l)
			}
		}
		for _, ib := range idles {
			c := newCandidate(cfg, in, ib.bus, ref)
			if !needsCharge(&c) {
				continue
			}
			known = append(known, &c)
			l := c
			l.availMin = c.availMin - tp - margin
			if l.availMin > 0 { // otherwise it cannot use the budget after the peak at all
				late = append(late, &l)
			}
		}
	}
	kept := fitAround(budget, late, deferrable)
	if len(kept) == 0 {
		return
	}

	// The connected buses that stay on charge now still need the budget after the peak.
	chosen := map[*candidate]bool{}
	for _, sh := range kept {
		chosen[shadow[sh]] = true
	}
	var rest, optional []*candidate
	for _, cd := range cands {
		switch {
		case chosen[cd]:
			c := *cd
			c.availMin = cd.availMin - margin
			shadow[&c] = cd
			optional = append(optional, &c)
		case needsCharge(cd):
			c := *cd
			rest = append(rest, &c)
		}
	}
	for _, c := range fitAround(budget, rest, optional) {
		shadow[c].deferred = true
	}
	// Deferring only pays if it lowers the power drawn in the peak. If the buses that
	// stay on charge can absorb the whole budget anyway, the peak energy is the same and
	// deferring merely reorders (measured: a little more energy in the peak, from tapering
	// buses no longer taking part of the budget), so nothing is deferred.
	absorb := 0.0
	for _, cd := range cands {
		if !cd.deferred && cd.need > 0 && !cd.unusable && finite(cd.maxKW) {
			absorb += cd.maxKW
		}
	}
	if absorb >= fullBudget {
		for _, cd := range cands {
			cd.deferred = false
		}
		return
	}
	for _, cd := range cands {
		if cd.deferred {
			cd.postPeakLaxityMin = cd.laxityMin - tp
		}
	}
}

// needsCharge reports whether a bus still needs charge and can be saved: it counts
// against the post-peak budget.
func needsCharge(cd *candidate) bool {
	return cd.need > 0 && !cd.unusable && cd.laxityMin >= 0 && finite(cd.gridNeed)
}

// deferrableNow is the per-bus part of the rule (see the comment at the top).
func deferrableNow(cd *candidate, tp, margin float64) bool {
	if !needsCharge(cd) || !cd.reliable {
		return false
	}
	if cd.gridNeed <= 0 || !finite(cd.maxKW) || !finite(cd.availMin) {
		return false
	}
	return (cd.availMin-tp-margin)*cd.maxKW/60 >= cd.gridNeed
}

// fitAround returns the part of optional that fits together with all of must under
// the budget (cumulative test of admit). While a must bus is left out, the optional
// bus needing the most grid energy (ties: larger ID) is dropped and the test repeated.
// If must alone does not fit, nothing is kept.
func fitAround(budget float64, must, optional []*candidate) []*candidate {
	isMust := make(map[*candidate]bool, len(must))
	for _, m := range must {
		isMust[m] = true
	}
	opt := append([]*candidate(nil), optional...)
	sort.SliceStable(opt, func(i, j int) bool { // drop order: last first
		if opt[i].gridNeed != opt[j].gridNeed {
			return opt[i].gridNeed < opt[j].gridNeed
		}
		return opt[i].bus.ID < opt[j].bus.ID
	})
	for len(opt) > 0 {
		all := append(append([]*candidate(nil), must...), opt...)
		admitted, left := admit(budget, all)
		blocked := false
		for _, l := range left {
			if isMust[l] {
				blocked = true
				break
			}
		}
		if !blocked {
			var out []*candidate
			for _, a := range admitted {
				if !isMust[a] {
					out = append(out, a)
				}
			}
			return out
		}
		opt = opt[:len(opt)-1]
	}
	return nil
}

// deferredStatus is the status of a deferred bus: it charges after the peak.
func (cd *candidate) deferredStatus() BusStatus {
	return BusStatus{
		BusID: cd.bus.ID, Assessed: true, WillReachTarget: true, LaxityMin: finiteLaxity(cd.laxityMin),
		Reason: fmt.Sprintf("adiado para depois da ponta: termina com folga de %.0f min", math.Max(0, cd.postPeakLaxityMin)),
	}
}

// scheduled is a bus announced in Input.Buses with ArrivalMin > Now (the depot knows
// its schedule) that will need charge, arriving ready minutes from now.
type scheduled struct {
	cand  candidate
	ready float64
}

// scheduledBuses returns the announced buses that will need charge, as candidates on the
// reference charger.
func scheduledBuses(cfg Config, in Input, ref model.Charger) []scheduled {
	var out []scheduled
	for _, b := range in.Buses {
		if b.ArrivalMin <= in.Now || busProblem(b) != "" {
			continue
		}
		c := newCandidate(cfg, in, b, ref)
		if c.need <= 0 || c.unusable || !finite(c.gridNeed) || !finite(c.availMin) || c.maxKW <= 0 {
			continue
		}
		out = append(out, scheduled{cand: c, ready: float64(b.ArrivalMin - in.Now)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].cand.bus.ID < out[j].cand.bus.ID })
	return out
}

// plugsNotScarce is the condition under which deferral is allowed (see the comment at the
// top): every bus present or announced can have its own healthy charger.
func plugsNotScarce(in Input) bool {
	healthy := 0
	for _, c := range in.Chargers {
		if c.Healthy() {
			healthy++
		}
	}
	buses := len(presentBuses(in))
	for _, b := range in.Buses {
		if b.ArrivalMin > in.Now && b.DepartureMin > in.Now && busProblem(b) == "" {
			buses++
		}
	}
	return buses <= healthy
}
