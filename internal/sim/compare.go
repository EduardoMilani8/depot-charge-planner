package sim

import (
	"context"
	"fmt"
	"sync"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// ControllerNames lists the controllers Compare runs, in table order.
var ControllerNames = []string{"fifo", "edf", "fifo-unplug", "safe", "planner"}

// ForController returns the scenario to run for a controller (fifo-unplug turns on the
// operators' manual unplug routine) and the controller itself.
func ForController(name string, cfg planner.Config, sc Scenario) (Scenario, Controller, error) {
	switch name {
	case "fifo":
		return sc, NewFIFO(), nil
	case "edf":
		return sc, NewEDF(), nil
	case "fifo-unplug":
		sc.UnplugFull = true
		return sc, NewFIFO(), nil
	case "safe":
		return sc, NewSafeOnly(), nil
	case "planner":
		return sc, NewPlannerController(cfg, sc), nil
	}
	return sc, nil, fmt.Errorf("unknown controller %q", name)
}

// SeedResult is one run of one controller.
type SeedResult struct {
	Seed    int64
	Metrics Metrics
}

// ControllerResult is one controller over seeds 1..N: the mean (Aggregate) and each run.
type ControllerResult struct {
	Name      string
	Aggregate Metrics
	Seeds     []SeedResult
}

// Compare runs every controller on seeds 1..seeds. Runs are independent, so up to
// `workers` of them run at once; results are always ordered by controller and seed, so
// they do not depend on workers (PlanP99Micros is wall-clock and does). ctx is checked
// before each run is started.
func Compare(ctx context.Context, p GenParams, cfg planner.Config, seeds, workers int) ([]ControllerResult, error) {
	if workers < 1 {
		workers = 1
	}
	out := make([]ControllerResult, 0, len(ControllerNames))
	for _, name := range ControllerNames {
		res := make([]SeedResult, seeds)
		sem := make(chan struct{}, workers)
		var wg sync.WaitGroup
		for i := 0; i < seeds; i++ {
			if ctx.Err() != nil {
				break
			}
			sem <- struct{}{}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				defer func() { <-sem }()
				seed := int64(i + 1)
				sc, ctrl, _ := ForController(name, cfg, Generate(p, seed)) // names are fixed: no error
				res[i] = SeedResult{Seed: seed, Metrics: Run(sc, ctrl, nil)}
			}(i)
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ms := make([]Metrics, seeds)
		for i, r := range res {
			ms[i] = r.Metrics
		}
		out = append(out, ControllerResult{Name: name, Aggregate: Aggregate(ms), Seeds: res})
	}
	return out, nil
}
