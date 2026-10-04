package planner

import (
	"fmt"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// maxSwapTrials bounds how many hypothetical swaps one planning cycle evaluates (each
// one re-runs the allocation), so swap search never threatens the cycle's latency.
const maxSwapTrials = 256

// bestCharger returns the healthy charger with the highest max power (ties by ID).
func bestCharger(in Input) (model.Charger, bool) {
	var best model.Charger
	found := false
	for _, c := range in.Chargers {
		if !c.Healthy() {
			continue
		}
		if !found || c.MaxKW > best.MaxKW || (c.MaxKW == best.MaxKW && c.ID < best.ID) {
			best, found = c, true
		}
	}
	return best, found
}

// reachCount allocates budget over copies of cands (the originals are untouched) and
// returns how many buses will reach their target: connected buses per their status,
// waiting buses only if they already need nothing.
func reachCount(cfg Config, budget float64, cands []candidate, waiting []model.Bus) int {
	ptrs := make([]*candidate, len(cands))
	for i := range cands {
		c := cands[i]
		c.allocKW, c.leftOut = 0, false
		ptrs[i] = &c
	}
	allocate(cfg, budget, ptrs)
	n := 0
	for _, c := range ptrs {
		if c.status().WillReachTarget {
			n++
		}
	}
	for _, b := range waiting {
		if idleStatus(cfg, idleBus{bus: b}).WillReachTarget {
			n++
		}
	}
	return n
}

// recommendSwaps suggests moves (executed by people) that put a waiting bus on the
// charger of a connected bus that already reached its (margin-adjusted) target.
// A swap is recommended only if it increases the number of buses that reach their
// target under the current budget, re-running the allocation with the swap applied
// (the incoming bus starts charging cfg.SwapMoveMin minutes later, on the donor's
// own charger). Swaps are chosen greedily, waiting buses with least laxity first:
// each one is judged on the state left by the swaps already recommended this cycle.
//
// Only full buses give way. A donor that still needs charge is never unplugged, not
// even one the current budget cannot finish: as other buses leave, budget frees up and
// such a bus may still finish, which a snapshot cannot see (measured: letting such
// donors go cost up to 5 points of ready% under tight limits).
func recommendSwaps(cfg Config, in Input, budget float64, cands []*candidate, idles []idleBus) []Swap {
	ref, ok := bestCharger(in)
	if !ok || len(idles) == 0 || len(cands) == 0 {
		return nil
	}
	occupied := map[string]bool{}
	for _, cd := range cands {
		occupied[cd.charger.ID] = true
	}
	for _, ib := range idles {
		if ib.bus.ChargerID != "" {
			occupied[ib.bus.ChargerID] = true
		}
	}
	for _, c := range in.Chargers {
		if c.Healthy() && !occupied[c.ID] {
			return nil // a free charger exists: the waiting bus just plugs in there
		}
	}
	// Screen: waiting buses that need charge and have less laxity than
	// cfg.SwapUrgentLaxityMin on the best charger (by default every such bus). Whether
	// a swap really helps is decided below, on the donor's charger.
	type urgent struct {
		bus    model.Bus
		laxity float64
	}
	var urgents []urgent
	for _, ib := range idles {
		cd := newCandidate(cfg, in, ib.bus, ref)
		if cd.need > 0 && !cd.unusable && cd.laxityMin < cfg.SwapUrgentLaxityMin {
			urgents = append(urgents, urgent{ib.bus, cd.laxityMin})
		}
	}
	if len(urgents) == 0 {
		return nil
	}
	sort.Slice(urgents, func(i, j int) bool {
		if urgents[i].laxity != urgents[j].laxity {
			return urgents[i].laxity < urgents[j].laxity
		}
		return urgents[i].bus.ID < urgents[j].bus.ID
	})

	state := make([]candidate, len(cands))
	for i, cd := range cands {
		state[i] = *cd
	}
	waiting := make([]model.Bus, 0, len(idles))
	for _, ib := range idles {
		waiting = append(waiting, ib.bus)
	}
	moved := map[string]bool{} // buses already part of a recommended swap
	base := reachCount(cfg, budget, state, waiting)
	later := in
	later.Now += cfg.SwapMoveMin
	trials := 0
	var swaps []Swap
	for _, u := range urgents {
		// Donors: connected buses already at their target, most laxity first.
		order := make([]int, 0, len(state))
		for i := range state {
			if !moved[state[i].bus.ID] && state[i].need <= 0 {
				order = append(order, i)
			}
		}
		sort.Slice(order, func(a, b int) bool {
			da, db := state[order[a]], state[order[b]]
			if da.laxityMin != db.laxityMin {
				return da.laxityMin > db.laxityMin
			}
			return da.bus.ID < db.bus.ID
		})
		// A full donor draws no power, so swapping it out frees no budget: the trial
		// result depends only on the donor's charger spec. Specs that already failed
		// for this bus are skipped.
		type spec struct{ max, min, eff float64 }
		failed := map[spec]bool{}
		for _, i := range order {
			if trials >= maxSwapTrials {
				return swaps
			}
			d := state[i]
			key := spec{d.charger.MaxKW, d.charger.MinKW, d.charger.Efficiency}
			if failed[key] {
				continue
			}
			in := newCandidate(cfg, later, u.bus, d.charger)
			if in.unusable || in.need <= 0 || in.laxityMin < 0 { // cannot finish even at full power
				failed[key] = true
				continue
			}
			trials++
			trial := append([]candidate(nil), state...)
			trial[i] = in
			trialWaiting := append(withoutBus(waiting, u.bus.ID), d.bus)
			n := reachCount(cfg, budget, trial, trialWaiting)
			if n <= base {
				failed[key] = true
				continue
			}
			swaps = append(swaps, Swap{
				ChargerID: d.charger.ID,
				OutBusID:  d.bus.ID,
				InBusID:   u.bus.ID,
				Reason: fmt.Sprintf("ônibus %s (folga de %.0f min no carregador %s, contando %d min da troca) precisa de carregador; ônibus %s já atingiu o alvo e cede o %s; ônibus prontos previstos passam de %d para %d",
					u.bus.ID, in.laxityMin, d.charger.ID, cfg.SwapMoveMin, d.bus.ID, d.charger.ID, base, n),
			})
			state, waiting, base = trial, trialWaiting, n
			moved[u.bus.ID], moved[d.bus.ID] = true, true
			break
		}
	}
	return swaps
}

func withoutBus(buses []model.Bus, id string) []model.Bus {
	out := make([]model.Bus, 0, len(buses))
	for _, b := range buses {
		if b.ID != id {
			out = append(out, b)
		}
	}
	return out
}
