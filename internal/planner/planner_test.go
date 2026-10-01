package planner

import (
	"strings"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func TestPlannerNormalPath(t *testing.T) {
	in := validInput()
	p := New(testConfig()).Plan(in)
	if p.Layer != LayerNormal || sp(p, "C1") <= 0 {
		t.Errorf("unexpected plan: %+v", p)
	}
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("violations: %v", v)
	}
}

func TestPlannerPanicFallsBackToLastValidThenSafe(t *testing.T) {
	calls := 0
	pl := New(testConfig()).WithNormal(func(c Config, in Input) Plan {
		calls++
		if calls > 1 {
			panic("boom")
		}
		return PlanNormal(c, in)
	})
	in := validInput()
	first := pl.Plan(in)
	if first.Layer != LayerNormal {
		t.Fatalf("first layer = %v", first.Layer)
	}
	in.Now = 1
	second := pl.Plan(in)
	if second.Layer != LayerLastValid || sp(second, "C1") != sp(first, "C1") {
		t.Errorf("expected last-valid with the same setpoint: %+v", second)
	}
	in.Now = 100 // beyond LastPlanTTLMin
	third := pl.Plan(in)
	if third.Layer != LayerSafe {
		t.Errorf("expected safe layer after TTL, got %v", third.Layer)
	}
	if v := Violations(in, third); len(v) != 0 {
		t.Errorf("violations: %v", v)
	}
}

func TestPlannerTimeoutFallsBackToSafe(t *testing.T) {
	cfg := testConfig()
	cfg.Timeout = 20 * time.Millisecond
	pl := New(cfg).WithNormal(func(c Config, in Input) Plan {
		time.Sleep(300 * time.Millisecond)
		return PlanNormal(c, in)
	})
	p := pl.Plan(validInput())
	if p.Layer != LayerSafe {
		t.Errorf("layer = %v, want safe", p.Layer)
	}
	if !strings.Contains(strings.Join(p.Notes, " "), "timeout") {
		t.Errorf("notes should mention the timeout: %v", p.Notes)
	}
}

func TestPlannerInvalidInputGivesZeroPlan(t *testing.T) {
	in := validInput()
	in.Chargers = append(in.Chargers, testCharger("C1")) // duplicate ID
	p := New(testConfig()).Plan(in)
	if p.Layer != LayerSafe {
		t.Errorf("layer = %v", p.Layer)
	}
	for _, s := range p.Setpoints {
		if s.KW != 0 {
			t.Errorf("invalid input must command 0 kW, got %+v", s)
		}
	}
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("violations: %v", v)
	}
}

func TestPlannerTwoBusesOnOneChargerDoesNotPanic(t *testing.T) {
	in := validInput()
	in.Buses = append(in.Buses, testBus("B2", "C1", 50, 200, 300))
	p := New(testConfig()).Plan(in)
	for _, s := range p.Setpoints {
		if s.KW != 0 {
			t.Errorf("unexpected power: %+v", s)
		}
	}
}

func TestPlannerEmptyInput(t *testing.T) {
	p := New(testConfig()).Plan(Input{Site: model.Site{LimitKW: 100, StepMin: 1}})
	if len(p.Setpoints) != 0 || p.Layer != LayerNormal {
		t.Errorf("unexpected plan: %+v", p)
	}
}

func TestPlannerMostlyUnreliableSoCUsesSafeProfile(t *testing.T) {
	in := validInput()
	in.Buses[0].SoCAgeMin = 100
	p := New(testConfig()).Plan(in)
	if p.Layer != LayerSafe {
		t.Errorf("layer = %v, want safe", p.Layer)
	}
}

func TestPlannerEnforcesBrokenNormalLayer(t *testing.T) {
	pl := New(testConfig()).WithNormal(func(c Config, in Input) Plan {
		return Plan{Setpoints: []Setpoint{{"C1", 99999}}}
	})
	in := validInput()
	p := pl.Plan(in)
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("verifier must repair the plan: %v", v)
	}
	if sp(p, "C1") != 150 {
		t.Errorf("setpoint = %v, want clamped to 150", sp(p, "C1"))
	}
}
