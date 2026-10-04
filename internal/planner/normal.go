package planner

import (
	"container/heap"
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
	leftOut    bool // left out by admission under overload: only gets leftover power
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

// allocate gives each bus the power that just meets its deadline, then spends spare
// power in the same order on buses whose laxity is below cfg.SurplusLaxityMin (by
// default all of them): savable buses least laxity first, doomed buses last.
//
// Under overload (spec §6.5: readiness first) admit picks the largest set of savable
// buses the budget can finish. Those are served first (requirement, then spare power);
// the buses it leaves out and the doomed ones only get what the admitted buses cannot
// use. Spreading power over every bus by laxity instead finishes fewer buses: it was
// measured below FIFO at 300-500 kW. Without overload nobody is left out and the
// allocation is the plain one above.
func allocate(cfg Config, budget float64, cands []*candidate) {
	var savable, doomed []*candidate
	for _, c := range cands {
		c.leftOut = false
		if c.need <= 0 || c.unusable {
			continue
		}
		if c.laxityMin >= 0 {
			savable = append(savable, c)
		} else {
			doomed = append(doomed, c)
		}
	}
	byLaxity := func(cs []*candidate) {
		sort.SliceStable(cs, func(i, j int) bool {
			if cs[i].laxityMin != cs[j].laxityMin {
				return cs[i].laxityMin < cs[j].laxityMin
			}
			return cs[i].bus.ID < cs[j].bus.ID
		})
	}
	admitted, left := admit(budget, savable)
	// leftOut drives the "priorizados os que conseguem terminar" reason; when nobody
	// was admitted (e.g. a 0 kW budget) nobody was prioritised, so it stays false and
	// the reason is the plain "sem potência disponível" / "potência insuficiente".
	if len(admitted) > 0 {
		for _, c := range left {
			c.leftOut = true
		}
	}
	byLaxity(admitted)
	byLaxity(left)
	sort.SliceStable(doomed, func(i, j int) bool {
		if doomed[i].laxityMin != doomed[j].laxityMin {
			return doomed[i].laxityMin > doomed[j].laxityMin
		}
		return doomed[i].bus.ID < doomed[j].bus.ID
	})
	if len(left) == 0 {
		fill(cfg, budget, append(admitted, doomed...))
		return
	}
	budget = fill(cfg, budget, admitted)
	fill(cfg, budget, append(left, doomed...))
}

// fill allocates budget over buses in the given order and returns what is left.
func fill(cfg Config, budget float64, order []*candidate) float64 {
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
	return budget
}

// admit splits savable buses into the largest set the budget can finish and the rest
// (Moore-Hodgson on the site budget, spec §6.5): buses are taken by deadline; whenever
// the grid energy of the buses taken so far exceeds what the budget delivers by the
// current deadline, the bus needing the most energy is left out. Without overload
// every bus is admitted. It assumes the current budget for the rest of the night and
// ignores buses not yet arrived; per-bus power limits are covered by laxity >= 0.
func admit(budget float64, savable []*candidate) (admitted, left []*candidate) {
	byDeadline := append([]*candidate(nil), savable...)
	sort.SliceStable(byDeadline, func(i, j int) bool {
		a, b := byDeadline[i], byDeadline[j]
		if a.availMin != b.availMin {
			return a.availMin < b.availMin
		}
		if a.gridNeed != b.gridNeed {
			return a.gridNeed < b.gridNeed
		}
		return a.bus.ID < b.bus.ID
	})
	h := &needHeap{}
	total := 0.0
	for _, c := range byDeadline {
		heap.Push(h, c)
		total += c.gridNeed
		if total > budget*c.availMin/60+1e-9 {
			out := heap.Pop(h).(*candidate)
			total -= out.gridNeed
			left = append(left, out)
		}
	}
	return []*candidate(*h), left
}

// needHeap is a max-heap of candidates by grid energy needed (ties: larger ID first,
// so the bus left out is deterministic).
type needHeap []*candidate

func (h needHeap) Len() int { return len(h) }
func (h needHeap) Less(i, j int) bool {
	if h[i].gridNeed != h[j].gridNeed {
		return h[i].gridNeed > h[j].gridNeed
	}
	return h[i].bus.ID > h[j].bus.ID
}
func (h needHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *needHeap) Push(x any)   { *h = append(*h, x.(*candidate)) }
func (h *needHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
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
	case cd.leftOut && cd.allocKW == 0:
		st.Reason = fmt.Sprintf("sem potência disponível dentro do limite da garagem: priorizados os ônibus que conseguem terminar; déficit previsto %.1f kWh", st.ShortfallKWh)
	case cd.leftOut && !st.WillReachTarget:
		st.Reason = fmt.Sprintf("potência insuficiente: o limite da garagem não termina todos os ônibus; priorizados os que conseguem terminar, este recebe a sobra (%.1f kW); déficit previsto %.1f kWh", cd.allocKW, st.ShortfallKWh)
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
