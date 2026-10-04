package planner

import (
	"errors"
	"fmt"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// validateSite rejects input the planner cannot plan at all: without a usable limit
// or step no charger may be powered.
func validateSite(in Input) error {
	if !finite(in.Site.LimitKW) || in.Site.LimitKW < 0 {
		return fmt.Errorf("limite da garagem inválido: %v", in.Site.LimitKW)
	}
	if in.Site.StepMin <= 0 {
		return fmt.Errorf("passo de planejamento inválido: %d", in.Site.StepMin)
	}
	return nil
}

// chargerProblem describes what is wrong with one charger record ("" if nothing).
func chargerProblem(c model.Charger) string {
	switch {
	case c.ID == "":
		return "carregador sem ID"
	case !finite(c.MaxKW) || c.MaxKW <= 0:
		return fmt.Sprintf("potência máxima inválida (%v)", c.MaxKW)
	case !finite(c.MinKW) || c.MinKW < 0 || c.MinKW > c.MaxKW:
		return fmt.Sprintf("piso inválido (%v)", c.MinKW)
	case !finite(c.Efficiency) || c.Efficiency <= 0 || c.Efficiency > 1:
		return fmt.Sprintf("rendimento inválido (%v)", c.Efficiency)
	case !finite(c.LastCommandedKW) || c.LastCommandedKW < 0:
		return fmt.Sprintf("última potência inválida (%v)", c.LastCommandedKW)
	case c.Status != model.ChargerOK && c.Status != model.ChargerFaulted && c.Status != model.ChargerOffline:
		return fmt.Sprintf("status desconhecido (%d)", c.Status)
	}
	return ""
}

// busProblem describes what is wrong with the fields of one bus record ("" if nothing).
// Bad SoC readings are NOT record problems: the layers treat them as unreliable.
func busProblem(b model.Bus) string {
	switch {
	case b.ID == "":
		return "ônibus sem ID"
	case !finite(b.CapacityKWh) || b.CapacityKWh <= 0:
		return fmt.Sprintf("capacidade inválida (%v)", b.CapacityKWh)
	case !finite(b.MaxBatteryKW) || b.MaxBatteryKW <= 0:
		return fmt.Sprintf("potência da bateria inválida (%v)", b.MaxBatteryKW)
	case !finite(b.TargetKWh) || b.TargetKWh < 0:
		return fmt.Sprintf("alvo inválido (%v)", b.TargetKWh)
	}
	return ""
}

// sanitized is an input the layers can plan safely, plus what was removed from it.
type sanitized struct {
	// in keeps every charger (so offline draws stay reserved) but marks bad or
	// contested ones as faulted, and drops bad bus records.
	in          Input
	badChargers map[string]string // charger ID -> problem; their buses get a reason
	dropped     []BusStatus       // present bus records that were removed
	problems    []string          // every problem found, in input order
}

// sanitize drops bad bus records and disables bad charger records instead of
// rejecting the whole input, so one corrupt record never switches the depot off.
// It never reads the site, so Plan runs it before validateSite: even on an invalid
// site its result is used to verify and annotate the fallback plan.
//
//   - A bad charger (bad spec, unknown status, empty or duplicated ID) is kept but
//     marked faulted (offline ones stay offline so their draw is still reserved): it
//     gets 0 kW and the bus on it gets a "registro inválido" reason.
//   - A bad bus (bad fields, empty or duplicated ID, unknown charger, or a charger
//     claimed by another bus record) is dropped and reported with a reason. A charger
//     claimed by a dropped bus is also marked faulted: nobody knows which bus is
//     really on it, so it stays off and is not seen as free.
func sanitize(in Input) sanitized {
	s := sanitized{in: in, badChargers: map[string]string{}}
	count := map[string]int{}
	for _, c := range in.Chargers {
		count[c.ID]++
	}
	s.in.Chargers = make([]model.Charger, len(in.Chargers))
	disable := func(i int) {
		if s.in.Chargers[i].Status != model.ChargerOffline {
			s.in.Chargers[i].Status = model.ChargerFaulted
		}
	}
	for i, c := range in.Chargers {
		s.in.Chargers[i] = c
		p := chargerProblem(c)
		if p == "" && count[c.ID] > 1 {
			p = "ID listado mais de uma vez"
		}
		if p == "" {
			continue
		}
		disable(i)
		if _, seen := s.badChargers[c.ID]; !seen {
			s.badChargers[c.ID] = p
			s.problems = append(s.problems, fmt.Sprintf("carregador %q: %s", c.ID, p))
		}
	}

	busCount := map[string]int{}
	claims := map[string][]string{} // charger ID -> bus IDs claiming it
	for _, b := range in.Buses {
		busCount[b.ID]++
		if b.ChargerID != "" {
			claims[b.ChargerID] = append(claims[b.ChargerID], b.ID)
		}
	}
	contested := map[string]bool{}
	s.in.Buses = make([]model.Bus, 0, len(in.Buses))
	reported := map[string]bool{}
	for _, b := range in.Buses {
		p := busProblem(b)
		switch {
		case p != "":
		case busCount[b.ID] > 1:
			p = "ID de ônibus listado mais de uma vez"
		case b.ChargerID != "" && count[b.ChargerID] == 0:
			p = fmt.Sprintf("referencia carregador inexistente %s", b.ChargerID)
		case b.ChargerID != "" && len(claims[b.ChargerID]) > 1:
			p = fmt.Sprintf("ônibus %v no mesmo carregador %s", claims[b.ChargerID], b.ChargerID)
		}
		if p == "" {
			s.in.Buses = append(s.in.Buses, b)
			continue
		}
		if b.ChargerID != "" {
			contested[b.ChargerID] = true
		}
		if reported[b.ID] {
			continue
		}
		reported[b.ID] = true
		s.problems = append(s.problems, fmt.Sprintf("ônibus %q: %s", b.ID, p))
		if b.ArrivalMin <= in.Now {
			s.dropped = append(s.dropped, BusStatus{BusID: b.ID, Reason: "registro inválido: " + p + "; ônibus ignorado"})
		}
	}
	for i, c := range s.in.Chargers {
		if contested[c.ID] {
			disable(i)
		}
	}
	sort.Slice(s.dropped, func(i, j int) bool { return s.dropped[i].BusID < s.dropped[j].BusID })
	return s
}

// annotate explains what sanitize removed: buses on disabled chargers get a status
// (recomputed from the current record: no power) whose reason names the charger, and
// dropped buses are listed. The plan may also be the cached last-valid one, made from
// an older input, so its statuses for those buses are stale and are replaced, and a
// status for a bus dropped now gives way to the dropped entry (each bus is listed once).
func (s sanitized) annotate(cfg Config, p Plan) Plan {
	if len(s.problems) == 0 {
		return p
	}
	present := map[string]model.Bus{}
	for _, b := range presentBuses(s.in) {
		present[b.ID] = b
	}
	isDropped := make(map[string]bool, len(s.dropped))
	for _, d := range s.dropped {
		isDropped[d.BusID] = true
	}
	kept := make([]BusStatus, 0, len(p.Buses)+len(s.dropped))
	for _, st := range p.Buses {
		if isDropped[st.BusID] {
			continue
		}
		if b, ok := present[st.BusID]; ok && b.ChargerID != "" {
			if prob, bad := s.badChargers[b.ChargerID]; bad {
				st = idleStatus(cfg, idleBus{bus: b, reason: fmt.Sprintf("carregador %s com registro inválido (%s): sem potência", b.ChargerID, prob)})
			}
		}
		kept = append(kept, st)
	}
	p.Buses = append(kept, s.dropped...)
	sort.SliceStable(p.Buses, func(i, j int) bool { return p.Buses[i].BusID < p.Buses[j].BusID })
	p.Notes = append(append([]string(nil), p.Notes...),
		fmt.Sprintf("registros inválidos ignorados (%d): %v", len(s.problems), s.problems))
	return p
}

// ValidateInput reports the first structural problem in the input, or nil. It is
// strict: the Planner itself only rejects an invalid site and plans around bad
// bus or charger records (see sanitize). Bad SoC readings are never problems here:
// they are treated as unreliable by the planner layers.
func ValidateInput(in Input) error {
	if err := validateSite(in); err != nil {
		return err
	}
	if s := sanitize(in); len(s.problems) > 0 {
		return errors.New(s.problems[0])
	}
	return nil
}
