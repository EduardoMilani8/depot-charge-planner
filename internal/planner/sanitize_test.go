package planner

import (
	"math"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// depotWithGoodBus has a healthy pair B1/C1 that must keep charging whatever else is
// wrong in the input, plus two spare chargers.
func depotWithGoodBus() Input {
	return Input{
		Site:     model.Site{LimitKW: 500, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2"), testCharger("C3")},
		Buses:    []model.Bus{testBus("B1", "C1", 100, 220, 300)},
	}
}

func assertGoodBusStillPlanned(t *testing.T, in Input, p Plan) {
	t.Helper()
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("violations: %v", v)
	}
	if p.Layer != LayerNormal {
		t.Errorf("layer = %v, want normal (one bad record must not switch the depot off); notes %v", p.Layer, p.Notes)
	}
	if sp(p, "C1") <= 0 {
		t.Errorf("the good bus B1 lost its power: %+v", p.Setpoints)
	}
	if st := statusOf(p, "B1"); !st.Assessed || !st.WillReachTarget {
		t.Errorf("B1 must still be planned normally: %+v", st)
	}
}

func TestPlannerOneBadBusAmongGoodOnes(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Input)
		badID  string
	}{
		"zero capacity": {func(in *Input) {
			b := testBus("X", "C2", 100, 200, 300)
			b.CapacityKWh = 0
			in.Buses = append(in.Buses, b)
		}, "X"},
		"NaN target": {func(in *Input) {
			b := testBus("X", "C2", 100, 200, 300)
			b.TargetKWh = math.NaN()
			in.Buses = append(in.Buses, b)
		}, "X"},
		"Inf battery power": {func(in *Input) {
			b := testBus("X", "C2", 100, 200, 300)
			b.MaxBatteryKW = math.Inf(1)
			in.Buses = append(in.Buses, b)
		}, "X"},
		"unknown charger": {func(in *Input) {
			in.Buses = append(in.Buses, testBus("X", "NOPE", 100, 200, 300))
		}, "X"},
		"two buses on one charger": {func(in *Input) {
			in.Buses = append(in.Buses, testBus("X", "C2", 100, 200, 300), testBus("Y", "C2", 100, 200, 300))
		}, "X"},
		"duplicate bus ID": {func(in *Input) {
			in.Buses = append(in.Buses, testBus("X", "C2", 100, 200, 300), testBus("X", "C3", 100, 200, 300))
		}, "X"},
		"empty bus ID": {func(in *Input) {
			in.Buses = append(in.Buses, testBus("", "C2", 100, 200, 300))
		}, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			in := depotWithGoodBus()
			c.mutate(&in)
			p := New(testConfig()).Plan(in)
			assertGoodBusStillPlanned(t, in, p)
			st := statusOf(p, c.badID)
			if st.BusID != c.badID || !strings.Contains(st.Reason, "registro inválido") || st.WillReachTarget {
				t.Errorf("bad bus %q needs a 'registro inválido' reason: %+v", c.badID, st)
			}
			if sp(p, "C2") != 0 || sp(p, "C3") != 0 {
				t.Errorf("chargers of dropped records must stay at 0 kW: %+v", p.Setpoints)
			}
			if len(p.Notes) == 0 {
				t.Error("dropping records must leave a note")
			}
		})
	}
}

// A bus record claiming a charger another bus is on: the planner cannot know which
// bus is really plugged in, so both records are dropped and that charger stays off,
// while the rest of the depot is planned normally.
func TestPlannerBusPreAssignedToOccupiedCharger(t *testing.T) {
	in := depotWithGoodBus()
	in.Buses = append(in.Buses, testBus("B3", "C3", 100, 220, 300), testBus("X", "C3", 50, 200, 300))
	p := New(testConfig()).Plan(in)
	assertGoodBusStillPlanned(t, in, p)
	if sp(p, "C3") != 0 {
		t.Errorf("contested C3 must be off, got %v", sp(p, "C3"))
	}
	for _, id := range []string{"B3", "X"} {
		if st := statusOf(p, id); !strings.Contains(st.Reason, "registro inválido") || !strings.Contains(st.Reason, "C3") {
			t.Errorf("%s needs a reason naming the contested charger: %+v", id, st)
		}
	}
}

func TestPlannerOneBadChargerAmongGoodOnes(t *testing.T) {
	cases := map[string]func(*model.Charger){
		"NaN max":            func(c *model.Charger) { c.MaxKW = math.NaN() },
		"zero max":           func(c *model.Charger) { c.MaxKW = 0 },
		"floor above max":    func(c *model.Charger) { c.MinKW = 500 },
		"zero efficiency":    func(c *model.Charger) { c.Efficiency = 0 },
		"efficiency above 1": func(c *model.Charger) { c.Efficiency = 1.5 },
		"negative last":      func(c *model.Charger) { c.LastCommandedKW = -1 },
		"unknown status":     func(c *model.Charger) { c.Status = model.ChargerStatus(42) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := depotWithGoodBus()
			mutate(&in.Chargers[1])
			in.Buses = append(in.Buses, testBus("B2", "C2", 100, 220, 300))
			p := New(testConfig()).Plan(in)
			assertGoodBusStillPlanned(t, in, p)
			if sp(p, "C2") != 0 {
				t.Errorf("bad charger C2 must get 0 kW, got %v", sp(p, "C2"))
			}
			st := statusOf(p, "B2")
			if !strings.Contains(st.Reason, "C2") || !strings.Contains(st.Reason, "registro inválido") || st.WillReachTarget {
				t.Errorf("bus on the bad charger needs a reason naming it: %+v", st)
			}
		})
	}
	t.Run("duplicated ID", func(t *testing.T) {
		in := depotWithGoodBus()
		in.Chargers = append(in.Chargers, testCharger("C2"))
		in.Buses = append(in.Buses, testBus("B2", "C2", 100, 220, 300))
		p := New(testConfig()).Plan(in)
		assertGoodBusStillPlanned(t, in, p)
		if sp(p, "C2") != 0 || !strings.Contains(statusOf(p, "B2").Reason, "registro inválido") {
			t.Errorf("duplicated C2 must be off with a reason: %+v / %+v", p.Setpoints, statusOf(p, "B2"))
		}
	})
	t.Run("empty ID", func(t *testing.T) {
		in := depotWithGoodBus()
		in.Chargers[1].ID = ""
		assertGoodBusStillPlanned(t, in, New(testConfig()).Plan(in))
	})
}

// An offline charger with a broken spec still draws its last power: dropping it must
// not free that power for the other chargers.
func TestPlannerBadOfflineChargerStillReservesItsDraw(t *testing.T) {
	in := depotWithGoodBus()
	in.Site.LimitKW = 100
	in.Chargers[1].Status = model.ChargerOffline
	in.Chargers[1].LastCommandedKW = 60
	in.Chargers[1].Efficiency = math.NaN()
	in.Buses[0] = testBus("B1", "C1", 0, 240, 100) // wants everything
	p := New(testConfig()).Plan(in)
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("violations: %v", v)
	}
	if kw := sp(p, "C1"); !near(kw, 40) {
		t.Errorf("C1 = %v, want 40 (100 minus the offline 60)", kw)
	}
}

func TestPlannerInvalidSiteGivesZeroPlan(t *testing.T) {
	for name, mutate := range map[string]func(*Input){
		"NaN limit":      func(in *Input) { in.Site.LimitKW = math.NaN() },
		"negative limit": func(in *Input) { in.Site.LimitKW = -1 },
		"zero step":      func(in *Input) { in.Site.StepMin = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			in := validInput()
			mutate(&in)
			p := New(testConfig()).Plan(in)
			if p.Layer != LayerSafe {
				t.Errorf("layer = %v", p.Layer)
			}
			for _, s := range p.Setpoints {
				if s.KW != 0 {
					t.Errorf("invalid site must command 0 kW, got %+v", s)
				}
			}
			if v := Violations(in, p); len(v) != 0 {
				t.Errorf("violations: %v", v)
			}
		})
	}
}
