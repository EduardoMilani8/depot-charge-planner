package sim

import (
	"math"
	"reflect"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func tracedRun(p GenParams, seed int64, controller string) (Metrics, *Trace, Scenario) {
	cfg := planner.DefaultConfig()
	sc, ctrl, _ := ForController(controller, cfg, Generate(p, seed))
	tr := NewTrace()
	m := RunTraced(sc, ctrl, nil, tr)
	return m, tr, sc
}

func sameFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.IsNaN(a[i]) != math.IsNaN(b[i]) || (!math.IsNaN(a[i]) && a[i] != b[i]) {
			return false
		}
	}
	return true
}

func TestRunTracedMatchesRun(t *testing.T) {
	for _, profile := range []FaultProfile{ProfileNone, ProfileSevere, ProfileRandom} {
		p := smallParams()
		p.Profile = profile
		for _, name := range ControllerNames {
			sc, ctrl, _ := ForController(name, planner.DefaultConfig(), Generate(p, 2))
			want := Run(sc, ctrl, nil)
			sc2, ctrl2, _ := ForController(name, planner.DefaultConfig(), Generate(p, 2))
			got := RunTraced(sc2, ctrl2, nil, NewTrace())
			want.PlanP99Micros, got.PlanP99Micros = 0, 0
			if !reflect.DeepEqual(want, got) {
				t.Errorf("%s/%s: tracing changed the metrics\nwant %+v\ngot  %+v", profile, name, want, got)
			}
		}
	}
}

func TestTraceShapes(t *testing.T) {
	p := smallParams()
	m, tr, sc := tracedRun(p, 1, "planner")
	n := sc.Horizon + 1
	for name, l := range map[string]int{"Limit": len(tr.Limit), "Commanded": len(tr.Commanded), "Physical": len(tr.Physical), "Layer": len(tr.Layer)} {
		if l != n {
			t.Errorf("%s has %d minutes, want %d", name, l, n)
		}
	}
	if len(tr.Chargers) != p.NumChargers || len(tr.Buses) != p.NumBuses || len(tr.Outcomes) != p.NumBuses {
		t.Fatalf("%d chargers, %d buses, %d outcomes", len(tr.Chargers), len(tr.Buses), len(tr.Outcomes))
	}
	for _, c := range tr.Chargers {
		if len(c.Status) != n || len(c.CommandedKW) != n || len(c.PhysicalKW) != n || len(c.BusID) != n {
			t.Errorf("charger %s series have the wrong length", c.ID)
		}
	}
	for _, b := range tr.Buses {
		if len(b.State) != n || len(b.TrueSoC) != n || len(b.Observed) != n || len(b.ChargerID) != n {
			t.Errorf("bus %s series have the wrong length", b.ID)
		}
	}
	if m.PlanViolations != 0 {
		t.Fatalf("planner violated the limit: %d", m.PlanViolations)
	}
	for i := 0; i < n; i++ {
		if tr.Commanded[i] > tr.Limit[i]+1e-6 {
			t.Fatalf("minute %d: commanded %.1f kW above the limit %.1f", i, tr.Commanded[i], tr.Limit[i])
		}
	}
	peak := 0.0
	for _, v := range tr.Physical {
		peak = math.Max(peak, v)
	}
	if math.Abs(peak-m.PeakKW) > 1e-6 {
		t.Errorf("peak of the physical series %.3f != metrics PeakKW %.3f", peak, m.PeakKW)
	}
}

func TestTraceOutcomesMatchMetrics(t *testing.T) {
	m, tr, _ := tracedRun(smallParams(), 3, "planner")
	ready, short := 0, 0.0
	for _, o := range tr.Outcomes {
		if o.Ready {
			ready++
		} else if o.Departed {
			short += o.ShortfallKWh
		}
		if o.Departure < o.Arrival {
			t.Errorf("bus %s departs (%d) before it arrives (%d)", o.ID, o.Departure, o.Arrival)
		}
	}
	if ready != m.Ready {
		t.Errorf("%d ready outcomes, metrics say %d", ready, m.Ready)
	}
	if math.Abs(short-m.ShortfallKWh) > 1e-6 {
		t.Errorf("outcome shortfall %.3f != metrics %.3f", short, m.ShortfallKWh)
	}
}

func TestTraceIsDeterministic(t *testing.T) {
	p := smallParams()
	p.Profile = ProfileRandom
	_, a, _ := tracedRun(p, 4, "planner")
	_, b, _ := tracedRun(p, 4, "planner")
	if !sameFloats(a.Physical, b.Physical) || !sameFloats(a.Commanded, b.Commanded) {
		t.Fatal("power series differ between identical runs")
	}
	for i := range a.Buses {
		if !sameFloats(a.Buses[i].TrueSoC, b.Buses[i].TrueSoC) || !sameFloats(a.Buses[i].Observed, b.Buses[i].Observed) {
			t.Fatalf("bus %s series differ between identical runs", a.Buses[i].ID)
		}
	}
	if !reflect.DeepEqual(a.Decisions, b.Decisions) || !reflect.DeepEqual(a.Outcomes, b.Outcomes) {
		t.Fatal("decisions or outcomes differ between identical runs")
	}
}

func TestDecisionsAreElided(t *testing.T) {
	_, tr, sc := tracedRun(smallParams(), 1, "planner")
	if len(tr.Decisions) == 0 || tr.Decisions[0].Minute != 0 {
		t.Fatalf("the first decision must be at minute 0: %+v", tr.Decisions)
	}
	if len(tr.Decisions) >= sc.Horizon+1 {
		t.Errorf("%d decisions for %d minutes: unchanged minutes must be skipped", len(tr.Decisions), sc.Horizon+1)
	}
	for i := 1; i < len(tr.Decisions); i++ {
		if tr.Decisions[i].Minute <= tr.Decisions[i-1].Minute {
			t.Fatalf("decisions are not in minute order at %d", i)
		}
	}
}

func TestObservedIsNaNWithoutAReading(t *testing.T) {
	p := smallParams()
	p.Profile = ProfileNone
	sc := Generate(p, 1)
	sc.Faults = append(sc.Faults, Fault{Kind: FaultSoCMissing, Target: "*", From: 0, To: forever})
	_, ctrl, _ := ForController("planner", planner.DefaultConfig(), sc)
	tr := NewTrace()
	RunTraced(sc, ctrl, nil, tr)
	seenPresent := false
	for _, b := range tr.Buses {
		for i, st := range b.State {
			if st != 1 {
				if !math.IsNaN(b.TrueSoC[i]) {
					t.Fatalf("bus %s minute %d: true SoC must be NaN while not present", b.ID, i)
				}
				continue
			}
			seenPresent = true
			if math.IsNaN(b.TrueSoC[i]) {
				t.Fatalf("bus %s minute %d: a present bus has a true SoC", b.ID, i)
			}
			if !math.IsNaN(b.Observed[i]) {
				t.Fatalf("bus %s minute %d: observed must be NaN when readings are missing, got %v", b.ID, i, b.Observed[i])
			}
		}
	}
	if !seenPresent {
		t.Fatal("no bus was ever present: the test checks nothing")
	}
}

func TestChargerSeriesAgreeWithBuses(t *testing.T) {
	_, tr, _ := tracedRun(smallParams(), 1, "planner")
	idx := map[string]*BusTrace{}
	for _, b := range tr.Buses {
		idx[b.ID] = b
	}
	for _, c := range tr.Chargers {
		for i, id := range c.BusID {
			if id == "" {
				continue
			}
			if b := idx[id]; b == nil || b.ChargerID[i] != c.ID {
				t.Fatalf("minute %d: charger %s says bus %s, the bus says otherwise", i, c.ID, id)
			}
		}
	}
}

func TestObservedMatchesTrueSoCWithPerfectSensors(t *testing.T) {
	p := smallParams()
	p.Profile = ProfileNone
	_, tr, _ := tracedRun(p, 1, "planner")
	present := 0
	for _, b := range tr.Buses {
		for i, st := range b.State {
			if st != 1 {
				continue
			}
			present++
			if d := math.Abs(b.Observed[i] - b.TrueSoC[i]); !(d < 1e-9) {
				t.Fatalf("bus %s minute %d: observed %.6f vs true %.6f (they must be read at the same instant)", b.ID, i, b.Observed[i], b.TrueSoC[i])
			}
		}
	}
	if present == 0 {
		t.Fatal("no bus was ever present: the test checks nothing")
	}
}

func TestDecisionKeyNotesAndMasking(t *testing.T) {
	base := planner.Plan{Layer: planner.LayerNormal, Setpoints: []planner.Setpoint{{ChargerID: "C1", KW: 50}}}
	withNote := func(notes ...string) planner.Plan {
		p := base
		p.Notes = notes
		return p
	}
	if decisionKey(base) == decisionKey(withNote("rodízio não recomendado: B004")) {
		t.Error("a plan with a note must differ from the same plan without it")
	}
	if decisionKey(withNote("folga 252 min")) != decisionKey(withNote("folga 251 min")) {
		t.Error("notes differing only in standalone numbers must share a key")
	}
	if decisionKey(withNote("rodízio não recomendado: B004")) == decisionKey(withNote("rodízio não recomendado: B006")) {
		t.Error("notes naming different buses must not share a key")
	}
	r1 := planner.Plan{Layer: base.Layer, Buses: []planner.BusStatus{{BusID: "B1", Reason: "bus B004 folga 3 min"}}}
	r2 := planner.Plan{Layer: base.Layer, Buses: []planner.BusStatus{{BusID: "B1", Reason: "bus B006 folga 3 min"}}}
	if decisionKey(r1) == decisionKey(r2) {
		t.Error("reasons naming different buses must not share a key")
	}
	// Recorded through the Trace: a note appearing with unchanged setpoints is a decision.
	tr := NewTrace()
	tr.recordDecision(0, base)
	tr.recordDecision(1, base)
	tr.recordDecision(2, withNote("verificador corrigiu o plano: 1 violação(ões)"))
	if len(tr.Decisions) != 2 || tr.Decisions[1].Minute != 2 {
		t.Errorf("decisions: %+v", tr.Decisions)
	}
}

func TestTraceBusArrivingAfterTheHorizon(t *testing.T) {
	sc := baseScenario()
	sc.Horizon = 100
	sc.Buses[0].Bus.ArrivalMin = 500
	sc.Buses[0].Bus.DepartureMin = 900
	tr := NewTrace()
	m := RunTraced(sc, plannerCtrl(sc), nil, tr)
	for i, st := range tr.Buses[0].State {
		if st != 0 {
			t.Fatalf("minute %d: state %d, want 0 (never present)", i, st)
		}
	}
	o := tr.Outcomes[0]
	if o.Departed || o.Ready || o.ShortfallKWh != 0 {
		t.Errorf("outcome of a bus that never arrived: %+v", o)
	}
	if m.Buses != 1 || m.Ready != 0 || m.ShortfallKWh != 0 {
		t.Errorf("metrics: %+v", m)
	}
}

func TestTraceEarlyDepartureOutcome(t *testing.T) {
	sc := baseScenario()
	sc.Faults = []Fault{{Kind: FaultEarlyDeparture, Target: "B1", From: 10, To: forever, Value: 30}}
	tr := NewTrace()
	RunTraced(sc, plannerCtrl(sc), nil, tr)
	o := tr.Outcomes[0]
	if o.Departure != 30 || !o.Departed || o.Ready || o.ShortfallKWh <= 0 {
		t.Errorf("outcome: %+v", o)
	}
	if st := tr.Buses[0].State; st[29] != 1 || st[30] != 2 {
		t.Errorf("state at 29/30: %d/%d, want 1/2", st[29], st[30])
	}
}
