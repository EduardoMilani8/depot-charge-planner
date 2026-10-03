package planner

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func twoChargerInput(limit float64) Input {
	return Input{
		Now:      0,
		Site:     model.Site{LimitKW: limit, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
	}
}

func TestEnforceKeepsValidPlan(t *testing.T) {
	in := twoChargerInput(200)
	p := Plan{Setpoints: []Setpoint{{"C1", 80}, {"C2", 100}}}
	if v := Violations(in, p); len(v) != 0 {
		t.Fatalf("unexpected violations: %v", v)
	}
	if got := Enforce(in, p); !reflect.DeepEqual(got, p) {
		t.Errorf("valid plan must be returned unchanged: %+v", got)
	}
}

func TestEnforceScalesDownOverLimit(t *testing.T) {
	in := twoChargerInput(100)
	p := Plan{Setpoints: []Setpoint{{"C1", 100}, {"C2", 100}}}
	if len(Violations(in, p)) == 0 {
		t.Fatal("expected a violation")
	}
	got := Enforce(in, p)
	if !near(sp(got, "C1"), 50) || !near(sp(got, "C2"), 50) {
		t.Errorf("expected 50/50, got %+v", got.Setpoints)
	}
	if v := Violations(in, got); len(v) != 0 {
		t.Errorf("enforced plan still violates: %v", v)
	}
	if len(got.Notes) == 0 {
		t.Error("a correction must leave a note")
	}
	if p.Setpoints[0].KW != 100 {
		t.Error("Enforce must not mutate its input")
	}
}

func TestEnforceClampsToChargerMax(t *testing.T) {
	in := twoChargerInput(1000)
	got := Enforce(in, Plan{Setpoints: []Setpoint{{"C1", 400}}})
	if sp(got, "C1") != 150 {
		t.Errorf("got %v, want 150", sp(got, "C1"))
	}
}

func TestEnforceZeroesBelowFloor(t *testing.T) {
	in := twoChargerInput(1000)
	got := Enforce(in, Plan{Setpoints: []Setpoint{{"C1", 3}}})
	if sp(got, "C1") != 0 {
		t.Errorf("got %v, want 0 (below 5 kW floor)", sp(got, "C1"))
	}
}

func TestEnforceDropsUnknownDuplicateInvalidAndUnhealthy(t *testing.T) {
	in := twoChargerInput(1000)
	in.Chargers[1].Status = model.ChargerFaulted
	p := Plan{Setpoints: []Setpoint{
		{"X", 50}, {"C1", 60}, {"C1", 70}, {"C2", 80},
	}}
	got := Enforce(in, p)
	if len(got.Setpoints) != 1 || got.Setpoints[0].ChargerID != "C1" || got.Setpoints[0].KW != 60 {
		t.Errorf("unexpected setpoints: %+v", got.Setpoints)
	}
	bad := Plan{Setpoints: []Setpoint{{"C1", math.NaN()}}}
	if got := Enforce(in, bad); len(got.Setpoints) != 0 {
		t.Errorf("NaN setpoint must be dropped: %+v", got.Setpoints)
	}
	neg := Plan{Setpoints: []Setpoint{{"C1", -5}}}
	if got := Enforce(in, neg); len(got.Setpoints) != 0 {
		t.Errorf("negative setpoint must be dropped: %+v", got.Setpoints)
	}
}

func TestEnforceReservesOfflineDraw(t *testing.T) {
	in := twoChargerInput(100)
	in.Chargers[0].Status = model.ChargerOffline
	in.Chargers[0].LastCommandedKW = 60
	got := Enforce(in, Plan{Setpoints: []Setpoint{{"C2", 80}}})
	if !near(sp(got, "C2"), 40) {
		t.Errorf("C2 = %v, want 40", sp(got, "C2"))
	}
}

func TestEnforceNaNLimitMeansZero(t *testing.T) {
	in := twoChargerInput(math.NaN())
	got := Enforce(in, Plan{Setpoints: []Setpoint{{"C1", 100}}})
	if sp(got, "C1") != 0 {
		t.Errorf("got %v, want 0", sp(got, "C1"))
	}
}

// Found by FuzzPlannerRespectsInvariants: a charger with a broken (negative) MaxKW
// must be commanded to 0 kW, and 0 kW must not be reported as "above the ceiling".
func TestZeroSetpointIsAlwaysLegal(t *testing.T) {
	bad := testCharger("C1")
	bad.MaxKW = -64
	in := Input{Site: model.Site{LimitKW: 100, StepMin: 1}, Chargers: []model.Charger{bad}}
	if v := Violations(in, Plan{Setpoints: []Setpoint{{"C1", 0}}}); len(v) != 0 {
		t.Errorf("0 kW must be legal: %v", v)
	}
	if v := Violations(in, Plan{Setpoints: []Setpoint{{"C1", 10}}}); len(v) == 0 {
		t.Error("a positive setpoint above a negative ceiling must still be a violation")
	}
	got := Enforce(in, Plan{Setpoints: []Setpoint{{"C1", 10}}})
	if kw := sp(got, "C1"); kw != 0 {
		t.Errorf("Enforce must switch the broken charger off, got %v", kw)
	}
	if v := Violations(in, got); len(v) != 0 {
		t.Errorf("enforced plan still violates: %v", v)
	}
}

func nanCharger(id string, maxNaN, minNaN bool) model.Charger {
	c := testCharger(id)
	if maxNaN {
		c.MaxKW = math.NaN()
	}
	if minNaN {
		c.MinKW = math.NaN()
	}
	return c
}

// A NaN ceiling or floor makes every ordinary comparison false, so a positive
// setpoint on such a charger must be rejected explicitly and switched off.
func TestNaNChargerSpecIsAViolationAndEnforcedOff(t *testing.T) {
	for name, c := range map[string]model.Charger{
		"NaN MaxKW": nanCharger("C1", true, false),
		"NaN MinKW": nanCharger("C1", false, true),
	} {
		t.Run(name, func(t *testing.T) {
			in := Input{Site: model.Site{LimitKW: 100, StepMin: 1}, Chargers: []model.Charger{c}}
			p := Plan{Setpoints: []Setpoint{{"C1", 10}}}
			if len(Violations(in, p)) == 0 {
				t.Fatal("10 kW on a charger with a NaN bound must be a violation")
			}
			got := Enforce(in, p)
			if v := Violations(in, got); len(v) != 0 {
				t.Errorf("enforced plan still violates: %v", v)
			}
			if kw := sp(got, "C1"); kw != 0 {
				t.Errorf("expected 0 kW, got %v", kw)
			}
			if v := Violations(in, Plan{Setpoints: []Setpoint{{"C1", 0}}}); len(v) != 0 {
				t.Errorf("0 kW must stay legal: %v", v)
			}
		})
	}
}

// Isolates the Enforce clamp: the ceiling is negative but the floor is not above it,
// so Violations alone flags the ceiling; and a NaN charger next to an over-limit one.
func TestEnforceClampsBrokenCeilings(t *testing.T) {
	neg := testCharger("C1")
	neg.MaxKW, neg.MinKW = -64, -100
	in := Input{Site: model.Site{LimitKW: 100, StepMin: 1}, Chargers: []model.Charger{neg}}
	got := Enforce(in, Plan{Setpoints: []Setpoint{{"C1", 10}}})
	if kw := sp(got, "C1"); kw != 0 {
		t.Errorf("negative ceiling: expected 0 kW, got %v", kw)
	}
	if v := Violations(in, got); len(v) != 0 {
		t.Errorf("negative ceiling: still violates: %v", v)
	}

	in = Input{Site: model.Site{LimitKW: 100, StepMin: 1},
		Chargers: []model.Charger{nanCharger("C1", true, false), testCharger("C2")}}
	got = Enforce(in, Plan{Setpoints: []Setpoint{{"C1", 10}, {"C2", 400}}})
	for _, s := range got.Setpoints {
		if !finite(s.KW) {
			t.Errorf("non-finite setpoint %+v", s)
		}
	}
	if v := Violations(in, got); len(v) != 0 {
		t.Errorf("mixed plan still violates: %v (%+v)", v, got.Setpoints)
	}
	if kw := sp(got, "C1"); kw != 0 {
		t.Errorf("NaN charger must be 0 kW, got %v", kw)
	}
	if kw := sp(got, "C2"); !near(kw, 100) {
		// C2 is capped at its 150 kW max and then scaled to the 100 kW budget
		// (total before scaling is 0 + 150).
		t.Errorf("C2: expected 100 kW, got %v", kw)
	}
}

// Scaling a plan down to a huge limit must not leave a rounding excess that the
// verifier then reports (the absolute 1e-6 kW tolerance is below one ulp of 1e8).
func TestEnforceHugeLimitsSurviveRounding(t *testing.T) {
	failures := 0
	for _, limit := range []float64{1e7, 1e8 / 3, 1e8, 7.77e8, 1e9, 1e12} {
		for n := 2; n <= 40; n++ {
			in := Input{Site: model.Site{LimitKW: limit, StepMin: 1}}
			var p Plan
			asked := 0.0
			for i := 0; i < n; i++ {
				c := testCharger(fmt.Sprintf("C%02d", i))
				c.MaxKW = 1e13
				in.Chargers = append(in.Chargers, c)
				p.Setpoints = append(p.Setpoints, Setpoint{c.ID, limit / 3 * (1 + float64(i)/7)})
				asked += p.Setpoints[i].KW
			}
			got := Enforce(in, p)
			if v := Violations(in, got); len(v) != 0 {
				failures++
				if failures < 5 {
					t.Errorf("limit %g, %d chargers: %v", limit, n, v)
				}
			}
			total := 0.0
			for _, s := range got.Setpoints {
				total += s.KW
			}
			if total > limit {
				t.Errorf("limit %g, %d chargers: enforced total %v exceeds the limit", limit, n, total)
			}
			if asked > limit && total < limit*(1-1e-9) {
				t.Errorf("limit %g, %d chargers: enforced total %v wastes power", limit, n, total)
			}
		}
	}
}
