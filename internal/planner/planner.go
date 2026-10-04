package planner

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// NormalFunc is the layer-1 implementation; replaceable so the simulator can inject faults.
//
// Concurrency: the planner runs it in its own goroutine and abandons it after
// Config.Timeout. An abandoned call keeps running until it returns, so a slow
// NormalFunc may run concurrently with a later call of itself (each call gets a
// private copy of the input). Implementations must not share mutable state between
// calls without their own synchronisation.
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
	return &Planner{cfg: cfg, normal: PlanNormal, log: discardLogger()}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func (p *Planner) WithNormal(f NormalFunc) *Planner { p.normal = f; return p }

// WithLogger sets the structured logger; nil means discard.
func (p *Planner) WithLogger(l *slog.Logger) *Planner {
	if l == nil {
		l = discardLogger()
	}
	p.log = l
	return p
}

// Plan always returns a plan that satisfies every invariant, checked against the
// input exactly as given and against its sanitized copy. An invalid site (limit or
// step) yields the last valid plan (still within its TTL) or all chargers at 0 kW;
// bad bus or charger records are dropped or switched off with a per-bus reason while
// the rest of the depot is planned normally.
func (p *Planner) Plan(in Input) Plan {
	// sanitize never reads the site, so it runs even when the site is invalid: a
	// replayed last-valid plan must not power a charger it switches off (I1).
	s := sanitize(in)
	if len(s.problems) > 0 {
		p.log.Warn("registros inválidos ignorados", "minute", in.Now, "count", len(s.problems), "first", s.problems[0])
	}
	if err := validateSite(in); err != nil {
		p.log.Warn("garagem inválida", "minute", in.Now, "err", err)
		return p.fallback(in, s, "entrada inválida: "+err.Error(), false)
	}
	if tooUnreliable(p.cfg, s.in) {
		p.log.Warn("leituras de SoC pouco confiáveis; usando perfil seguro", "minute", in.Now)
		plan := PlanSafe(s.in)
		plan.Notes = append(plan.Notes, "leituras de SoC pouco confiáveis na maioria dos ônibus")
		return s.annotate(p.cfg, p.finish(in, s, plan))
	}
	plan, err := p.runNormal(s.in)
	if err != nil {
		p.log.Warn("camada normal falhou", "minute", in.Now, "err", err)
		return p.fallback(in, s, err.Error(), true)
	}
	plan.Layer = LayerNormal
	plan = p.finish(in, s, plan)
	p.last, p.hasLast, p.lastAt = clonePlan(plan), true, in.Now
	return s.annotate(p.cfg, plan)
}

// finish runs the verifier against the input exactly as received, so whoever computed
// the plan is never who guarantees it, and then against the sanitized copy as well: a
// plan not computed from s.in (the cached last-valid plan, or garbage from the normal
// layer) must not power a charger that sanitize switched off on this input.
func (p *Planner) finish(in Input, s sanitized, plan Plan) Plan {
	out := Enforce(s.in, Enforce(in, plan))
	if len(out.Notes) > len(plan.Notes) {
		p.log.Warn("verificador corrigiu o plano", "minute", in.Now, "layer", plan.Layer.String())
	}
	return out
}

// fallback replays the last valid plan while it is within LastPlanTTLMin, otherwise
// it uses the safe profile (valid site) or all chargers at 0 kW (invalid site). On an
// invalid site whose limit is still valid (e.g. only StepMin is broken) the replay
// keeps the depot charging within the limit instead of stopping it; with an invalid
// limit the budget is 0 and the replay is scaled to 0 kW.
func (p *Planner) fallback(in Input, s sanitized, reason string, siteValid bool) Plan {
	if p.hasLast && in.Now >= p.lastAt && in.Now-p.lastAt <= p.cfg.LastPlanTTLMin {
		plan := clonePlan(p.last)
		plan.Layer = LayerLastValid
		plan.Swaps = nil
		plan.Notes = append(plan.Notes, "usando o último plano válido: "+reason)
		return s.annotate(p.cfg, p.finish(in, s, plan))
	}
	var plan Plan
	if siteValid {
		plan = PlanSafe(s.in)
	} else {
		plan = zeroPlan(in)
	}
	plan.Notes = append(plan.Notes, "fallback: "+reason)
	return s.annotate(p.cfg, p.finish(in, s, plan))
}

func (p *Planner) runNormal(in Input) (Plan, error) {
	type result struct {
		plan Plan
		err  error
	}
	cfg, normal := p.cfg, p.normal
	// The goroutine may outlive a timeout, while the caller reuses its slices on the
	// next tick. Give it a private snapshot (Charger and Bus hold no reference
	// fields, so a shallow slice clone is a full copy).
	snap := in
	snap.Chargers = slices.Clone(in.Chargers)
	snap.Buses = slices.Clone(in.Buses)
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- result{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		ch <- result{plan: normal(cfg, snap)}
	}()
	select {
	case r := <-ch:
		return r.plan, r.err
	case <-time.After(cfg.Timeout):
		// The abandoned goroutine only touches its private snapshot and finishes on its own.
		return Plan{}, errors.New("timeout da camada normal")
	}
}

// clonePlan deep-copies the slices of a plan so cached and returned plans never alias.
func clonePlan(p Plan) Plan {
	p.Setpoints = slices.Clone(p.Setpoints)
	p.Buses = slices.Clone(p.Buses)
	p.Swaps = slices.Clone(p.Swaps)
	p.Notes = slices.Clone(p.Notes)
	return p
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

// zeroPlan commands 0 kW on every (deduplicated) charger; used for an invalid site.
func zeroPlan(in Input) Plan {
	p := Plan{Layer: LayerSafe, Notes: []string{"garagem inválida: todos os carregadores em 0 kW"}}
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
