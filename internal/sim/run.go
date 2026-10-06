package sim

import (
	"context"
	"math"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// Recorder receives every decision (input and plan); see the decision log.
type Recorder interface {
	Record(minute int, in planner.Input, p planner.Plan) error
}

// Run simulates the scenario with the given controller. rec may be nil.
func Run(sc Scenario, ctrl Controller, rec Recorder) Metrics {
	return RunTraced(sc, ctrl, rec, nil)
}

// RunTraced is Run that also fills tr (when not nil) with the simulator's truth and
// the decisions. It returns exactly the metrics Run returns.
func RunTraced(sc Scenario, ctrl Controller, rec Recorder, tr *Trace) Metrics {
	m, _ := RunContext(context.Background(), sc, ctrl, rec, tr) // Background never ends: no error
	return m
}

// RunContext is RunTraced that can be cancelled: it checks ctx before every simulated
// minute and returns ctx.Err() (with zero Metrics and a partial tr) as soon as it ends.
// With a context that never ends it returns exactly the metrics RunTraced returns.
func RunContext(ctx context.Context, sc Scenario, ctrl Controller, rec Recorder, tr *Trace) (Metrics, error) {
	w := newWorld(sc)
	m := Metrics{Buses: len(sc.Buses), LayerTicks: map[string]int{}}
	durations := make([]time.Duration, 0, sc.Horizon+1)
	prev := map[string]float64{}
	for t := 0; t <= sc.Horizon; t++ {
		if err := ctx.Err(); err != nil {
			return Metrics{}, err
		}
		w.beginTick(t)
		in := w.observe()
		if tr != nil {
			tr.sense(w)
		}
		start := time.Now()
		plan := ctrl.Plan(in)
		durations = append(durations, time.Since(start))
		m.LayerTicks[plan.Layer.String()]++
		if rec != nil {
			_ = rec.Record(t, in, plan)
		}
		w.cmd = map[string]float64{}
		for _, s := range plan.Setpoints {
			_ = w.SetPower(s.ChargerID, s.KW)
		}
		if w.commandedTotal() > w.limitAt(t)+1e-6 {
			m.PlanViolations++
		}
		if changed(prev, w.cmd) {
			m.PlanChanges++
		}
		prev = w.cmd
		if sc.FollowSwaps {
			w.applySwaps(plan.Swaps)
		}
		w.advance(&m)
		if tr != nil {
			tr.capture(w, in, plan)
			tr.recordDecision(t, plan)
		}
		w.endTick()
	}
	w.finish(&m)
	if tr != nil {
		tr.finish(w)
	}
	m.PlanP99Micros = p99Micros(durations)
	return m, nil
}

func changed(a, b map[string]float64) bool {
	for k, v := range a {
		if math.Abs(v-b[k]) > 1e-6 {
			return true
		}
	}
	for k, v := range b {
		if math.Abs(v-a[k]) > 1e-6 {
			return true
		}
	}
	return false
}
