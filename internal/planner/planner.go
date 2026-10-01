package planner

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// NormalFunc is the layer-1 implementation; replaceable so the simulator can inject faults.
type NormalFunc func(Config, Input) Plan

// Planner wraps the layers with graceful degradation. It is the only stateful
// piece: it remembers the last valid plan. Not safe for concurrent use.
type Planner struct {
	cfg     Config
	normal  NormalFunc
	log     *slog.Logger
	last    Plan
	hasLast bool
	lastAt  model.Minute
}

func New(cfg Config) *Planner {
	return &Planner{cfg: cfg, normal: PlanNormal, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func (p *Planner) WithNormal(f NormalFunc) *Planner   { p.normal = f; return p }
func (p *Planner) WithLogger(l *slog.Logger) *Planner { p.log = l; return p }

// Plan always returns a plan that satisfies every invariant.
func (p *Planner) Plan(in Input) Plan {
	if err := ValidateInput(in); err != nil {
		p.log.Warn("entrada inválida", "minute", in.Now, "err", err)
		return p.fallback(in, "entrada inválida: "+err.Error(), false)
	}
	if tooUnreliable(p.cfg, in) {
		p.log.Warn("leituras de SoC pouco confiáveis; usando perfil seguro", "minute", in.Now)
		plan := PlanSafe(in)
		plan.Notes = append(plan.Notes, "leituras de SoC pouco confiáveis na maioria dos ônibus")
		return p.finish(in, plan)
	}
	plan, err := p.runNormal(in)
	if err != nil {
		p.log.Warn("camada normal falhou", "minute", in.Now, "err", err)
		return p.fallback(in, err.Error(), true)
	}
	plan.Layer = LayerNormal
	plan = p.finish(in, plan)
	p.last, p.hasLast, p.lastAt = plan, true, in.Now
	return plan
}

func (p *Planner) finish(in Input, plan Plan) Plan {
	out := Enforce(in, plan)
	if len(out.Notes) > len(plan.Notes) {
		p.log.Warn("verificador corrigiu o plano", "minute", in.Now, "layer", plan.Layer.String())
	}
	return out
}

func (p *Planner) fallback(in Input, reason string, inputValid bool) Plan {
	if p.hasLast && in.Now >= p.lastAt && in.Now-p.lastAt <= p.cfg.LastPlanTTLMin {
		plan := p.last
		plan.Layer = LayerLastValid
		plan.Swaps = nil
		plan.Notes = append(append([]string(nil), plan.Notes...), "usando o último plano válido: "+reason)
		return p.finish(in, plan)
	}
	var plan Plan
	if inputValid {
		plan = PlanSafe(in)
	} else {
		plan = zeroPlan(in)
	}
	plan.Notes = append(plan.Notes, "fallback: "+reason)
	return p.finish(in, plan)
}

func (p *Planner) runNormal(in Input) (Plan, error) {
	type result struct {
		plan Plan
		err  error
	}
	cfg, normal := p.cfg, p.normal
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- result{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		ch <- result{plan: normal(cfg, in)}
	}()
	select {
	case r := <-ch:
		return r.plan, r.err
	case <-time.After(cfg.Timeout):
		// The abandoned goroutine only reads its inputs and finishes on its own.
		return Plan{}, errors.New("timeout da camada normal")
	}
}

// tooUnreliable reports whether most connected buses have untrustworthy SoC data.
func tooUnreliable(cfg Config, in Input) bool {
	chargers := chargerMap(in)
	total, bad := 0, 0
	for _, b := range presentBuses(in) {
		if c, ok := chargers[b.ChargerID]; ok && c.Healthy() {
			total++
			if !isReliable(cfg, b) {
				bad++
			}
		}
	}
	return total > 0 && float64(bad)/float64(total) > cfg.MaxUnreliableFrac
}

// zeroPlan commands 0 kW on every (deduplicated) charger; used for broken input.
func zeroPlan(in Input) Plan {
	p := Plan{Layer: LayerSafe, Notes: []string{"entrada inválida: todos os carregadores em 0 kW"}}
	seen := map[string]bool{}
	for _, c := range in.Chargers {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		p.Setpoints = append(p.Setpoints, Setpoint{ChargerID: c.ID})
	}
	return p
}
