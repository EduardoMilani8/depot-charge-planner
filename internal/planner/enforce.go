package planner

import (
	"fmt"
	"math"
)

const eps = 1e-6

// Violations lists every invariant the plan breaks. It is intentionally small and
// independent of the planner layers: whoever computes is not who guarantees.
func Violations(in Input, p Plan) []string {
	chargers := chargerMap(in)
	var out []string
	seen := map[string]bool{}
	total := 0.0
	for _, s := range p.Setpoints {
		c, ok := chargers[s.ChargerID]
		if !ok {
			out = append(out, fmt.Sprintf("carregador desconhecido: %s", s.ChargerID))
			continue
		}
		if seen[s.ChargerID] {
			out = append(out, fmt.Sprintf("setpoint duplicado: %s", s.ChargerID))
			continue
		}
		seen[s.ChargerID] = true
		if !finite(s.KW) || s.KW < 0 {
			out = append(out, fmt.Sprintf("potência inválida em %s: %v", s.ChargerID, s.KW))
			continue
		}
		if !c.Healthy() && s.KW > 0 {
			out = append(out, fmt.Sprintf("carregador indisponível %s recebeu %.1f kW", s.ChargerID, s.KW))
		}
		// 0 kW is always a legal setpoint, even for a charger whose spec is broken
		// (e.g. a negative MaxKW): switching off can never exceed a ceiling. A positive
		// setpoint needs finite bounds: with a NaN bound every ordinary comparison is
		// false and would silently pass.
		if s.KW > 0 {
			switch {
			case !finite(c.MaxKW) || !finite(c.MinKW):
				out = append(out, fmt.Sprintf("%s com especificação inválida (teto %v, piso %v): só 0 kW é permitido", s.ChargerID, c.MaxKW, c.MinKW))
			case s.KW > c.MaxKW+eps:
				out = append(out, fmt.Sprintf("%s acima do teto: %.1f > %.1f kW", s.ChargerID, s.KW, c.MaxKW))
			case s.KW < c.MinKW-eps:
				out = append(out, fmt.Sprintf("%s abaixo do piso: %.1f < %.1f kW", s.ChargerID, s.KW, c.MinKW))
			}
		}
		total += s.KW
	}
	if limit := AvailableKW(in); total > limit+eps {
		out = append(out, fmt.Sprintf("potência total %.1f kW acima do orçamento %.1f kW", total, limit))
	}
	return out
}

// Enforce returns a plan that satisfies every invariant, correcting the input
// plan if needed (scaling down proportionally, then switching off below the floor).
func Enforce(in Input, p Plan) Plan {
	viol := Violations(in, p)
	if len(viol) == 0 {
		return p
	}
	chargers := chargerMap(in)
	fixed := make([]Setpoint, 0, len(p.Setpoints))
	seen := map[string]bool{}
	total := 0.0
	for _, s := range p.Setpoints {
		c, ok := chargers[s.ChargerID]
		if !ok || seen[s.ChargerID] || !c.Healthy() || !finite(s.KW) || s.KW < 0 {
			continue
		}
		seen[s.ChargerID] = true
		kw := math.Min(s.KW, c.MaxKW)
		if !finite(kw) || kw < 0 || !finite(c.MaxKW) || !finite(c.MinKW) {
			kw = 0 // broken charger spec: the only safe command is off
		}
		fixed = append(fixed, Setpoint{ChargerID: s.ChargerID, KW: kw})
		total += kw
	}
	if limit := AvailableKW(in); total > limit {
		f := 0.0
		if total > 0 {
			f = limit / total
		}
		for i := range fixed {
			fixed[i].KW *= f
		}
	}
	for i := range fixed {
		if fixed[i].KW < chargers[fixed[i].ChargerID].MinKW {
			fixed[i].KW = 0
		}
	}
	out := p
	out.Setpoints = fixed
	out.Notes = append(append([]string(nil), p.Notes...), fmt.Sprintf("verificador corrigiu o plano: %d violação(ões)", len(viol)))
	return out
}
