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
// Energy costs more inside the peak window, so a bus that can still finish after the
// peak with comfortable slack does not have to charge during it. While the clock is
// inside the window, PlanNormal calls deferPastPeak, which marks such buses as
// deferred. A deferred bus gets 0 kW, is left out of the allocation and of the
// spare-power pass, and its BusStatus says it WILL reach the target (after the peak)
// with the reason "adiado para depois da ponta: termina com folga de N min". Every
// other bus is allocated by the normal rules with the FULL budget, never less than
// without the policy.
//
// A bus is deferrable when all of these hold:
//   - the policy is valid and tp > 0, where tp is the number of minutes until the peak
//     window ends (0 outside the peak and for a window that never ends);
//   - it needs charge, its charger is OK and usable, its SoC reading is reliable (fresh
//     and confident: isReliable), its numbers are finite and its laxity is not negative;
//   - at full power it still finishes after the peak with the margin:
//     (availMin - tp - DeferMarginMin) * maxKW / 60 >= gridNeed.
//
// Joint feasibility. The deferrable buses must also fit together after the peak: they
// are tested with admit (Moore-Hodgson on the site budget) as if they all started when
// the peak ends, i.e. with availMin - tp - DeferMarginMin minutes. The largest set that
// fits is deferred; the rest is charged now as usual. This is the brief's rule. Measured
// on the simulator it is NOT enough to keep readiness (see "Why more than the plain
// rule" below), so three things the plain rule does not know are added to the test:
//
//   - the connected buses that are not deferred keep charging after the peak and compete
//     for the budget (a second admit over them and the deferred ones from now on);
//   - buses present but waiting for a charger, or on a faulted one, will need the budget
//     after the peak as well (they count as starting when the peak ends);
//   - chargers that are free now will probably get a bus before the peak ends: each
//     free healthy charger counts as one more bus needing the mean grid energy of the
//     buses present, due at their mean deadline.
//
// If these extra buses do not all fit, the deferrable buses are dropped from the
// deferred set, largest grid energy first, until they do (and nobody is deferred if
// they still do not). The test uses only deferBudgetFrac of the budget: the rest is the
// reserve for faults, SoC noise and arrivals the planner has not seen.
//
// The planner re-plans every minute, so the test is re-evaluated each time with the
// remaining time: the minute the peak ends tp is 0 and every deferred bus starts at
// once, and the margin keeps enough slack for it to finish. Never deferred: a bus with
// an unreliable reading, on a faulted or offline charger, waiting for a charger, at its
// target, or that cannot finish anyway.
//
// Why more than the plain rule. With the plain rule alone, the simulator (arrivals
// during the peak, see sim's cost property test) lost up to 24 points of ready% at 1200
// kW and 8-14 points at 600 kW: the buses present at the start of the peak look
// feasible, so they are all deferred, the budget goes unused for hours, and the buses
// that arrive later then no longer fit. The reserve above removes that loss in the
// profiles without noisy data (none, mild) at every limit tested.
//
// Limits, honestly. This is a heuristic on the buses seen so far, not a guarantee.
//   - Under SoC noise or a wrong consumption forecast, a bus charged early keeps being
//     topped up whenever a noisy reading dips below the target, which gives it energy
//     beyond the plan; a deferred bus has less time for that. In the severe and random
//     fault profiles this costs a bus here and there, seed by seed, even though the
//     deferral itself is feasible (it disappears when noise and consumption faults are
//     removed from the scenario).
//   - Anything that goes wrong after the peak (charger failures, limit drops, sensors
//     freezing) now hits buses that have not charged yet; the reserve and the margin
//     absorb small events, not a total loss of capacity.
//   - When the depot is overloaded (the extra buses above do not fit), nothing is
//     deferred and the plan is the plain one: no saving, no loss.
//
// Safety does not depend on this file: a deferred plan only lowers power, and the
// last-valid layer, the safe layer and Enforce still check every plan against the
// site limit.

const (
	// defaultDeferMarginMin is CostPolicy.DeferMarginMin when it is 0.
	defaultDeferMarginMin = 45
	// deferBudgetFrac is the share of the site budget the post-peak feasibility test
	// may count on: the plan keeps the rest as a reserve (see the comment at the top).
	deferBudgetFrac = 0.35
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
	margin := p.margin()
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
		if cd.need > 0 && !cd.unusable && cd.laxityMin >= 0 && finite(cd.gridNeed) {
			known = append(known, cd)
		}
	}
	ref, haveRef := bestCharger(in)
	if haveRef {
		for _, ib := range idles {
			c := newCandidate(cfg, in, ib.bus, ref)
			if c.need <= 0 || c.unusable || c.laxityMin < 0 || !finite(c.gridNeed) {
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
	late = append(late, expectedBuses(in, known, tp, margin)...)
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
		case cd.need > 0 && !cd.unusable && cd.laxityMin >= 0 && finite(cd.gridNeed):
			c := *cd
			rest = append(rest, &c)
		}
	}
	for _, c := range fitAround(budget, rest, optional) {
		o := shadow[c]
		o.deferred = true
		o.postPeakLaxityMin = o.laxityMin - tp
	}
}

// deferrableNow is the per-bus part of the rule (see the comment at the top).
func deferrableNow(cd *candidate, tp, margin float64) bool {
	if cd.need <= 0 || !cd.reliable || cd.unusable || cd.laxityMin < 0 || !cd.charger.Healthy() {
		return false
	}
	if !finite(cd.gridNeed) || cd.gridNeed <= 0 || !finite(cd.maxKW) || !finite(cd.availMin) {
		return false
	}
	return (cd.availMin-tp-margin)*cd.maxKW/60 >= cd.gridNeed
}

// expectedBuses returns one placeholder per healthy charger that has no bus now, each
// needing the mean grid energy of the known buses and due at their mean deadline,
// counted from the end of the peak like the waiting buses.
func expectedBuses(in Input, known []*candidate, tp, margin float64) []*candidate {
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
