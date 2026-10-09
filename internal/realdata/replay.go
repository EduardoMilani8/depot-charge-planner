package realdata

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

// ReplayNames are the rows after "real": the five sim.ControllerNames, with the
// planner twice (operators follow swaps / do not).
var ReplayNames = []string{"fifo", "edf", "fifo-unplug", "safe", "planner", "planner (sem rodízio)"}

const (
	rowPlanner = 4 // index of "planner" in ReplayNames
	rowNoSwap  = 5 // index of "planner (sem rodízio)"
)

// SeedResult is one simulator run (here: one night).
type SeedResult struct {
	Seed    int64       `json:"seed"`
	Metrics sim.Metrics `json:"metrics"`
}

// ControllerResult mirrors sim.ControllerResult with JSON tags: one controller
// over the runs it was given (per night: the single run, Seed 1; over all
// nights: one run per night, Seed = night index + 1) and their mean.
//
// LayerTicks: a per-night Aggregate keeps the layer counts of its single run;
// the cross-night Report.Aggregate[i].Aggregate.LayerTicks is null (sim.Aggregate
// does not sum maps), and the per-night counts live in Seeds[k].Metrics.LayerTicks.
type ControllerResult struct {
	Name      string       `json:"name"`
	Aggregate sim.Metrics  `json:"aggregate"`
	Seeds     []SeedResult `json:"seeds"`
}

// BusRow compares one bus of a night: what happened and what the planner (with
// and without the operators following swaps) ends with. The Real* fields are nil
// when soc_saida_real_pct is missing for the bus.
type BusRow struct {
	ID              string   `json:"id"`
	CapacityKWh     float64  `json:"capacity_kwh"`
	TargetKWh       float64  `json:"target_kwh"` // required charge at departure, capped to the capacity
	RealFinalKWh    *float64 `json:"real_final_kwh"`
	RealReady       *bool    `json:"real_ready"`
	PlannerFinalKWh float64  `json:"planner_final_kwh"`
	PlannerReady    bool     `json:"planner_ready"`
	NoSwapFinalKWh  float64  `json:"no_swap_final_kwh"`
	NoSwapReady     bool     `json:"no_swap_ready"`
}

// NightReport is one night: the measured outcome and every controller on it.
type NightReport struct {
	Key         string             `json:"key"`
	Buses       int                `json:"buses"`
	Real        Real               `json:"real"`
	Controllers []ControllerResult `json:"controllers"` // one run (Seed 1) per night, in ReplayNames order; Aggregate = that run
	PerBus      []BusRow           `json:"per_bus"`     // sorted by ID
}

// Report is the whole comparison. PlanP99Micros inside the metrics is wall-clock
// time, kept as measured.
type Report struct {
	Nights    []NightReport      `json:"nights"`
	Aggregate []ControllerResult `json:"aggregate"` // over the nights, in ReplayNames order
	// RealAggregate: Buses, WithOutcome and Ready are summed over the nights;
	// ReadyPct and ShortfallKWh are means over the nights that have an outcome;
	// EnergyKWh, PeakKW and CostBRL are means over the nights that have the value
	// (nil if none); the Estimated flags are true if any contributing night is.
	// Partial is true if any night is; CoveredBuses is summed like Buses, and PartialNote
	// says which kind of gap it is ("parcial: 5 de 6 ônibus", or the divergence note).
	RealAggregate Real `json:"real_aggregate"`
	// CostComparable is false when the simulated cost cannot be compared with the
	// real one: no tariff, or a peak window that crosses midnight (sim.Tariff
	// cannot express it, so the simulator prices the whole night off-peak).
	CostComparable bool      `json:"cost_comparable"`
	Assumptions    []string  `json:"assumptions"` // fixed Portuguese statements
	Warnings       []Warning `json:"warnings"`    // d.Warnings, then the ones Nights returned
}

type replayJob struct {
	night, row int
	sc         sim.Scenario
	trace      bool
}

type replayOut struct {
	m        sim.Metrics
	outcomes []sim.BusOutcome
	err      error
}

// Replay runs every controller of ReplayNames over every night of the dataset.
// Up to workers runs go at once (fewer than 1 means 1); the order of the result
// never depends on workers. A cancelled ctx returns ctx.Err() and no report.
func Replay(ctx context.Context, d *Dataset, cfg planner.Config, o NightOptions, workers int) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if workers < 1 {
		workers = 1
	}
	nights, nightWarns := d.Nights(o)

	jobs := make([]replayJob, 0, len(nights)*len(ReplayNames))
	for ni, n := range nights {
		for ri := range ReplayNames {
			sc := cloneScenario(n.Scenario)
			if ri == rowNoSwap {
				sc.FollowSwaps = false
			}
			jobs = append(jobs, replayJob{night: ni, row: ri, sc: sc, trace: ri == rowPlanner || ri == rowNoSwap})
		}
	}

	outs := make([]replayOut, len(jobs))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range jobs {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			j := jobs[i]
			name := ReplayNames[j.row]
			if j.row == rowNoSwap {
				name = "planner"
			}
			// ForController builds the controller from the scenario of this very
			// run (the planner reads it), so it is created here, in the goroutine.
			sc, ctrl, err := sim.ForController(name, cfg, j.sc)
			if err != nil {
				outs[i].err = err
				return
			}
			var tr *sim.Trace
			if j.trace {
				tr = sim.NewTrace()
			}
			m, err := sim.RunContext(ctx, sc, ctrl, nil, tr)
			outs[i].m, outs[i].err = m, err
			if tr != nil {
				outs[i].outcomes = tr.Outcomes
			}
		}(i)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range outs {
		if outs[i].err != nil {
			return nil, outs[i].err
		}
		if !finiteMetrics(outs[i].m) {
			return nil, fmt.Errorf("noite %s, %s: o simulador devolveu um número que não é finito", nights[jobs[i].night].Key, ReplayNames[jobs[i].row])
		}
	}

	rep := &Report{
		Nights:         make([]NightReport, 0, len(nights)),
		CostComparable: costComparable(d.Garage),
	}
	perRow := make([][]sim.Metrics, len(ReplayNames))
	for ni, n := range nights {
		nr := NightReport{Key: n.Key, Buses: len(n.Scenario.Buses), Real: n.Real}
		base := ni * len(ReplayNames)
		for ri, name := range ReplayNames {
			m := outs[base+ri].m
			agg := sim.Aggregate([]sim.Metrics{m})
			agg.LayerTicks = m.LayerTicks // the single run keeps its layer counts
			nr.Controllers = append(nr.Controllers, ControllerResult{
				Name: name, Aggregate: agg, Seeds: []SeedResult{{Seed: 1, Metrics: m}},
			})
			perRow[ri] = append(perRow[ri], m)
		}
		nr.PerBus = busRows(n.BusReal, outs[base+rowPlanner].outcomes, outs[base+rowNoSwap].outcomes)
		for _, b := range nr.PerBus {
			// a non-finite number must never reach the JSON (encoding/json fails on it):
			// like finiteMetrics above, report it as an error instead of clamping it.
			if !finiteBusRow(b) {
				return nil, fmt.Errorf("noite %s, ônibus %s: o simulador devolveu um número que não é finito", n.Key, b.ID)
			}
		}
		rep.Nights = append(rep.Nights, nr)
	}
	for ri, name := range ReplayNames {
		cr := ControllerResult{Name: name, Aggregate: sim.Aggregate(perRow[ri]), Seeds: make([]SeedResult, len(perRow[ri]))}
		for k, m := range perRow[ri] {
			cr.Seeds[k] = SeedResult{Seed: int64(k + 1), Metrics: m}
		}
		rep.Aggregate = append(rep.Aggregate, cr)
	}
	rep.RealAggregate = realAggregate(nights)
	rep.Assumptions = assumptions(o, d.Garage)
	rep.Warnings = append(append([]Warning{}, d.Warnings...), nightWarns...)
	return rep, nil
}

// cloneScenario copies the slices of a scenario so concurrent runs never share
// memory.
func cloneScenario(sc sim.Scenario) sim.Scenario {
	sc.Chargers = append(sc.Chargers[:0:0], sc.Chargers...)
	sc.Buses = append(sc.Buses[:0:0], sc.Buses...)
	sc.Faults = append(sc.Faults[:0:0], sc.Faults...)
	return sc
}

// costComparable: the simulated cost needs a tariff the simulator can express.
func costComparable(g Garage) bool { return g.HasTariff && g.PeakFromMin <= g.PeakToMin }

func finiteMetrics(m sim.Metrics) bool {
	for _, v := range []float64{m.ReadyPct, m.ShortfallKWh, m.PeakKW, m.EnergyKWh, m.CostBRL, m.OperatorMoves} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// finiteBusRow: every float of the row is a finite number (nil real outcome is fine).
func finiteBusRow(b BusRow) bool {
	vs := []float64{b.CapacityKWh, b.TargetKWh, b.PlannerFinalKWh, b.NoSwapFinalKWh}
	if b.RealFinalKWh != nil {
		vs = append(vs, *b.RealFinalKWh)
	}
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func busRows(real []BusReal, planner, noSwap []sim.BusOutcome) []BusRow {
	byID := func(os []sim.BusOutcome) map[string]sim.BusOutcome {
		m := make(map[string]sim.BusOutcome, len(os))
		for _, o := range os {
			m[o.ID] = o
		}
		return m
	}
	p, n := byID(planner), byID(noSwap)
	rows := make([]BusRow, 0, len(real))
	for _, r := range real {
		po := p[r.ID]
		no := n[r.ID]
		rows = append(rows, BusRow{
			ID: r.ID, CapacityKWh: po.CapacityKWh, TargetKWh: po.TrueTargetKWh,
			RealFinalKWh: r.FinalKWh, RealReady: r.Ready,
			PlannerFinalKWh: po.FinalSoCKWh, PlannerReady: po.Ready,
			NoSwapFinalKWh: no.FinalSoCKWh, NoSwapReady: no.Ready,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func realAggregate(nights []Night) Real {
	var a Real
	var outN int
	var eSum, pSum, cSum float64
	var eN, pN, cN int
	for _, n := range nights {
		r := n.Real
		a.Buses += r.Buses
		a.CoveredBuses += r.CoveredBuses
		a.Partial = a.Partial || r.Partial
		a.WithOutcome += r.WithOutcome
		a.Ready += r.Ready
		if r.WithOutcome > 0 {
			a.ReadyPct += r.ReadyPct
			a.ShortfallKWh += r.ShortfallKWh
			outN++
		}
		if r.EnergyKWh != nil {
			eSum += *r.EnergyKWh
			eN++
		}
		if r.PeakKW != nil {
			pSum += *r.PeakKW
			pN++
			a.PeakEstimated = a.PeakEstimated || r.PeakEstimated
		}
		if r.CostBRL != nil {
			cSum += *r.CostBRL
			cN++
			a.CostEstimated = a.CostEstimated || r.CostEstimated
		}
	}
	if outN > 0 {
		a.ReadyPct /= float64(outN)
		a.ShortfallKWh /= float64(outN)
	}
	mean := func(sum float64, n int) *float64 {
		if n == 0 {
			return nil
		}
		v := sum / float64(n)
		return &v
	}
	a.EnergyKWh, a.PeakKW, a.CostBRL = mean(eSum, eN), mean(pSum, pN), mean(cSum, cN)
	if a.Partial {
		if a.CoveredBuses < a.Buses {
			a.PartialNote = fmt.Sprintf("parcial: %d de %d ônibus", a.CoveredBuses, a.Buses)
		} else {
			a.PartialNote = "parcial: sessões e leituras de potência divergem em alguma noite"
		}
	}
	return a
}

func assumptions(o NightOptions, g Garage) []string {
	soc := "As leituras de carga (SoC) dos ônibus são tratadas como perfeitas."
	if o.SoCNoiseKWh > 0 && !math.IsInf(o.SoCNoiseKWh, 0) {
		soc = fmt.Sprintf("As leituras de carga (SoC) dos ônibus têm ruído de %g kWh (desvio padrão), para testar a sensibilidade.", o.SoCNoiseKWh)
	}
	as := []string{
		soc,
		"O limite de potência da garagem (limite_kw) é fixo durante toda a noite.",
		"Nenhum defeito de carregador é injetado: o que deu errado na operação real já está nos dados.",
		"\"planner\" supõe que os operadores executam todos os rodízios recomendados; \"planner (sem rodízio)\" supõe que nenhum é executado.",
		"A operação real tomou decisões (ordem de ligação, rodízios manuais) que o planejador não vê; o simulador reproduz as condições da noite, não o passado minuto a minuto.",
	}
	if g.HasTariff && g.PeakFromMin > g.PeakToMin {
		as = append(as, "A janela de ponta atravessa a meia-noite e o simulador não a representa: o custo simulado não é comparável com o custo real (o custo real usa a janela correta).")
	}
	return as
}
