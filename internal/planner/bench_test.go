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
