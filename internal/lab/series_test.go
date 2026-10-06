package lab

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

func TestSeriesJSON(t *testing.T) {
	got, err := json.Marshal(Series{1.04, math.NaN(), math.Inf(1), -0.04, 150, 37.56, 0.1 + 0.2})
	if err != nil {
		t.Fatal(err)
	}
	want := `[1,null,null,0,150,37.6,0.3]`
	if string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if b, _ := json.Marshal(Series(nil)); string(b) != "[]" {
		t.Fatalf("nil series: %s", b)
	}
}

func TestConvertReasonTableAndClamp(t *testing.T) {
	p := defaultParams()
	p.Buses, p.Chargers, p.LimitKW, p.Profile = 10, 5, 500, "severe"
	g, cfg := p.toSim()
	sc, ctrl, _ := sim.ForController("planner", cfg, sim.Generate(g, 1))
	tr := sim.NewTrace()
	m := sim.RunTraced(sc, ctrl, nil, tr)
	resp := buildRun(p, 1, "planner", sc, m, tr)
	if len(resp.Decisions) == 0 {
		t.Fatal("no decisions")
	}
	seen := map[string]bool{}
	for _, r := range resp.Reasons {
		if seen[r] {
			t.Fatalf("reason table repeats %q", r)
		}
		seen[r] = true
	}
	for _, f := range resp.Scenario.Faults {
		if f.To > resp.Scenario.HorizonMin {
			t.Fatalf("fault window not clamped: %+v", f)
		}
	}
	// A planner Plan with a non-finite shortfall must still serialize.
	d := decisionToDTO(sim.Decision{Buses: []planner.BusStatus{{BusID: "B1", ShortfallKWh: math.NaN()}}}, func(string) int { return -1 })
	if _, err := json.Marshal(d); err != nil {
		t.Fatalf("a NaN shortfall broke the JSON: %v", err)
	}
}
