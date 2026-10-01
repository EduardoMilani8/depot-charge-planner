package sim

import (
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// twoBusScenario has two buses that together want the whole 300 kW limit.
func twoBusScenario() Scenario {
	sc := baseScenario()
	sc.BaseLimitKW = 300
	sc.Chargers = append(sc.Chargers, model.Charger{ID: "C2", MaxKW: 150, MinKW: 5, Efficiency: 0.95, Status: model.ChargerOK})
	mk := func(id string) BusSpec {
		return BusSpec{Bus: model.Bus{
			ID: id, CapacityKWh: 300, SoCKWh: 0, SoCConfidence: 1, TargetKWh: 240,
			ArrivalMin: 0, DepartureMin: 200, MaxBatteryKW: 150,
		}}
	}
	sc.Buses = []BusSpec{mk("B1"), mk("B2")}
	return sc
}

func TestFaultPermanentChargerFailure(t *testing.T) {
	sc := baseScenario()
	sc.Faults = []Fault{{Kind: FaultChargerFail, Target: "C1", From: 0, To: forever}}
	m := Run(sc, plannerCtrl(sc), nil)
	if m.Ready != 0 || m.ShortfallKWh <= 0 {
		t.Errorf("a bus with no working charger cannot leave ready: %+v", m)
	}
	if m.PlanViolations != 0 {
		t.Errorf("violations: %+v", m)
	}
}

func TestFaultOfflineChargerKeepsDrawingAndPlannerAccountsForIt(t *testing.T) {
	sc := twoBusScenario()
	sc.Faults = []Fault{{Kind: FaultChargerOffline, Target: "C1", From: 50, To: 250}}
	m := Run(sc, plannerCtrl(sc), nil)
	if m.PlanViolations != 0 || m.OvershootMin != 0 {
		t.Errorf("the offline charger's draw must be reserved from the budget: %+v", m)
	}
}

func TestFaultLimitDropCausesOnlyPhysicalOvershoot(t *testing.T) {
	sc := twoBusScenario()
	sc.Faults = []Fault{{Kind: FaultLimitDrop, From: 30, To: 200, Value: 0.5}}
	m := Run(sc, plannerCtrl(sc), nil)
	if m.PlanViolations != 0 {
		t.Errorf("the plan must respect the new limit immediately: %+v", m)
	}
	if m.OvershootMin < 1 {
		t.Errorf("with a 1-step command latency the drop should overshoot at least once: %+v", m)
	}
}

func TestFaultMissingSoCUsesSafeProfileAndStillCharges(t *testing.T) {
	sc := baseScenario()
	sc.Faults = []Fault{{Kind: FaultSoCMissing, Target: "*", From: 0, To: forever}}
	m := Run(sc, plannerCtrl(sc), nil)
	if m.LayerTicks["safe"] == 0 {
		t.Errorf("expected the safe layer to take over: %+v", m.LayerTicks)
	}
	if m.Ready != 1 {
		t.Errorf("the safe profile must still charge the bus: %+v", m)
	}
}

func TestFaultFrozenSoCBecomesUnreliable(t *testing.T) {
	sc := baseScenario()
	sc.Faults = []Fault{{Kind: FaultSoCFreeze, Target: "B1", From: 10, To: 200}}
	m := Run(sc, plannerCtrl(sc), nil)
	if m.LayerTicks["safe"] == 0 {
		t.Errorf("a frozen reading must age into 'unreliable': %+v", m.LayerTicks)
	}
	if m.Ready != 1 {
		t.Errorf("bus should still leave ready: %+v", m)
	}
}

func TestFaultSoCNoiseIsTolerated(t *testing.T) {
	sc := baseScenario()
	sc.Faults = []Fault{{Kind: FaultSoCNoise, Target: "*", From: 0, To: forever, Value: 5}}
	if m := Run(sc, plannerCtrl(sc), nil); m.Ready != 1 || m.PlanViolations != 0 {
		t.Errorf("%+v", m)
	}
}

func TestFaultPlannerPanicFallsBackToLastValidPlan(t *testing.T) {
	sc := baseScenario()
	sc.Faults = []Fault{{Kind: FaultPlannerPanic, From: 10, To: 20}}
	m := Run(sc, plannerCtrl(sc), nil)
	if m.LayerTicks["last-valid"] == 0 {
		t.Errorf("expected last-valid ticks: %+v", m.LayerTicks)
	}
	if m.Ready != 1 || m.PlanViolations != 0 {
		t.Errorf("%+v", m)
	}
}

func TestFaultPlannerSlowTimesOut(t *testing.T) {
	sc := baseScenario()
	sc.Faults = []Fault{{Kind: FaultPlannerSlow, From: 10, To: 12}}
	cfg := planner.DefaultConfig()
	cfg.Timeout = 20 * time.Millisecond
	m := Run(sc, NewPlannerController(cfg, sc), nil)
	if m.LayerTicks["last-valid"]+m.LayerTicks["safe"] == 0 {
		t.Errorf("a timeout must degrade the layer: %+v", m.LayerTicks)
	}
	if m.Ready != 1 {
		t.Errorf("%+v", m)
	}
}

func TestFaultConsumptionAboveForecastLeavesBusShort(t *testing.T) {
	sc := baseScenario()
	sc.Faults = []Fault{{Kind: FaultConsumption, Target: "B1", Value: 100}}
	m := Run(sc, plannerCtrl(sc), nil)
	if m.Ready != 0 || m.ShortfallKWh <= 0 {
		t.Errorf("the planner cannot know the extra consumption: %+v", m)
	}
}

func TestFaultLateArrival(t *testing.T) {
	sc := baseScenario()
	sc.Faults = []Fault{{Kind: FaultLateArrival, Target: "B1", Value: 100}}
	if m := Run(sc, plannerCtrl(sc), nil); m.Ready != 1 {
		t.Errorf("%+v", m)
	}
}

func TestFaultEarlyDepartureIsReportedNotHidden(t *testing.T) {
	sc := baseScenario()
	sc.Faults = []Fault{{Kind: FaultEarlyDeparture, Target: "B1", From: 10, To: forever, Value: 30}}
	m := Run(sc, plannerCtrl(sc), nil)
	if m.Ready != 0 || m.ShortfallKWh <= 0 {
		t.Errorf("30 minutes are not enough to charge 150 kWh: %+v", m)
	}
	if m.PlanViolations != 0 {
		t.Errorf("%+v", m)
	}
}

// recordingController wraps a Controller and keeps a copy of every observation it is
// given (and the swaps it recommends), so tests can inspect what the planner saw.
type recordingController struct {
	inner Controller
	ins   []planner.Input
	swaps int
}

func (r *recordingController) Plan(in planner.Input) planner.Plan {
	cp := in
	cp.Buses = append([]model.Bus(nil), in.Buses...)
	r.ins = append(r.ins, cp)
	p := r.inner.Plan(in)
	r.swaps += len(p.Swaps)
	return p
}

// socSequence flattens the observed SoC of every bus, minute by minute.
func (r *recordingController) socSequence() []float64 {
	var seq []float64
	for _, in := range r.ins {
		for _, b := range in.Buses {
			seq = append(seq, b.SoCKWh)
		}
	}
	return seq
}

// The noise stream belongs to the world, not to the controller: for the same seed,
// two controllers must observe exactly the same readings.
//
// To make the true SoC identical under both controllers, both chargers are failed for
// the whole run: the buses never charge, so the true SoC stays at 0 kWh and every
// observed value is 0 + noise. Any difference between the sequences would therefore
// come from the noise draws depending on the controller. Both buses are present
// during minutes 0..199 (departure 200), so each run has 2 buses * 200 min = 400 readings.
func TestFaultSoCNoiseIdenticalAcrossControllers(t *testing.T) {
	sc := twoBusScenario()
	sc.Faults = []Fault{
		{Kind: FaultChargerFail, Target: "C1", From: 0, To: forever},
		{Kind: FaultChargerFail, Target: "C2", From: 0, To: forever},
		{Kind: FaultSoCNoise, Target: "*", From: 0, To: forever, Value: 5},
	}
	run := func(c Controller) []float64 {
		rc := &recordingController{inner: c}
		Run(sc, rc, nil)
		return rc.socSequence()
	}
	fifo, edf, pl := run(NewFIFO()), run(NewEDF()), run(plannerCtrl(sc))
	if want := 2 * 200; len(fifo) != want {
		t.Fatalf("expected %d readings, got %d", want, len(fifo))
	}
	nonZero := 0
	for i, v := range fifo {
		if v != 0 {
			nonZero++
		}
		if edf[i] != v || pl[i] != v {
			t.Fatalf("reading %d differs across controllers: fifo=%v edf=%v planner=%v", i, v, edf[i], pl[i])
		}
	}
	if nonZero < len(fifo)/2 {
		t.Errorf("noise fault did not perturb the readings (%d of %d non-zero)", nonZero, len(fifo))
	}
}

// No two buses may share a charger and no bus may hold two (a bus has one ChargerID,
// so the second half holds by construction; uniqueness is what can break) while the
// planner's swaps are followed and a charger fails and recovers.
//
// Scenario: 2 chargers (150 kW each), buses B1 (departs at 400, 150 -> 240 kWh, lots of
// slack), B2 and B3 (depart at 200, 0 -> 240 kWh). B1 and B2 take the chargers; B3 waits.
// A bus with 240 kWh to gain needs about 240/0.95/150 h = 101 min of full power, so B3's
// laxity (200 - t - 101 min) drops under SwapUrgentLaxityMin (60) around t = 40, while
// B1's laxity is above 250 min: the gap exceeds SwapDonorGapMin (120), so the planner
// recommends B1 -> B3 on C1, and the world executes it (FollowSwaps). C1 then fails in
// [100, 140) while it holds B3, so the failure window overlaps the swapped state.
// The test requires that the swap actually happened (otherwise it checks nothing).
func TestFaultNoChargerSharedUnderSwapsAndFailures(t *testing.T) {
	sc := twoBusScenario()
	sc.Buses[0].Bus.SoCKWh = 150
	sc.Buses[0].Bus.DepartureMin = 400
	sc.Buses = append(sc.Buses, BusSpec{Bus: model.Bus{
		ID: "B3", CapacityKWh: 300, SoCKWh: 0, SoCConfidence: 1, TargetKWh: 240,
		ArrivalMin: 0, DepartureMin: 200, MaxBatteryKW: 150,
	}})
	sc.Faults = []Fault{{Kind: FaultChargerFail, Target: "C1", From: 100, To: 140}}
	sc.FollowSwaps = true

	rc := &recordingController{inner: plannerCtrl(sc)}
	m := Run(sc, rc, nil)

	moved := false
	last := map[string]string{}
	for _, in := range rc.ins {
		seen := map[string]string{}
		for _, b := range in.Buses {
			if b.ChargerID != "" {
				if other, dup := seen[b.ChargerID]; dup {
					t.Fatalf("minute %d: buses %s and %s share charger %s", in.Now, other, b.ID, b.ChargerID)
				}
				seen[b.ChargerID] = b.ID
			}
			if prev, ok := last[b.ID]; ok && prev != b.ChargerID { // plugged, unplugged or moved
				moved = true
			}
			last[b.ID] = b.ChargerID
		}
	}
	if rc.swaps == 0 || !moved {
		t.Errorf("no swap was recommended/executed, the invariant was not exercised: swaps=%d moved=%v %+v", rc.swaps, moved, m)
	}
}
