package planner

import (
	"fmt"
	"math"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// candidate is a connected bus on a healthy charger, with its derived numbers.
type candidate struct {
	bus        model.Bus
	charger    model.Charger
	need       float64 // battery kWh missing to the (margin-adjusted) target
	gridNeed   float64 // grid kWh required, using the conservative taper cost
	availMin   float64 // minutes until departure (never negative)
	maxKW      float64 // highest grid-side power for this pair
	laxityMin  float64 // minutes of spare time at max power
	requiredKW float64 // power that just meets the deadline
	allocKW    float64
	unusable   bool // battery limit is below the charger floor: cannot charge at all
	reliable   bool
}

// idleBus is a present bus that cannot be charged right now.
type idleBus struct {
	bus    model.Bus
	reason string
}

func isReliable(cfg Config, b model.Bus) bool {
	if !finite(b.SoCKWh) || b.SoCKWh < 0 || b.SoCKWh > b.CapacityKWh {
		return false
	}
	if !finite(b.SoCConfidence) || b.SoCConfidence < cfg.MinConfidence {
		return false
	}
	return b.SoCAgeMin <= cfg.StaleAfterMin
}

// usableSoC returns the SoC the planner will trust: absurd readings are treated as
// an empty battery (worst case), stale ones get a conservative penalty.
func usableSoC(cfg Config, b model.Bus) (float64, bool) {
	rel := isReliable(cfg, b)
	switch {
	case !finite(b.SoCKWh) || b.SoCKWh < 0 || b.SoCKWh > b.CapacityKWh:
		return 0, false
	case !rel:
		return clamp(b.SoCKWh-cfg.UnreliablePenaltyFrac*b.CapacityKWh, 0, b.CapacityKWh), false
	}
	return b.SoCKWh, true
}

func newCandidate(cfg Config, in Input, b model.Bus, c model.Charger) candidate {
	soc, rel := usableSoC(cfg, b)
	target := math.Min(b.CapacityKWh, b.TargetKWh+cfg.MarginKWh)
	cd := candidate{bus: b, charger: c, reliable: rel}
	cd.need = math.Max(0, target-soc)
	cd.maxKW = MaxGridKW(c, b)
	cd.availMin = math.Max(0, float64(b.DepartureMin-in.Now))
	cd.gridNeed = model.EffectiveEnergy(soc, target, b.CapacityKWh) / c.Efficiency
	if cd.maxKW <= 0 || cd.maxKW < c.MinKW {
		cd.unusable = true
	}
	switch {
	case cd.need <= 0:
		cd.laxityMin = cd.availMin
	case cd.unusable:
		cd.laxityMin = math.Inf(-1)
	default:
		cd.laxityMin = cd.availMin - cd.gridNeed/cd.maxKW*60
		if cd.availMin > 0 {
			cd.requiredKW = clamp(cd.gridNeed/(cd.availMin/60), c.MinKW, cd.maxKW)
		} else {
			cd.requiredKW = cd.maxKW
		}
	}
	return cd
}

// allocate gives each bus the power that just meets its deadline (savable buses
// first, least laxity first; doomed buses last), then spends spare power in the same
// order on buses whose laxity is below cfg.SurplusLaxityMin (by default all of them).
func allocate(cfg Config, budget float64, cands []*candidate) {
	var savable, doomed []*candidate
	for _, c := range cands {
		if c.need <= 0 || c.unusable {
			continue
		}
		if c.laxityMin >= 0 {
			savable = append(savable, c)
		} else {
			doomed = append(doomed, c)
		}
	}
	sort.SliceStable(savable, func(i, j int) bool {
		if savable[i].laxityMin != savable[j].laxityMin {
			return savable[i].laxityMin < savable[j].laxityMin
		}
		return savable[i].bus.ID < savable[j].bus.ID
	})
	sort.SliceStable(doomed, func(i, j int) bool {
		if doomed[i].laxityMin != doomed[j].laxityMin {
			return doomed[i].laxityMin > doomed[j].laxityMin
		}
		return doomed[i].bus.ID < doomed[j].bus.ID
	})
	order := append(append([]*candidate(nil), savable...), doomed...)
	// Pass A: buses whose full requirement still fits get it. A bus whose requirement
	// exceeds what is left cannot be saved by this budget, so it is deferred and must
	// not consume power that a bus further down the order could still use to finish.
	var deferred []*candidate
	for _, c := range order {
		if c.requiredKW > budget {
			deferred = append(deferred, c)
			continue
		}
		c.allocKW = c.requiredKW
		budget -= c.requiredKW
	}
	// Deferred buses (same order) share whatever is left; below the charger floor
	// the setpoint is 0, never a value between 0 and the floor.
	for _, c := range deferred {
		give := math.Min(c.requiredKW, budget)
		if give < c.charger.MinKW {
			give = 0
		}
		c.allocKW = give
		budget -= give
	}
	for _, c := range order {
		if budget <= 0 {
			break
		}
		if c.allocKW == 0 || c.laxityMin >= cfg.SurplusLaxityMin {
			continue
		}
		extra := math.Min(c.maxKW-c.allocKW, budget)
		if extra > 0 {
			c.allocKW += extra
			budget -= extra
		}
	}
}

func finiteLaxity(x float64) float64 {
	switch {
	case math.IsInf(x, -1):
		return -1e6
	case math.IsInf(x, 1):
		return 1e6
	}
	return x
}

func (cd *candidate) status() BusStatus {
	st := BusStatus{BusID: cd.bus.ID, Assessed: true, LaxityMin: finiteLaxity(cd.laxityMin)}
	delivered := cd.allocKW * cd.availMin / 60
	st.WillReachTarget = cd.need <= 0 || delivered >= cd.gridNeed-1e-6
	if !st.WillReachTarget {
		st.ShortfallKWh = math.Max(0, cd.gridNeed-delivered) * cd.charger.Efficiency
	}
	switch {
	case cd.need <= 0:
		st.Reason = "alvo atingido"
	case cd.unusable:
		st.Reason = "limite da bateria abaixo do piso do carregador: não é possível carregar"
	case cd.allocKW == 0:
		st.Reason = "sem potência disponível dentro do limite da garagem"
	case cd.laxityMin < 0 && !st.WillReachTarget: // a hair below 0 that still delivers is not infeasible
		st.Reason = fmt.Sprintf("inviável: mesmo na potência máxima faltam %.0f min; déficit previsto %.1f kWh", -cd.laxityMin, st.ShortfallKWh)
	case !st.WillReachTarget:
		st.Reason = fmt.Sprintf("potência insuficiente: %.1f de %.1f kW necessários; déficit previsto %.1f kWh", cd.allocKW, cd.requiredKW, st.ShortfallKWh)
	default:
		st.Reason = fmt.Sprintf("folga %.0f min; %.1f kW", math.Max(0, cd.laxityMin), cd.allocKW)
	}
	if !cd.reliable && cd.need > 0 {
		st.Reason += " (leitura de SoC não confiável: estimativa conservadora)"
	}
	return st
}

func statusName(s model.ChargerStatus) string {
	switch s {
	case model.ChargerFaulted:
		return "em falha"
	case model.ChargerOffline:
		return "offline"
	}
	return "ok"
}

func idleStatus(cfg Config, ib idleBus) BusStatus {
	soc, _ := usableSoC(cfg, ib.bus)
	target := math.Min(ib.bus.CapacityKWh, ib.bus.TargetKWh+cfg.MarginKWh)
	need := math.Max(0, target-soc)
	return BusStatus{BusID: ib.bus.ID, Assessed: true, WillReachTarget: need <= 0, ShortfallKWh: need, Reason: ib.reason}
}

// PlanNormal is layer 1: least-laxity-first allocation under the power budget.
func PlanNormal(cfg Config, in Input) Plan {
	chargers := chargerMap(in)
	plan := Plan{Layer: LayerNormal}
	var cands []*candidate
	var idles []idleBus
	for _, b := range presentBuses(in) {
		c, ok := chargers[b.ChargerID]
		switch {
		case b.ChargerID == "" || !ok:
			idles = append(idles, idleBus{bus: b, reason: "aguardando carregador"})
		case !c.Healthy():
			idles = append(idles, idleBus{bus: b, reason: fmt.Sprintf("carregador %s indisponível (%s)", c.ID, statusName(c.Status))})
		default:
			cd := newCandidate(cfg, in, b, c)
			cands = append(cands, &cd)
		}
	}
	budget := AvailableKW(in)
	plan.Swaps = recommendSwaps(cfg, in, budget, cands, idles) // works on copies: before allocate
	allocate(cfg, budget, cands)
	for _, cd := range cands {
		plan.Setpoints = append(plan.Setpoints, Setpoint{ChargerID: cd.charger.ID, KW: cd.allocKW})
		plan.Buses = append(plan.Buses, cd.status())
	}
	for _, ib := range idles {
		plan.Buses = append(plan.Buses, idleStatus(cfg, ib))
	}
	sort.Slice(plan.Setpoints, func(i, j int) bool { return plan.Setpoints[i].ChargerID < plan.Setpoints[j].ChargerID })
	sort.Slice(plan.Buses, func(i, j int) bool { return plan.Buses[i].BusID < plan.Buses[j].BusID })
	return plan
}
