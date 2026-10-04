package planner

import (
	"fmt"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func bigInput(n int) Input {
	in := Input{Site: model.Site{LimitKW: 6000, StepMin: 1}}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%03d", i)
		in.Chargers = append(in.Chargers, testCharger("C"+id))
		in.Buses = append(in.Buses, testBus("B"+id, "C"+id, float64(20+i%200), 250, 200+(i*7)%500))
	}
	return in
}

func BenchmarkPlanNormal200(b *testing.B) {
	in, cfg := bigInput(200), DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PlanNormal(cfg, in)
	}
}

// The spec's initial goal: 200 buses in under 50 ms per cycle.
func TestPlanNormal200Under50ms(t *testing.T) {
	in, cfg := bigInput(200), DefaultConfig()
	start := time.Now()
	PlanNormal(cfg, in)
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("planning 200 buses took %v, goal is under 50ms", d)
	}
}

// swapHeavyInput: 200 buses on 100 chargers; half the connected buses are already full
// and 100 buses wait, so every cycle searches swaps up to the trial cap.
func swapHeavyInput() Input {
	in := Input{Site: model.Site{LimitKW: 3000, StepMin: 1}}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("%03d", i)
		in.Chargers = append(in.Chargers, testCharger("C"+id))
		soc := 260.0 // full
		if i%2 == 0 {
			soc = float64(20 + i)
		}
		in.Buses = append(in.Buses, testBus("B"+id, "C"+id, soc, 250, 200+(i*7)%500))
		in.Buses = append(in.Buses, testBus("W"+id, "", float64(20+i), 250, 150+(i*11)%500))
	}
	return in
}

func BenchmarkPlanNormalSwapSearch200(b *testing.B) {
	in, cfg := swapHeavyInput(), DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PlanNormal(cfg, in)
	}
}

// swapHeavyDistinctInput is swapHeavyInput with a different MaxKW on every charger.
// The swap search skips donor chargers whose spec already failed for a waiting bus;
// with all specs distinct nothing can be skipped, so this is the worst case of the
// trial cap (maxSwapTrials allocations per cycle).
func swapHeavyDistinctInput() Input {
	in := swapHeavyInput()
	for i := range in.Chargers {
		in.Chargers[i].MaxKW = 100 + 0.5*float64(i)
	}
	return in
}

func BenchmarkPlanNormalSwapSearch200Distinct(b *testing.B) {
	in, cfg := swapHeavyDistinctInput(), DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PlanNormal(cfg, in)
	}
}

// The facade adds sanitize, the goroutine and timeout, and two verifier passes.
func BenchmarkPlannerPlanSwapSearch200Distinct(b *testing.B) {
	in := swapHeavyDistinctInput()
	cfg := DefaultConfig()
	cfg.Timeout = time.Minute // measure the work, not a fallback
	pl := New(cfg)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if p := pl.Plan(in); p.Layer != LayerNormal {
			b.Fatalf("layer %v", p.Layer)
		}
	}
}

func BenchmarkPlannerPlan200(b *testing.B) {
	in := bigInput(200)
	cfg := DefaultConfig()
	cfg.Timeout = time.Minute
	pl := New(cfg)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pl.Plan(in)
	}
}
