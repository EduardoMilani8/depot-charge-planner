package planner

import (
	"fmt"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

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

func recommendSwaps(cfg Config, in Input, cands []*candidate, idles []idleBus) []Swap {
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
	sort.Slice(urgents, func(i, j int) bool {
		if urgents[i].laxity != urgents[j].laxity {
			return urgents[i].laxity < urgents[j].laxity
		}
		return urgents[i].bus.ID < urgents[j].bus.ID
	})
	donors := append([]*candidate(nil), cands...)
	sort.Slice(donors, func(i, j int) bool {
		if donors[i].laxityMin != donors[j].laxityMin {
			return donors[i].laxityMin > donors[j].laxityMin
		}
		return donors[i].bus.ID < donors[j].bus.ID
	})
	var swaps []Swap
	for i, u := range urgents {
		if i >= len(donors) {
			break
		}
		d := donors[i]
		if d.laxityMin-u.laxity < cfg.SwapDonorGapMin {
			break
		}
		swaps = append(swaps, Swap{
			ChargerID: d.charger.ID,
			OutBusID:  d.bus.ID,
			InBusID:   u.bus.ID,
			Reason: fmt.Sprintf("ônibus %s tem folga de %.0f min e precisa de carregador; ônibus %s tem folga de %.0f min",
				u.bus.ID, u.laxity, d.bus.ID, finiteLaxity(d.laxityMin)),
		})
	}
	return swaps
}
