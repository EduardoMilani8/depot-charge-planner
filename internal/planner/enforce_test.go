package planner

import (
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
