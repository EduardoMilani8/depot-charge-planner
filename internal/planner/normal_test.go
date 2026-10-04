package planner

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// justInTimeConfig keeps spare power away from buses with laxity >= 120 min (the
// pre-I3 default), so tests about the just-in-time requirement see it unmodified.
func justInTimeConfig() Config {
	c := testConfig()
	c.SurplusLaxityMin = 120
	return c
}

func oneBus(limit float64, b model.Bus) Input {
	return Input{
		Now:      0,
		Site:     model.Site{LimitKW: limit, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1")},
		Buses:    []model.Bus{b},
	}
}

// With SurplusLaxityMin set (peak flattening), a bus with plenty of laxity only gets
// the power that just meets its deadline.
func TestNormalJustInTime(t *testing.T) {
	// need 120 kWh, 240 min left, laxity 192 min (>= 120): no surplus, just 30 kW.
	cfg := testConfig()
	cfg.SurplusLaxityMin = 120
	p := PlanNormal(cfg, oneBus(1000, testBus("B1", "C1", 100, 220, 240)))
	if !near(sp(p, "C1"), 30) {
		t.Errorf("setpoint = %v, want 30", sp(p, "C1"))
	}
	st := statusOf(p, "B1")
	if !st.WillReachTarget || !strings.Contains(st.Reason, "folga") {
		t.Errorf("unexpected status: %+v", st)
	}
	if p.Layer != LayerNormal {
		t.Errorf("layer = %v", p.Layer)
	}
}

func TestNormalSurplusForLowLaxity(t *testing.T) {
	// 100 min left: laxity 52 min (< 120): required 72 kW, surplus raises it to the max.
	cfg := testConfig()
	cfg.SurplusLaxityMin = 120
	p := PlanNormal(cfg, oneBus(1000, testBus("B1", "C1", 100, 220, 100)))
	if !near(sp(p, "C1"), 150) {
		t.Errorf("setpoint = %v, want 150", sp(p, "C1"))
	}
}

// Default (spec §6.4, ruling I3): all spare power is spent, least laxity first, even
// on buses with lots of laxity: readiness first, peak flattening second.
func TestNormalDefaultSpendsAllSparePowerInLaxityOrder(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 200, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
		Buses: []model.Bus{
			testBus("A", "C1", 100, 220, 600), // laxity 552, required 12 kW
			testBus("B", "C2", 100, 220, 400), // laxity 352, required 18 kW
		},
	}
	p := PlanNormal(testConfig(), in)
	// Both get their requirement (30 kW), the 170 kW left go to B first (up to its
	// 150 kW max: +132), then the remaining 38 kW to A.
	if !near(sp(p, "C2"), 150) || !near(sp(p, "C1"), 50) {
		t.Errorf("got A=%v B=%v, want 50/150", sp(p, "C1"), sp(p, "C2"))
	}
}

func TestNormalScarceBudgetPrioritisesLeastLaxity(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 160, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
		Buses: []model.Bus{
			testBus("A", "C1", 100, 220, 60),  // laxity 12, required 120
			testBus("B", "C2", 100, 220, 300), // laxity 252, required 24
		},
	}
	p := PlanNormal(testConfig(), in)
	if !near(sp(p, "C1"), 136) || !near(sp(p, "C2"), 24) {
		t.Errorf("got A=%v B=%v, want 136/24", sp(p, "C1"), sp(p, "C2"))
	}
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("plan violates invariants: %v", v)
	}
}

func TestNormalDoomedBusDoesNotStarveSavableOne(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 150, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
		Buses: []model.Bus{
			testBus("A", "C1", 0, 240, 60),    // needs 96 min at max, has 60: doomed
			testBus("B", "C2", 100, 220, 300), // savable, required 24
		},
	}
	p := PlanNormal(testConfig(), in)
	if !near(sp(p, "C2"), 24) || !near(sp(p, "C1"), 126) {
		t.Errorf("got A=%v B=%v, want 126/24", sp(p, "C1"), sp(p, "C2"))
	}
	a := statusOf(p, "A")
	if a.WillReachTarget || a.ShortfallKWh <= 0 || !strings.Contains(a.Reason, "inviável") {
		t.Errorf("A should be flagged infeasible with shortfall: %+v", a)
	}
	if !statusOf(p, "B").WillReachTarget {
		t.Error("B must reach its target")
	}
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("plan violates invariants: %v", v)
	}
}

func TestNormalBatteryLimitBelowChargerFloor(t *testing.T) {
	b := testBus("B1", "C1", 100, 220, 300)
	b.MaxBatteryKW = 3 // charger floor is 5 kW
	p := PlanNormal(testConfig(), oneBus(1000, b))
	if sp(p, "C1") != 0 {
		t.Errorf("setpoint = %v, want 0", sp(p, "C1"))
	}
	if st := statusOf(p, "B1"); st.WillReachTarget || !strings.Contains(st.Reason, "bateria") {
		t.Errorf("unexpected status: %+v", st)
	}
}

func TestNormalUnreliableSoCIsConservative(t *testing.T) {
	b := testBus("B1", "C1", 200, 220, 300)
	b.SoCAgeMin = 100 // stale: 200 - 10% of 300 = 170, need 50 kWh over 300 min
	p := PlanNormal(justInTimeConfig(), oneBus(1000, b))
	if !near(sp(p, "C1"), 10) {
		t.Errorf("setpoint = %v, want 10", sp(p, "C1"))
	}
	if !strings.Contains(statusOf(p, "B1").Reason, "não confiável") {
		t.Errorf("reason should mention unreliable reading: %q", statusOf(p, "B1").Reason)
	}
}

func TestNormalAbsurdSoCReadingsAssumeEmptyBattery(t *testing.T) {
	for _, soc := range []float64{math.NaN(), -10, 500} {
		b := testBus("B1", "C1", soc, 150, 300) // target 150 from 0, 300 min: 30 kW
		p := PlanNormal(justInTimeConfig(), oneBus(1000, b))
		if !near(sp(p, "C1"), 30) {
			t.Errorf("soc %v: setpoint = %v, want 30", soc, sp(p, "C1"))
		}
	}
}

func TestNormalOfflineChargerReservesBudget(t *testing.T) {
	in := Input{
		Site: model.Site{LimitKW: 100, StepMin: 1},
		Chargers: []model.Charger{
			{ID: "C1", MaxKW: 150, MinKW: 5, Efficiency: 1, Status: model.ChargerOffline, LastCommandedKW: 60},
			testCharger("C2"),
		},
		Buses: []model.Bus{
			testBus("B1", "C1", 100, 220, 300),
			testBus("B2", "C2", 0, 240, 100),
		},
	}
	p := PlanNormal(testConfig(), in)
	if !near(sp(p, "C2"), 40) {
		t.Errorf("C2 = %v, want 40 (limit 100 minus 60 offline draw)", sp(p, "C2"))
	}
	if sp(p, "C1") != 0 || !strings.Contains(statusOf(p, "B1").Reason, "indisponível") {
		t.Errorf("offline charger must not be commanded: %+v", p)
	}
}

func TestNormalDepartureNotInTheFuture(t *testing.T) {
	p := PlanNormal(testConfig(), oneBus(1000, testBus("B1", "C1", 100, 220, 0)))
	kw := sp(p, "C1")
	if math.IsNaN(kw) || math.IsInf(kw, 0) || kw > 150 {
		t.Errorf("setpoint must be finite and within the charger max, got %v", kw)
	}
	if st := statusOf(p, "B1"); st.WillReachTarget || st.ShortfallKWh <= 0 {
		t.Errorf("bus past its departure must be infeasible: %+v", st)
	}
}

func TestNormalEmptyAndZeroLimit(t *testing.T) {
	if p := PlanNormal(testConfig(), Input{}); len(p.Setpoints) != 0 {
		t.Errorf("empty input: %+v", p)
	}
	p := PlanNormal(testConfig(), oneBus(0, testBus("B1", "C1", 100, 220, 100)))
	if sp(p, "C1") != 0 {
		t.Errorf("zero limit: setpoint %v", sp(p, "C1"))
	}
	if st := statusOf(p, "B1"); !strings.Contains(st.Reason, "potência") {
		t.Errorf("reason should explain the lack of power: %+v", st)
	}
}

func TestNormalBusAlreadyAtTarget(t *testing.T) {
	p := PlanNormal(testConfig(), oneBus(1000, testBus("B1", "C1", 230, 220, 300)))
	if sp(p, "C1") != 0 || !statusOf(p, "B1").WillReachTarget {
		t.Errorf("unexpected plan: %+v", p)
	}
}

func TestNormalIsDeterministicAndPure(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 200, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2"), testCharger("C3")},
		Buses: []model.Bus{
			testBus("B3", "C3", 90, 220, 200),
			testBus("B1", "C1", 100, 220, 200),
			testBus("B2", "C2", 100, 220, 200),
		},
	}
	a := PlanNormal(testConfig(), in)
	b := PlanNormal(testConfig(), in)
	if !reflect.DeepEqual(a, b) {
		t.Error("same input must give the same plan")
	}
	if in.Buses[0].ID != "B3" {
		t.Error("input must not be reordered or mutated")
	}
	if v := Violations(in, a); len(v) != 0 {
		t.Errorf("normal plan violates invariants: %v", v)
	}
}

func TestNormalUnsavableBusDoesNotStarveSavableOne(t *testing.T) {
	// limit 100. A: need 120 kWh in 60 min (laxity 12) but 100 kW for 60 min deliver
	// only 100 kWh: the budget cannot finish it. B: need 120 kWh in 300 min, required
	// 24 kW. Admission (R1) leaves A out, so B is served first: its 24 kW plus the
	// spare power up to the budget (100 kW), and A gets only what B cannot use (none).
	// (Before R1, B got exactly 24 kW and A the other 76 kW.)
	in := Input{
		Site:     model.Site{LimitKW: 100, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
		Buses: []model.Bus{
			testBus("A", "C1", 100, 220, 60),
			testBus("B", "C2", 100, 220, 300),
		},
	}
	p := PlanNormal(testConfig(), in)
	if !near(sp(p, "C2"), 100) || sp(p, "C1") != 0 {
		t.Errorf("got A=%v B=%v, want 0/100", sp(p, "C1"), sp(p, "C2"))
	}
	if !statusOf(p, "B").WillReachTarget {
		t.Errorf("B must reach its target: %+v", statusOf(p, "B"))
	}
	a := statusOf(p, "A")
	if a.WillReachTarget || !near(a.ShortfallKWh, 120) {
		t.Errorf("A must miss its target by 120 kWh: %+v", a)
	}
	if !strings.Contains(a.Reason, "priorizados") || strings.Contains(a.Reason, "folga") {
		t.Errorf("A's reason must say the buses that can finish come first, not slack: %q", a.Reason)
	}
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("plan violates invariants: %v", v)
	}
}

func TestNormalLeftoverBelowChargerFloorGivesZero(t *testing.T) {
	// limit 27. A and B both need 120 kWh in 300 min -> required 24 kW, equal laxity,
	// order A, B. A takes 24, 3 kW are left (< floor 5): B must get exactly 0.
	in := Input{
		Site:     model.Site{LimitKW: 27, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
		Buses: []model.Bus{
			testBus("A", "C1", 100, 220, 300),
			testBus("B", "C2", 100, 220, 300),
		},
	}
	p := PlanNormal(justInTimeConfig(), in)
	if !near(sp(p, "C1"), 24) || sp(p, "C2") != 0 {
		t.Errorf("got A=%v B=%v, want 24/0", sp(p, "C1"), sp(p, "C2"))
	}
	if st := statusOf(p, "B"); st.WillReachTarget || !strings.Contains(st.Reason, "potência") {
		t.Errorf("B must be reported as unpowered: %+v", st)
	}
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("plan violates invariants: %v", v)
	}
}

func TestNormalTinyRequirementIsRaisedToChargerFloor(t *testing.T) {
	// need 1 kWh in 300 min -> 0.2 kW, below the 5 kW floor: setpoint is exactly 5.
	in := oneBus(1000, testBus("B1", "C1", 100, 101, 300))
	p := PlanNormal(justInTimeConfig(), in)
	if sp(p, "C1") != 5 {
		t.Errorf("setpoint = %v, want exactly 5", sp(p, "C1"))
	}
	if !statusOf(p, "B1").WillReachTarget {
		t.Errorf("B1 must reach its target: %+v", statusOf(p, "B1"))
	}
	if v := Violations(in, p); len(v) != 0 {
		t.Errorf("plan violates invariants: %v", v)
	}
}

// A laxity a hair below zero (float rounding) with full power still delivering the
// need: the bus will reach its target and must not be called infeasible.
func TestNormalEpsilonNegativeLaxityIsNotInfeasible(t *testing.T) {
	b := testBus("B1", "C1", 100, 250+1e-9, 60)
	b.CapacityKWh = 400 // keep the target below the taper knee
	p := PlanNormal(testConfig(), oneBus(1000, b))
	st := statusOf(p, "B1")
	if !st.WillReachTarget {
		t.Fatalf("B1 should reach its target: %+v", st)
	}
	if strings.Contains(st.Reason, "inviável") || strings.Contains(st.Reason, "faltam") {
		t.Errorf("reason contradicts WillReachTarget: %q", st.Reason)
	}
}

// R1, spec §6.5/§12: under overload the planner must maximise the number of buses
// that reach their target, not spread power by laxity. 150 kW for 2 h is 300 kWh; A
// needs 200 kWh (least laxity), B and C 120 kWh each. Least laxity first finishes only
// A; admitting the buses that fit (B and C) finishes two.
func TestNormalOverloadAdmitsTheBusesThatCanFinish(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 150, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2"), testCharger("C3")},
		Buses: []model.Bus{
			testBus("A", "C1", 0, 200, 120),   // 200 kWh, laxity 40 min
			testBus("B", "C2", 100, 220, 120), // 120 kWh, laxity 72 min
			testBus("C", "C3", 100, 220, 120), // 120 kWh, laxity 72 min
		},
	}
	p := PlanNormal(testConfig(), in)
	if v := Violations(in, p); len(v) != 0 {
		t.Fatalf("violations: %v", v)
	}
	for _, id := range []string{"B", "C"} {
		if st := statusOf(p, id); !st.WillReachTarget {
			t.Errorf("%s fits in the budget with the other short job and must be planned to finish: %+v", id, st)
		}
	}
	if st := statusOf(p, "A"); st.WillReachTarget || sp(p, "C1") != 0 || !strings.Contains(st.Reason, "priorizados") {
		t.Errorf("A cannot finish together with B and C and must not take their power: A=%.1f kW %+v", sp(p, "C1"), st)
	}
	if !near(sp(p, "C2")+sp(p, "C3"), 150) {
		t.Errorf("all the budget must still be spent: B=%v C=%v", sp(p, "C2"), sp(p, "C3"))
	}
}

// Admission only matters under overload: when every bus fits, nobody is starved.
func TestNormalNoOverloadKeepsEveryBus(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 300, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2"), testCharger("C3")},
		Buses: []model.Bus{
			testBus("A", "C1", 0, 200, 120),
			testBus("B", "C2", 100, 220, 120),
			testBus("C", "C3", 100, 220, 300),
		},
	}
	p := PlanNormal(testConfig(), in)
	for _, id := range []string{"A", "B", "C"} {
		if st := statusOf(p, id); !st.WillReachTarget {
			t.Errorf("%s must reach its target: %+v", id, st)
		}
	}
}
