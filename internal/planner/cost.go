package planner

import (
	"fmt"
	"math"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/billing"
	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// Cost-aware planning (Config.Cost): defer charging past the peak window and, as a second
// lever, cap the grid demand at a target (see "Demand cap" below).
//
// What it does. While the clock is inside the peak window (tp = minutes until it ends),
// PlanNormal calls deferPastPeak, which marks some buses as deferred. A deferred bus gets
// 0 kW, is left out of the allocation and of the spare-power pass, and its BusStatus says
// it WILL reach the target after the peak ("adiado para depois da ponta: termina com
// folga de N min"). Every other bus is allocated as usual with the whole budget (the site
// limit, or the demand target when the cap is on). Outside the peak, or with an invalid
// policy, nothing changes. The planner re-plans every minute,
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
// Demand cap (CostPolicy.DemandTargetKW > 0). The second lever: PlanNormal plans with the
// budget min(AvailableKW, DemandTargetKW) instead of the full budget, so the grid demand
// (the largest 15-minute block on the bill) stays near the target. The full limit stays
// what Enforce checks, and the deferral works on the budget the cap chose. The cap does
// not need plugs to be plentiful (it never keeps a bus off a charger) and works with and
// without the schedule, but it only holds when readiness is not at stake. capDemand drops
// it for the full budget, with a note in Plan.Notes, when:
//   - a bus waiting for a charger could still be saved (with the schedule: only one with
//     less than 60 min of laxity). Slower charging keeps the plugs busy longer, so the
//     waiting bus would start later; measured, this cost up to 4 points of ready% in
//     depots with three buses per charger;
//   - some bus on charge has an unreliable SoC reading (its need is a guess);
//   - without the schedule, a healthy charger is still free (the fleet may still grow);
//   - the buses that need charge (connected, waiting and, with the schedule, announced)
//     could not all finish if the site delivered 85% of the target all the time (70%
//     without the schedule): fleetFits, a cumulative energy test per release time and
//     deadline. A target the night's energy cannot fit would only delay the energy, and
//     the surge to catch up costs more than the cap saved;
//   - the capped plan leaves some bus short that the full budget saves or just gives more
//     power (readiness first, the check of the brief, widened to buses the planner thinks
//     cannot finish, which the simulator still finishes with the leftover power the cap
//     would take away).
//
// What was measured (internal/sim/demand_property_test.go; the baseline is the same cost
// policy without a target). Asserted in the tests, with the schedule announced: on every
// seed of the 18 geometries tested (the project's 50 buses/25 chargers at 600, 1200 and
// 2000 kW with targets at 50% and 80% of the limit, and depots with a plug for every
// bus), profiles none and mild, ready% never lower and the demand never higher. Measured
// once with the env-gated TestDemandCapSweep (COST_CAP_SWEEP=1, not in CI), on two
// random samples (240 cells of 8 seeds, and 200 cells of 6 seeds from another random
// sample): with the schedule ready% was never lower, and the demand was higher than the
// baseline on 5 of 3,120 seeds (up to 2% of the limit). The demand falls only where the
// target is feasible (50 buses/25 chargers at 2000 kW with the target at 1600: -20%); a
// target the night's energy cannot fit changes nothing. Without the schedule the cap is
// weaker (it waits for every plug to be taken) and in depots with several buses per
// charger it can still lose a bus: pinned bounds, at most 4 points on a seed and 6 of 8
// seeds in a cell. Under severe or random faults the property does not hold per seed
// either (bounds in the test).
//
// The cap is not a bill saver by itself. Lowering the demand can RAISE the monthly bill
// when it pushes energy into the peak: with the deferral on, a target below the load the
// deferral concentrates after the peak makes buses charge during the peak. Measured over
// 8 nights, 30 buses/30 chargers at 1500 kW, target 750: energy 5816 -> 9533 R$/night and
// the estimated month 204,472 -> 330,975 R$ (+62%) against the deferral alone (still below
// the plain planner's 467,435); 50 buses/50 chargers at 2000 kW, target 1600: demand -20%
// but the month 415,016 -> 541,330 R$ (+30%); demand prices 40/20 R$/kW, illustrative.
// Set the target with the bill in view. And a lower peak brings no saving when a demand
// contract floor already exceeds the measured demand (billing charges the contracted
// demand up to 1.05 times it).
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

// Demand cap notes (Plan.Notes).
const (
	noteDemandCapped     = "demanda limitada ao alvo"
	noteDemandNoFit      = "alvo de demanda não cabe: usando o limite"
	noteDemandWaiting    = "alvo de demanda em espera: há ônibus aguardando carregador"
	noteDemandUnreliable = "alvo de demanda em espera: leitura de SoC não confiável"
	noteDemandUnknown    = "alvo de demanda em espera: agenda desconhecida e há carregador livre"
)

// capDemand applies CostPolicy.DemandTargetKW (see the comment at the top). full is the
// budget without the cap (the limit minus what offline chargers keep drawing). It returns
// the budget PlanNormal must plan with, which is min(full, target) unless that would
// leave a bus short that the full budget saves, and the note to add ("" when the cap is
// off, does not bind or nobody charges). The cap is independent of the deferral: it needs
// neither the schedule nor a plug for every bus, because it never keeps a bus off a plug.
func capDemand(cfg Config, p CostPolicy, in Input, full float64, cands []*candidate, idles []idleBus) (float64, string) {
	target := p.DemandTargetKW
	if target <= 0 || target >= full {
		return full, ""
	}
	for _, cd := range cands {
		if cd.need > 0 && !cd.unusable && !cd.reliable {
			return full, noteDemandUnreliable // the needs are guesses: readiness first
		}
	}
	urgent := math.Inf(1)
	if p.ScheduleKnown {
		urgent = demandWaitUrgentMin
	}
	if waitingSavable(cfg, in, idles, urgent) {
		return full, noteDemandWaiting
	}
	if !p.ScheduleKnown && freeHealthyChargers(in) > 0 {
		return full, noteDemandUnknown
	}
	frac := demandCapFrac
	if !p.ScheduleKnown {
		frac = demandCapFracBlind
	}
	if !fleetFits(cfg, p, in, target*frac, cands, idles) {
		return full, noteDemandNoFit
	}
	capped := trialPlan(cfg, p, in, target, cands, idles)
	shorts := shortBuses(capped)
	if len(shorts) == 0 {
		for _, cd := range capped {
			if cd.allocKW > 0 {
				return target, noteDemandCapped
			}
		}
		return target, ""
	}
	// The capped plan leaves some bus short. Is it the cap's fault? It is when the full
	// budget saves the bus, or just gives it more power: a bus the planner thinks cannot
	// finish (negative laxity, conservative taper model) still finishes in the simulator
	// when it gets the leftover power, which the cap would take away. A bus that gets the
	// same power either way is not the cap's business.
	fullPlan := trialPlan(cfg, p, in, full, cands, idles)
	for _, i := range shorts {
		if fullPlan[i].status().WillReachTarget || fullPlan[i].allocKW > capped[i].allocKW+1e-9 {
			return full, noteDemandNoFit
		}
	}
	return target, noteDemandCapped
}

// trialPlan allocates budget over copies of cands (the originals are untouched), with
// the deferral computed for that budget, and returns the copies in the same order.
func trialPlan(cfg Config, p CostPolicy, in Input, budget float64, cands []*candidate, idles []idleBus) []*candidate {
	cp := make([]*candidate, len(cands))
	for i, cd := range cands {
		c := *cd
		c.allocKW, c.leftOut = 0, false
		cp[i] = &c
	}
	deferPastPeak(cfg, p, in, budget, cp, idles)
	allocate(cfg, budget, cp)
	return cp
}

// shortBuses returns the indexes of the buses the plan leaves short: they need charge,
// their charger is usable and the plan does not deliver what they need. Doomed buses
// (negative laxity) are included, and so are buses with an unreliable SoC reading (the
// plan already assumes the conservative estimate for them): the brief asked for savable
// buses with a reliable reading only, but the planner's estimate of what is savable is
// pessimistic, and a bus that the cap starves of leftover power is a bus lost.
func shortBuses(plan []*candidate) []int {
	var out []int
	for i, cd := range plan {
		if cd.need > 0 && !cd.unusable && finite(cd.gridNeed) && !cd.status().WillReachTarget {
			out = append(out, i)
		}
	}
	return out
}

// demandCapFrac is the share of the target the fleet's energy test may count on (the rest
// is the reserve for taper, faults and noise). Empirical.
const (
	demandCapFrac      = 0.85 // schedule known
	demandCapFracBlind = 0.70 // schedule unknown: more reserve, the fleet may still grow
	// A waiting bus with less laxity than this (minutes, at full power on the best
	// charger) makes the cap give way when the schedule is known. Without the schedule
	// any savable waiting bus does.
	demandWaitUrgentMin = 60
)

// capJob is a bus that needs grid energy in [release, deadline] minutes from now.
type capJob struct {
	release, deadline, energy float64
}

// fleetFits reports whether every bus known to need charge could finish if the site
// delivered budget kW all the time: for every release time r and deadline d, the energy
// of the buses released at or after r and due by d fits in budget*(d-r). Known buses are
// the connected ones, the ones waiting for a plug (as on the best charger) and, when the
// schedule is known, the announced ones. Buses the limit cannot save (negative laxity,
// unusable charger) do not count. It ignores plugs and each bus's own power ceiling.
func fleetFits(cfg Config, p CostPolicy, in Input, budget float64, cands []*candidate, idles []idleBus) bool {
	var jobs []capJob
	add := func(cd *candidate, release float64) {
		if cd.need > 0 && !cd.unusable && cd.laxityMin >= 0 && finite(cd.gridNeed) && finite(cd.availMin) && cd.availMin > release {
			jobs = append(jobs, capJob{release: release, deadline: cd.availMin, energy: cd.gridNeed})
		}
	}
	for _, cd := range cands {
		add(cd, 0)
	}
	if ref, ok := bestCharger(in); ok {
		for _, ib := range idles {
			c := newCandidate(cfg, in, ib.bus, ref)
			add(&c, 0)
		}
		if p.ScheduleKnown {
			for _, f := range scheduledBuses(cfg, in, ref) {
				c := f.cand
				add(&c, f.ready)
			}
		}
	}
	releases := map[float64]bool{}
	for _, j := range jobs {
		releases[j.release] = true
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].deadline < jobs[j].deadline })
	for r := range releases {
		cum := 0.0
		for _, j := range jobs {
			if j.release < r {
				continue
			}
			cum += j.energy
			if cum > budget*(j.deadline-r)/60+1e-9 {
				return false
			}
		}
	}
	return true
}

// freeHealthyChargers counts the healthy chargers with no bus on them.
func freeHealthyChargers(in Input) int {
	used := map[string]bool{}
	for _, b := range presentBuses(in) {
		if b.ChargerID != "" {
			used[b.ChargerID] = true
		}
	}
	n := 0
	for _, c := range in.Chargers {
		if c.Healthy() && !used[c.ID] {
			n++
		}
	}
	return n
}

// waitingSavable reports whether some bus waiting for a charger needs charge and could
// still finish at full power on the best charger. The cap slows the buses that hold the
// plugs, so they keep them longer and the waiting bus (moved to a plug by a swap or by
// the operator when one frees) starts later: while a savable bus waits, the limit rules.
func waitingSavable(cfg Config, in Input, idles []idleBus, urgentMin float64) bool {
	ref, ok := bestCharger(in)
	if !ok {
		return false
	}
	for _, ib := range idles {
		c := newCandidate(cfg, in, ib.bus, ref)
		if c.need > 0 && !c.unusable && c.laxityMin >= 0 && c.laxityMin < urgentMin && finite(c.gridNeed) {
			return true
		}
	}
	return false
}
