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
// While the clock is inside the peak window (tp = minutes until it ends), PlanNormal
// calls deferPastPeak, which marks some buses as deferred. A deferred bus gets 0 kW, is
// left out of the allocation and of the spare-power pass, and its BusStatus says it WILL
// reach the target after the peak ("adiado para depois da ponta: termina com folga de N
// min"). Every other bus is allocated as usual with the FULL budget. Outside the peak,
// or with an invalid policy, nothing changes.
//
// A bus is deferrable when it needs charge, its SoC reading is reliable (isReliable),
// its charger is OK and usable, its numbers are finite, its laxity is not negative and
//
//	(availMin - tp - DeferMarginMin) * maxKW / 60 >= gridNeed
//
// i.e. at full power it still finishes after the peak with the margin. The deferrable
// buses must also fit together after the peak: admit (Moore-Hodgson) on the budget, with
// availMin - tp - margin minutes each; the largest set that fits is deferred, the rest
// charges now. That is the brief's rule. In the simulator it alone lost up to 24 points
// of ready% (50 buses, 25 chargers, arrivals all inside the peak), because the buses
// present at the start of the peak look feasible, all wait, and the buses that arrive
// later no longer fit. So the deferral is also subject to:
//
//   - budget reserve: the connected buses that stay on charge, the waiting buses and one
//     expected bus per free charger (the mean grid energy and deadline of the buses
//     present) must fit after the peak with the deferred ones, using only
//     deferBudgetFrac of the budget; the optional (deferred) buses with the most grid
//     energy are dropped first, and if the others alone do not fit nothing is deferred;
//   - plug turnover (protectQueue): a deferred bus keeps its plug without drawing power,
//     and a waiting bus, or one scheduled to arrive (Input.Buses with ArrivalMin > Now),
//     starts only when a plug frees. If deferring makes such a bus start so late that it
//     can no longer finish with the margin, while it could without the deferral, the
//     deferred buses are released, earliest finisher first, until it can.
//
// The planner re-plans every minute, so all of this is re-evaluated with the time left;
// when the peak ends tp is 0 and every deferred bus starts at once.
//
// Measured on the simulator (internal/sim/cost_property_test.go): with fault-free or
// mild data readiness is never lower than without the policy on any tested seed, for the
// geometries listed there. deferBudgetFrac is empirical, tuned on those geometries, not
// derived; the plug-turnover model is optimistic (full-power finish times, no budget
// sharing). Readiness can still drop where the planner cannot know the future: SoC noise
// and consumption faults (a bus charged early keeps being topped up by noisy readings; a
// deferred one has had less time), and anything that fails after the peak (sensors
// freezing, chargers failing) hits energy not yet delivered. When the depot is overloaded
// nothing is deferred.
//
// Safety does not depend on this file: a deferred plan only lowers power, and the
// last-valid layer, the safe layer and Enforce still check every plan against the limit.

const (
	// defaultDeferMarginMin is CostPolicy.DeferMarginMin when it is 0.
	defaultDeferMarginMin = 45
	// deferBudgetFrac is the share of the site budget the post-peak feasibility test
	// may count on: the plan keeps the rest as a reserve (see the comment at the top).
	deferBudgetFracBlind = 0.35
	deferBudgetFracKnown = 0.6
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

// budgetFrac is the share of the budget the post-peak test may count on.
func (p CostPolicy) budgetFrac() float64 {
	if p.ScheduleKnown {
		return deferBudgetFracKnown
	}
	return deferBudgetFracBlind
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
	margin := p.margin()
	fullBudget := budget
	budget *= p.budgetFrac()

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
	if !p.ScheduleKnown {
		late = append(late, expectedBuses(in, known, len(future), tp, margin)...)
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
	protectQueue(cfg, in, tp, margin, cands, idles)
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

// expectedBuses returns one placeholder per healthy charger that has no bus now and is
// not taken by one of the announced buses (scheduled arrivals), each
// needing the mean grid energy of the known buses and due at their mean deadline,
// counted from the end of the peak like the waiting buses.
func expectedBuses(in Input, known []*candidate, announced int, tp, margin float64) []*candidate {
	if len(known) == 0 {
		return nil
	}
	occupied := map[string]bool{}
	for _, b := range presentBuses(in) {
		if b.ChargerID != "" {
			occupied[b.ChargerID] = true
		}
	}
	need, avail := 0.0, 0.0
	for _, k := range known {
		need += k.gridNeed
		avail += k.availMin
	}
	need /= float64(len(known))
	avail = avail/float64(len(known)) - tp - margin
	if avail <= 0 {
		return nil
	}
	var out []*candidate
	for _, c := range in.Chargers {
		if c.Healthy() && !occupied[c.ID] {
			if announced > 0 { // the announced buses take the free chargers first
				announced--
				continue
			}
			out = append(out, &candidate{bus: model.Bus{ID: "~esperado-" + c.ID}, gridNeed: need, availMin: avail})
		}
	}
	return out
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
// its schedule): it will need a plug ready minutes from now.
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

// queued is a bus that needs a plug it does not have yet: waiting now (ready 0) or
// scheduled to arrive (ready = minutes until arrival).
type queued struct {
	id    string
	ready float64 // minutes from now until it can take a plug
	avail float64 // minutes from now to its departure
	dur   float64 // minutes to charge at full power
}

// protectQueue releases deferred buses whose plug a queued bus needs (see the comment
// at the top). Plug availability is estimated optimistically: a free plug now, a bus
// at its target as soon as it is asked to give way, any other bus when it would finish
// at full power, and a deferred bus no earlier than tp plus that. Queued buses take the
// earliest plug in order of deadline. A queued bus is protected if it can finish when
// nothing is deferred; if deferring delays its start and leaves it less than the margin
// of slack, the deferred bus that would finish first (ties: ID) goes back to charging.
func protectQueue(cfg Config, in Input, tp, margin float64, cands []*candidate, idles []idleBus) {
	ref, ok := bestCharger(in)
	if !ok {
		return
	}
	var q []queued
	add := func(b model.Bus, ready float64) {
		c := newCandidate(cfg, in, b, ref)
		if c.need <= 0 || c.unusable || !finite(c.gridNeed) || !finite(c.availMin) || c.maxKW <= 0 {
			return
		}
		q = append(q, queued{id: b.ID, ready: ready, avail: c.availMin, dur: c.gridNeed / c.maxKW * 60})
	}
	for _, ib := range idles {
		add(ib.bus, 0)
	}
	for _, f := range scheduledBuses(cfg, in, ref) {
		q = append(q, queued{id: f.cand.bus.ID, ready: f.ready, avail: f.cand.availMin, dur: f.cand.gridNeed / f.cand.maxKW * 60})
	}
	if len(q) == 0 {
		return
	}
	sort.SliceStable(q, func(i, j int) bool {
		if q[i].avail != q[j].avail {
			return q[i].avail < q[j].avail
		}
		return q[i].id < q[j].id
	})
	occupied := map[string]bool{}
	for _, b := range presentBuses(in) {
		if b.ChargerID != "" {
			occupied[b.ChargerID] = true
		}
	}
	free := 0
	for _, c := range in.Chargers {
		if c.Healthy() && !occupied[c.ID] {
			free++
		}
	}
	// finish is when the bus's plug frees, with or without its deferral.
	finish := func(cd *candidate, deferred bool) float64 {
		switch {
		case cd.need <= 0:
			return 0
		case cd.unusable || cd.maxKW <= 0 || !finite(cd.gridNeed):
			return math.Inf(1)
		case deferred:
			return tp + cd.gridNeed/cd.maxKW*60
		}
		return cd.gridNeed / cd.maxKW * 60
	}
	starts := func(withDeferral bool) []float64 {
		plugs := make([]float64, 0, free+len(cands))
		for i := 0; i < free; i++ {
			plugs = append(plugs, 0)
		}
		for _, cd := range cands {
			plugs = append(plugs, finish(cd, withDeferral && cd.deferred))
		}
		out := make([]float64, len(q))
		for k, e := range q {
			best := -1
			for i, t := range plugs {
				if best < 0 || t < plugs[best] {
					best = i
				}
			}
			if best < 0 {
				out[k] = math.Inf(1)
				continue
			}
			out[k] = math.Max(e.ready, plugs[best])
			plugs[best] = out[k] + e.dur
		}
		return out
	}
	base := starts(false)
	for {
		cur := starts(true)
		violated := false
		for k, e := range q {
			protected := base[k]+e.dur <= e.avail+1e-9
			if protected && cur[k] > base[k]+1e-9 && e.avail-cur[k]-margin < e.dur {
				violated = true
				break
			}
		}
		if !violated {
			return
		}
		var first *candidate
		for _, cd := range cands {
			if !cd.deferred {
				continue
			}
			if first == nil || finish(cd, true) < finish(first, true) ||
				(finish(cd, true) == finish(first, true) && cd.bus.ID < first.bus.ID) {
				first = cd
			}
		}
		if first == nil {
			return
		}
		first.deferred = false
	}
}
