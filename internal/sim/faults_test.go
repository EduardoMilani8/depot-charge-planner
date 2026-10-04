package sim

import (
	"math"
	"strings"
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

// Scenario: two 150 kW chargers under a 250 kW limit (so the chargers alone can exceed
// the limit: 2*150 = 300 > 250), two buses that want full power (240 kWh in 150 min needs
// 240/0.95/150 h = 101 min of full power, laxity 49 min, required power above the 150 kW
// charger max). Least-laxity-first with ID tie-break gives B1 (on C1) about 150 kW
// (147.2 kW observed at minute 50) and B2 (on C2) the rest. When C1 goes offline at minute
// 50 it keeps drawing its last ~150 kW, so the budget left for C2 is about 250 - 150 = 100 kW. If the planner ignored the
// offline draw it would give C2 up to 150 kW (150 + 150 = 300 > 250): a commanded
// violation (and a physical overshoot).
func TestFaultOfflineChargerKeepsDrawingAndPlannerAccountsForIt(t *testing.T) {
	const from, to = 50, 120
	sc := twoBusScenario()
	sc.BaseLimitKW = 250
	sc.Buses[0].Bus.DepartureMin = 150
	sc.Buses[1].Bus.DepartureMin = 150
	sc.Faults = []Fault{{Kind: FaultChargerOffline, Target: "C1", From: from, To: to}}
	rc := &recordingController{inner: plannerCtrl(sc)}
	m := Run(sc, rc, nil)

	for min := from; min < to; min++ {
		in, plan := rc.ins[min], rc.plans[min]
		var c1 model.Charger
		for _, c := range in.Chargers {
			if c.ID == "C1" {
				c1 = c
			}
		}
		if c1.Status != model.ChargerOffline || c1.LastCommandedKW <= 0 {
			t.Fatalf("minute %d: fault not in effect, C1 observed as %v with last command %v kW", min, c1.Status, c1.LastCommandedKW)
		}
		for _, sp := range plan.Setpoints {
			if sp.ChargerID == "C2" && sp.KW > in.Site.LimitKW-c1.LastCommandedKW+1e-6 {
				t.Fatalf("minute %d: C2 set to %.1f kW but only %.1f - %.1f = %.1f kW is left", min,
					sp.KW, in.Site.LimitKW, c1.LastCommandedKW, in.Site.LimitKW-c1.LastCommandedKW)
			}
		}
	}
	if m.PlanViolations != 0 || m.OvershootMin != 0 {
		t.Errorf("the offline charger's draw must be reserved from the budget: %+v", m)
	}

	// The accounting side: the metrics must count the offline draw. A controller that
	// ignores it (C1 at 150 kW until it goes offline, then C2 at 150 kW as well) commands
	// 150 (offline C1, still drawing) + 150 = 300 > 250 kW on every minute of the window:
	// to-from = 70 commanded violations, and the physical draw exceeds 250 kW from the
	// second window minute on (commands take effect one step later).
	naive := funcController(func(in planner.Input) planner.Plan {
		kw := map[string]float64{"C1": 150}
		if in.Now >= from {
			kw = map[string]float64{"C2": 150}
		}
		var p planner.Plan
		for id, v := range kw {
			p.Setpoints = append(p.Setpoints, planner.Setpoint{ChargerID: id, KW: v})
		}
		return p
	})
	nm := Run(sc, naive, nil)
	if nm.PlanViolations != to-from {
		t.Errorf("offline draw not counted in the commanded total: want %d violations, got %+v", to-from, nm)
	}
	if nm.OvershootMin < 1 {
		t.Errorf("offline draw not counted in the physical total: %+v", nm)
	}
}

// funcController adapts a function to Controller.
type funcController func(in planner.Input) planner.Plan

func (f funcController) Plan(in planner.Input) planner.Plan { return f(in) }

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

	// The fault must really perturb the readings. With a controller that commands 0 kW
	// the true SoC stays at 50 kWh, so without noise every observation is exactly 50;
	// with noise (std dev 5) many readings must deviate by more than 1 kWh.
	observe := func(faults []Fault) []float64 {
		s := baseScenario()
		s.Faults = faults
		rc := &recordingController{inner: stubController{kw: 0}}
		Run(s, rc, nil)
		return rc.socSequence()
	}
	for i, v := range observe(nil) {
		if v != 50 {
			t.Fatalf("no-fault reading %d is %v, want exactly 50", i, v)
		}
	}
	noisy := observe(sc.Faults)
	far := 0
	for _, v := range noisy {
		if math.Abs(v-50) > 1 {
			far++
		}
	}
	// P(|N(0,5)| > 1) = 84%; over 300 readings, far < 100 is astronomically unlikely.
	if far < 100 {
		t.Errorf("noise not applied: only %d of %d readings deviate by more than 1 kWh", far, len(noisy))
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

// While a planner_garbage fault is active the normal layer returns bad output (above
// charger ceilings and the site limit, unknown or unhealthy chargers, NaN, Inf or
// negative setpoints, duplicate IDs). The verifier must correct every such plan: each
// cycle's plan is valid for its observation, and the corrections really happened.
func TestFaultPlannerGarbageIsCorrectedByTheVerifier(t *testing.T) {
	sc := twoBusScenario()
	sc.Chargers = append(sc.Chargers, model.Charger{ID: "C3", MaxKW: 150, MinKW: 5, Efficiency: 0.95, Status: model.ChargerOK})
	sc.Faults = []Fault{
		{Kind: FaultChargerFail, Target: "C3", From: 0, To: forever},
		{Kind: FaultPlannerGarbage, From: 10, To: 30},
	}
	rc := &recordingController{inner: checkingController{t: t, inner: plannerCtrl(sc), label: "garbage"}}
	m := Run(sc, rc, nil)
	if m.PlanViolations != 0 {
		t.Errorf("plan violations: %+v", m)
	}
	corrected := 0
	for min := 10; min < 30; min++ {
		for _, n := range rc.plans[min].Notes {
			if strings.HasPrefix(n, "verificador corrigiu") {
				corrected++
				break
			}
		}
	}
	if corrected != 20 {
		t.Errorf("the verifier corrected %d of the 20 garbage cycles, want all 20", corrected)
	}
	for min := 30; min < 40; min++ {
		for _, n := range rc.plans[min].Notes {
			if strings.HasPrefix(n, "verificador corrigiu") {
				t.Errorf("minute %d: the fault is over but the verifier still corrected: %v", min, rc.plans[min].Notes)
			}
		}
	}
}

// A planner panic while the site limit drops: the last-valid plan was made for the
// higher limit and must be scaled down to the new one.
func TestFaultPlannerPanicDuringLimitDropIsScaledDown(t *testing.T) {
	sc := twoBusScenario()
	sc.Faults = []Fault{
		{Kind: FaultPlannerPanic, From: 10, To: 15},
		{Kind: FaultLimitDrop, From: 12, To: 20, Value: 0.5},
	}
	rc := &recordingController{inner: checkingController{t: t, inner: plannerCtrl(sc), label: "panic+drop"}}
	m := Run(sc, rc, nil)
	if m.PlanViolations != 0 {
		t.Errorf("plan violations: %+v", m)
	}
	for min := 12; min < 15; min++ {
		p := rc.plans[min]
		total := 0.0
		for _, s := range p.Setpoints {
			total += s.KW
		}
		if p.Layer != planner.LayerLastValid || total > 150+1e-6 || total <= 0 {
			t.Errorf("minute %d: want the last-valid plan scaled to the 150 kW limit, got layer %v total %.1f", min, p.Layer, total)
		}
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
	rc := &recordingController{inner: plannerCtrl(sc)}
	if m := Run(sc, rc, nil); m.Ready != 1 {
		t.Errorf("%+v", m)
	}
	// The bus is scheduled at minute 0 but must only be observed from minute 100.
	for min := 0; min <= 110; min++ {
		seen := len(rc.ins[min].Buses) == 1
		if want := min >= 100; seen != want {
			t.Fatalf("minute %d: bus observed=%v, want %v", min, seen, want)
		}
	}
	// Control: without the fault the bus is observed from minute 0.
	nf := baseScenario()
	ctrl := &recordingController{inner: plannerCtrl(nf)}
	Run(nf, ctrl, nil)
	if len(ctrl.ins[0].Buses) != 1 {
		t.Errorf("without the fault the bus must be observed at minute 0")
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
// given (and the plans it returns, and the swaps it recommends), so tests can inspect what the planner saw.
type recordingController struct {
	inner Controller
	ins   []planner.Input
	plans []planner.Plan
	swaps int
}

func (r *recordingController) Plan(in planner.Input) planner.Plan {
	cp := in
	cp.Buses = append([]model.Bus(nil), in.Buses...)
	cp.Chargers = append([]model.Charger(nil), in.Chargers...)
	r.ins = append(r.ins, cp)
	p := r.inner.Plan(in)
	r.plans = append(r.plans, p)
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
// Scenario: 2 chargers (150 kW each), buses B1 (departs at 400, already above its 240 kWh
// target plus the planner's 10 kWh margin: it only occupies C1), B2 and B3 (depart at
// 200, 0 -> 240 kWh). B1 and B2 take the chargers; B3 waits. B1 is full, so it may give
// way, and with B3 on C1 (about 101 min of full power plus the 5-min move, 200 min left)
// the predicted number of ready buses goes from 2 to 3 within the 300 kW limit: the
// planner recommends B1 -> B3 on C1 at minute 0 and the world executes it (FollowSwaps).
// C1 then fails in [100, 140) while it holds B3, so the failure window overlaps the
// swapped state; at minute 107 the planner moves B3 from the failed C1 to C2, which
// B2 (full by then) gives up, and B1 is plugged into C1 again when it recovers.
// (Measured when this comment was written; the swap rule itself is in planner/swaps.go.)
// The test requires that the swap actually happened (otherwise it checks nothing).
func TestFaultNoChargerSharedUnderSwapsAndFailures(t *testing.T) {
	sc := twoBusScenario()
	sc.Buses[0].Bus.SoCKWh = 270 // above the 240 kWh target plus the planner's 10 kWh margin
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
