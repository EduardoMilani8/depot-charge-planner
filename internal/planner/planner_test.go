package planner

import (
	"math"
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

func TestPlannerTimeoutGoroutineDoesNotRaceWithCaller(t *testing.T) {
	cfg := testConfig()
	cfg.Timeout = 20 * time.Millisecond
	var sink float64
	done := make(chan struct{})
	pl := New(cfg).WithNormal(func(c Config, in Input) Plan {
		time.Sleep(100 * time.Millisecond)
		sink = in.Buses[0].SoCKWh + in.Chargers[0].MaxKW // read after the caller has moved on
		close(done)
		return PlanNormal(c, in)
	})
	in := validInput()
	if p := pl.Plan(in); p.Layer != LayerSafe {
		t.Fatalf("layer = %v, want safe", p.Layer)
	}
	// The simulator reuses its slices on the next tick.
	in.Buses[0].SoCKWh = 1
	in.Chargers[0].MaxKW = 2
	select { // let the abandoned goroutine finish its reads
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("abandoned goroutine never finished")
	}
	_ = sink
	time.Sleep(50 * time.Millisecond)
}

func TestPlannerCachedPlanIsIsolatedFromCallers(t *testing.T) {
	calls := 0
	pl := New(testConfig()).WithNormal(func(c Config, in Input) Plan {
		calls++
		if calls > 1 {
			panic("boom")
		}
		return PlanNormal(c, in)
	})
	in := validInput()
	a := pl.Plan(in)
	orig := sp(a, "C1")
	if a.Layer != LayerNormal || orig <= 0 || orig == 1 {
		t.Fatalf("unexpected first plan: %+v", a)
	}
	a.Setpoints[0].KW = 1 // caller mutates the returned plan
	a.Buses[0].Reason = "mutated"

	in.Now = 1
	b := pl.Plan(in) // panics -> last-valid from cache
	if b.Layer != LayerLastValid {
		t.Fatalf("layer = %v, want last-valid", b.Layer)
	}
	if sp(b, "C1") != orig {
		t.Errorf("cached setpoint changed: got %v, want %v", sp(b, "C1"), orig)
	}
	if b.Buses[0].Reason == "mutated" {
		t.Errorf("cached bus status shares memory with the caller")
	}
	b.Setpoints[0].KW = 2 // mutating the last-valid plan must not corrupt the cache either
	in.Now = 2
	c := pl.Plan(in)
	if c.Layer != LayerLastValid || sp(c, "C1") != orig {
		t.Errorf("cache corrupted by caller: %+v", c)
	}
}

// A charger spec that turns NaN between cycles sends the planner down the last-valid
// path; the replayed positive setpoint must still be switched off.
func TestPlannerReplayedPlanOnNaNChargerIsOff(t *testing.T) {
	pl := New(testConfig())
	in := validInput()
	first := pl.Plan(in)
	if first.Layer != LayerNormal || sp(first, "C1") <= 0 {
		t.Fatalf("setup: expected a normal plan with power on C1, got %+v", first)
	}
	in2 := validInput()
	in2.Now = 1
	in2.Chargers[0].MaxKW = math.NaN()
	p := pl.Plan(in2)
	if v := Violations(in2, p); len(v) != 0 {
		t.Errorf("violations: %v (%+v)", v, p.Setpoints)
	}
	if kw := sp(p, "C1"); kw != 0 {
		t.Errorf("C1 must be 0 kW, got %v (layer %v)", kw, p.Layer)
	}
}

func TestPlannerWithNilLoggerDoesNotPanic(t *testing.T) {
	// Exercise paths that log: normal-layer panic (warn) and the verifier correcting a plan.
	pl := New(testConfig()).WithLogger(nil).WithNormal(func(c Config, in Input) Plan {
		if in.Now > 0 {
			panic("boom")
		}
		return Plan{Setpoints: []Setpoint{{"C1", 99999}}}
	})
	in := validInput()
	_ = pl.Plan(in)
	in.Now = 1
	if p := pl.Plan(in); len(Violations(in, p)) != 0 {
		t.Errorf("violations: %v", Violations(in, p))
	}
}

// The reviewer's case: a plan made while C1 was listed once is replayed (last-valid
// or any other path) after a second, contradicting C1 entry appears. The facade must
// never command the ambiguous charger.
func TestPlannerNeverPowersDuplicatedChargerID(t *testing.T) {
	tiny := testCharger("C1")
	tiny.MaxKW, tiny.MinKW = 1, 0
	faulted := testCharger("C1")
	faulted.Status = model.ChargerFaulted
	for name, dup := range map[string]model.Charger{"max 1 kW": tiny, "faulted": faulted, "identical": testCharger("C1")} {
		t.Run(name, func(t *testing.T) {
			pl := New(testConfig())
			in := validInput()
			if p := pl.Plan(in); sp(p, "C1") <= 0 {
				t.Fatalf("setup: C1 should be powered first: %+v", p)
			}
			in.Now = 1
			in.Chargers = append(in.Chargers, dup)
			p := pl.Plan(in)
			if v := Violations(in, p); len(v) != 0 {
				t.Errorf("violations: %v", v)
			}
			if kw := sp(p, "C1"); kw != 0 {
				t.Errorf("duplicated C1 must be at 0 kW, got %v (layer %v)", kw, p.Layer)
			}
		})
	}
}
