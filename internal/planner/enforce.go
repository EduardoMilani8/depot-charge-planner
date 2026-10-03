package planner

import (
	"fmt"
	"math"
)

// eps is the absolute tolerance (kW) of the verifier's comparisons. It is far above
// float64 rounding for any realistic depot (one ulp of 1e7 kW is about 2e-9 kW, and
// cmd/simrun caps -limit at 1e7). For much larger limits a proportional scale-down
// can round a few ulps above the limit, more than eps; Enforce therefore never relies
// on eps for the total: it shaves the rounding off (shaveRounding) so the enforced
// total, summed in plan order as Violations does, is never above the budget.
const eps = 1e-6

// Violations lists every invariant the plan breaks. It is intentionally small and
// independent of the planner layers: whoever computes is not who guarantees.
func Violations(in Input, p Plan) []string {
	chargers := chargerMap(in)
	dupIDs := duplicateChargerIDs(in)
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
		// An ID listed twice is ambiguous: we cannot know which entry's spec or
		// status is real, so only 0 kW is allowed on it.
		if dupIDs[s.ChargerID] && s.KW > 0 {
			out = append(out, fmt.Sprintf("carregador %s listado mais de uma vez recebeu %.1f kW: só 0 kW é permitido", s.ChargerID, s.KW))
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
	dupIDs := duplicateChargerIDs(in)
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
		if !finite(kw) || kw < 0 || !finite(c.MaxKW) || !finite(c.MinKW) || dupIDs[s.ChargerID] {
			kw = 0 // broken or ambiguous (duplicated) charger spec: the only safe command is off
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
		shaveRounding(fixed, limit)
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

// shaveRounding nudges proportionally scaled setpoints down until their sum (in plan
// order, as Violations computes it) is not above limit. Scaling by limit/total is
// exact in real arithmetic but may round a few ulps high.
func shaveRounding(sps []Setpoint, limit float64) {
	sum := func() float64 {
		t := 0.0
		for _, s := range sps {
			t += s.KW
		}
		return t
	}
	shrink := 1.0
	for i := 0; i < 64; i++ {
		if sum() <= limit {
			return
		}
		shrink -= math.Ldexp(1, -52+i) // grows from 1 ulp of 1.0 upwards
		for j := range sps {
			sps[j].KW *= shrink
		}
	}
	for j := range sps { // unreachable in practice; off is always safe
		sps[j].KW = 0
	}
}
