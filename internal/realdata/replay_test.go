package realdata

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func demoReplay(t *testing.T, files map[string][]byte, o NightOptions, workers int) *Report {
	t.Helper()
	d := loadFiles(t, files)
	r, err := Replay(context.Background(), d, planner.DefaultConfig(), o, workers)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return r
}

// zeroWall clears the wall-clock field so two reports can be compared.
func zeroWall(r *Report) {
	for i := range r.Nights {
		for j := range r.Nights[i].Controllers {
			c := &r.Nights[i].Controllers[j]
			c.Aggregate.PlanP99Micros = 0
			for k := range c.Seeds {
				c.Seeds[k].Metrics.PlanP99Micros = 0
			}
		}
	}
	for j := range r.Aggregate {
		c := &r.Aggregate[j]
		c.Aggregate.PlanP99Micros = 0
		for k := range c.Seeds {
			c.Seeds[k].Metrics.PlanP99Micros = 0
		}
	}
}

func TestReplayDemoShape(t *testing.T) {
	r := demoReplay(t, demoFiles(t), NightOptions{}, 2)
	wantNames := []string{"fifo", "edf", "fifo-unplug", "safe", "planner", "planner (sem rodízio)"}
	if !reflect.DeepEqual(ReplayNames, wantNames) {
		t.Fatalf("ReplayNames = %v", ReplayNames)
	}
	if len(r.Nights) != 2 || r.Nights[0].Key != "2026-03-04" || r.Nights[1].Key != "2026-03-05" {
		t.Fatalf("nights = %+v", r.Nights)
	}
	if len(r.Aggregate) != len(wantNames) {
		t.Fatalf("aggregate rows = %d", len(r.Aggregate))
	}
	for i, ag := range r.Aggregate {
		if ag.Name != wantNames[i] || len(ag.Seeds) != 2 || ag.Seeds[0].Seed != 1 || ag.Seeds[1].Seed != 2 {
			t.Errorf("aggregate[%d] = %s seeds=%+v", i, ag.Name, ag.Seeds)
		}
		if ag.Aggregate.Buses != 6 { // 3 buses on each of 2 nights, summed
			t.Errorf("%s aggregate Buses = %d, want 6", ag.Name, ag.Aggregate.Buses)
		}
	}
	for _, n := range r.Nights {
		if n.Buses != 3 || len(n.Controllers) != 6 || len(n.PerBus) != 3 {
			t.Fatalf("%s: buses=%d controllers=%d perbus=%d", n.Key, n.Buses, len(n.Controllers), len(n.PerBus))
		}
		for i, c := range n.Controllers {
			if c.Name != wantNames[i] {
				t.Errorf("%s controller %d = %q, want %q", n.Key, i, c.Name, wantNames[i])
			}
			if c.Aggregate.Buses != 3 || len(c.Seeds) != 1 || c.Seeds[0].Seed != 1 || c.Seeds[0].Metrics.Buses != 3 {
				t.Errorf("%s/%s: %+v", n.Key, c.Name, c)
			}
			if c.Aggregate.ReadyPct != c.Seeds[0].Metrics.ReadyPct {
				t.Errorf("%s/%s: aggregate differs from its single run", n.Key, c.Name)
			}
		}
		for _, name := range []string{"planner", "planner (sem rodízio)"} {
			for _, c := range n.Controllers {
				if c.Name == name && c.Aggregate.PlanViolations != 0 {
					t.Errorf("%s/%s: PlanViolations = %d", n.Key, name, c.Aggregate.PlanViolations)
				}
			}
		}
		// the planner follows swaps in one row and not in the other
		if n.Controllers[4].Aggregate.OperatorMoves < n.Controllers[5].Aggregate.OperatorMoves {
			t.Errorf("%s: no-swap row asks for more operator moves than the swap row", n.Key)
		}
		for i, b := range n.PerBus {
			if i > 0 && n.PerBus[i-1].ID >= b.ID {
				t.Errorf("%s: PerBus not sorted: %v", n.Key, n.PerBus)
			}
			if b.CapacityKWh != 300 || b.TargetKWh <= 0 || b.TargetKWh > b.CapacityKWh {
				t.Errorf("%s/%s: capacity=%v target=%v", n.Key, b.ID, b.CapacityKWh, b.TargetKWh)
			}
		}
	}
	// PerBus.Real* comes from BusReal: night 1 is all measured, night 2 has B03 unknown.
	for i, wantReady := range []bool{true, false, true} {
		b := r.Nights[0].PerBus[i]
		if b.RealReady == nil || *b.RealReady != wantReady || b.RealFinalKWh == nil {
			t.Errorf("night1 %s: RealReady=%v RealFinal=%v", b.ID, b.RealReady, b.RealFinalKWh)
		}
	}
	wantPtr(t, "B01 final", r.Nights[0].PerBus[0].RealFinalKWh, 273)
	b3 := r.Nights[1].PerBus[2]
	if b3.ID != "B03" || b3.RealReady != nil || b3.RealFinalKWh != nil {
		t.Errorf("night2 B03 = %+v, want unknown real outcome", b3)
	}
	if r.Nights[0].Real.Ready != 2 {
		t.Errorf("night1 Real = %+v", r.Nights[0].Real)
	}
}

func TestReplayDeterministicAcrossWorkers(t *testing.T) {
	a := demoReplay(t, demoFiles(t), NightOptions{}, 1)
	b := demoReplay(t, demoFiles(t), NightOptions{}, 4)
	zeroWall(a)
	zeroWall(b)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("reports differ between workers=1 and workers=4")
	}
}

func TestReplayWorkersBelowOne(t *testing.T) {
	a := demoReplay(t, demoFiles(t), NightOptions{}, 0)
	if len(a.Nights) != 2 {
		t.Errorf("nights = %d", len(a.Nights))
	}
}

func TestReplayCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := Replay(ctx, loadFiles(t, demoFiles(t)), planner.DefaultConfig(), NightOptions{}, 2)
	if !errors.Is(err, context.Canceled) || r != nil {
		t.Errorf("got %v, %v; want nil, context.Canceled", r, err)
	}
}

func TestReplayRealAggregate(t *testing.T) {
	r := demoReplay(t, demoFiles(t), NightOptions{}, 2)
	a := r.RealAggregate
	// night 1: 2 of 3 ready (66.667%), shortfall 45; night 2: 1 of 2 ready (50%), shortfall 6
	if a.Buses != 6 || a.WithOutcome != 5 || a.Ready != 3 {
		t.Errorf("counts = %+v", a)
	}
	if math.Abs(a.ReadyPct-(200.0/3+50)/2) > 1e-9 || math.Abs(a.ReadyPct-58.3333333) > 1e-6 {
		t.Errorf("ReadyPct = %v, want 58.333", a.ReadyPct)
	}
	if math.Abs(a.ShortfallKWh-25.5) > 1e-9 {
		t.Errorf("ShortfallKWh = %v, want 25.5", a.ShortfallKWh)
	}
	wantPtr(t, "EnergyKWh", a.EnergyKWh, 272.5) // (480+65)/2
	if a.PeakKW == nil || a.CostBRL == nil {
		t.Errorf("peak/cost = %v / %v", a.PeakKW, a.CostBRL)
	}
	wantPtr(t, "CostBRL", a.CostBRL, (522+65*0.90)/2) // night 2: 65 kWh measured at 22:00, off-peak
	if !a.CostEstimated || !a.PeakEstimated {
		t.Errorf("estimated flags: cost=%v peak=%v", a.CostEstimated, a.PeakEstimated)
	}
}

func TestReplayRealAggregateNoData(t *testing.T) {
	// without sessions and power nothing is known about energy, peak and cost
	r := demoReplay(t, without(demoFiles(t), FileSessoes, FilePotencia), NightOptions{}, 2)
	a := r.RealAggregate
	wantNil(t, "EnergyKWh", a.EnergyKWh)
	wantNil(t, "PeakKW", a.PeakKW)
	wantNil(t, "CostBRL", a.CostBRL)
	if a.PeakEstimated || a.CostEstimated {
		t.Errorf("flags without data: %+v", a)
	}
	// without any outcome the average is 0, not NaN, and the buses are not counted as not ready
	m := mutate(t, demoFiles(t), FileOnibus, "2026-03-05 05:00,90,150,91,2026-03-05 05:02", "2026-03-05 05:00,90,150,,")
	m = mutate(t, m, FileOnibus, "2026-03-05 06:00,85,150,70,2026-03-05 06:00", "2026-03-05 06:00,85,150,,")
	m = mutate(t, m, FileOnibus, "2026-03-05 05:30,90,150,90,2026-03-05 05:30", "2026-03-05 05:30,90,150,,")
	m = mutate(t, m, FileOnibus, "2026-03-06 05:00,90,150,88,2026-03-06 05:00", "2026-03-06 05:00,90,150,,")
	m = mutate(t, m, FileOnibus, "2026-03-06 06:00,80,150,80,2026-03-06 06:00", "2026-03-06 06:00,80,150,,")
	r = demoReplay(t, m, NightOptions{}, 2)
	if r.RealAggregate.WithOutcome != 0 || r.RealAggregate.ReadyPct != 0 || r.RealAggregate.ShortfallKWh != 0 {
		t.Errorf("RealAggregate = %+v", r.RealAggregate)
	}
	for _, n := range r.Nights {
		for _, b := range n.PerBus {
			if b.RealReady != nil || b.RealFinalKWh != nil {
				t.Errorf("%s/%s: %+v", n.Key, b.ID, b)
			}
		}
	}
}

func TestReplayAssumptions(t *testing.T) {
	r := demoReplay(t, demoFiles(t), NightOptions{}, 2)
	if len(r.Assumptions) != 5 {
		t.Fatalf("assumptions = %q", r.Assumptions)
	}
	if !strings.Contains(r.Assumptions[0], "perfeitas") || strings.Contains(r.Assumptions[0], "ruído de") {
		t.Errorf("first assumption = %q", r.Assumptions[0])
	}
	joined := strings.Join(r.Assumptions, "\n")
	for _, want := range []string{"limite de potência", "defeito", "rodízios", "sem rodízio", "decisões"} {
		if !strings.Contains(joined, want) {
			t.Errorf("assumptions lack %q: %q", want, r.Assumptions)
		}
	}
	if !r.CostComparable {
		t.Errorf("demo tariff does not cross midnight: CostComparable must be true")
	}

	rn := demoReplay(t, demoFiles(t), NightOptions{SoCNoiseKWh: 2.5}, 2)
	if !strings.Contains(rn.Assumptions[0], "ruído de 2.5 kWh") || strings.Contains(rn.Assumptions[0], "perfeitas") {
		t.Errorf("noise assumption = %q", rn.Assumptions[0])
	}
	if len(rn.Assumptions) != 5 {
		t.Errorf("assumptions with noise = %d", len(rn.Assumptions))
	}
}

func TestReplayCostComparable(t *testing.T) {
	m := mutate(t, demoFiles(t), FileGaragem, "18:00,21:00", "22:00,06:00")
	r := demoReplay(t, m, NightOptions{}, 2)
	if r.CostComparable {
		t.Errorf("window crossing midnight: CostComparable must be false")
	}
	if len(r.Assumptions) != 6 || !strings.Contains(r.Assumptions[5], "meia-noite") || !strings.Contains(r.Assumptions[5], "não é comparável") {
		t.Errorf("assumptions = %q", r.Assumptions)
	}
	wantPtr(t, "real cost stays visible", r.Nights[1].Real.CostBRL, 65*2.70)

	r = demoReplay(t, withFile(demoFiles(t), FileGaragem, "limite_kw\n400\n"), NightOptions{}, 2)
	if r.CostComparable {
		t.Errorf("no tariff: CostComparable must be false")
	}
	if len(r.Assumptions) != 5 {
		t.Errorf("no tariff must not add the midnight line: %q", r.Assumptions)
	}
}

func TestReplayWarningsOrder(t *testing.T) {
	m := withFile(demoFiles(t), "extra.csv", "x")
	m = mutate(t, m, FileGaragem, "18:00,21:00", "22:00,06:00")
	d := loadFiles(t, m)
	_, nightWs := d.Nights(NightOptions{})
	if len(d.Warnings) == 0 || len(nightWs) == 0 {
		t.Fatalf("fixture must produce both kinds of warnings: %v / %v", d.Warnings, nightWs)
	}
	r, err := Replay(context.Background(), d, planner.DefaultConfig(), NightOptions{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]Warning(nil), d.Warnings...), nightWs...)
	if !reflect.DeepEqual(r.Warnings, want) {
		t.Errorf("warnings = %v, want %v", r.Warnings, want)
	}
}

func TestReplayShuffledRowsSameResult(t *testing.T) {
	orig := demoFiles(t)
	lines := strings.Split(strings.TrimRight(string(orig[FileOnibus]), "\n"), "\n")
	rows := lines[1:]
	shuffled := []string{lines[0]}
	for i := len(rows) - 1; i >= 0; i-- { // reversed
		shuffled = append(shuffled, rows[i])
	}
	sh := withFile(orig, FileOnibus, strings.Join(shuffled, "\n")+"\n")

	a := demoReplay(t, orig, NightOptions{}, 2)
	b := demoReplay(t, sh, NightOptions{}, 2)
	zeroWall(a)
	zeroWall(b)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("shuffled onibus.csv rows changed the replay")
	}
}

func TestReplayJSONShape(t *testing.T) {
	r := demoReplay(t, demoFiles(t), NightOptions{SoCNoiseKWh: 1}, 2)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"nights", "aggregate", "real_aggregate", "assumptions", "warnings", "cost_comparable"} {
		if _, ok := top[k]; !ok {
			t.Errorf("missing top-level key %q in %s", k, raw)
		}
	}
	nights := top["nights"].([]any)
	n0 := nights[0].(map[string]any)
	for _, k := range []string{"key", "buses", "real", "controllers", "per_bus"} {
		if _, ok := n0[k]; !ok {
			t.Errorf("night lacks %q", k)
		}
	}
	c0 := n0["controllers"].([]any)[0].(map[string]any)
	for _, k := range []string{"name", "aggregate", "seeds"} {
		if _, ok := c0[k]; !ok {
			t.Errorf("controller lacks %q: %v", k, c0)
		}
	}
	seed0 := c0["seeds"].([]any)[0].(map[string]any)
	if _, ok := seed0["seed"]; !ok {
		t.Errorf("seed run lacks seed: %v", seed0)
	}
	if _, ok := seed0["metrics"].(map[string]any)["ready_pct"]; !ok {
		t.Errorf("seed run lacks metrics.ready_pct: %v", seed0)
	}
	if _, ok := c0["aggregate"].(map[string]any)["plan_p99_micros"]; !ok {
		t.Errorf("aggregate metrics lack plan_p99_micros")
	}
	pb := n0["per_bus"].([]any)[0].(map[string]any)
	for _, k := range []string{"id", "capacity_kwh", "target_kwh", "real_final_kwh", "real_ready", "planner_final_kwh", "planner_ready", "no_swap_final_kwh", "no_swap_ready"} {
		if _, ok := pb[k]; !ok {
			t.Errorf("per_bus lacks %q: %v", k, pb)
		}
	}
	// unknown real outcome serializes as null, never as 0 or false
	pb3 := nights[1].(map[string]any)["per_bus"].([]any)[2].(map[string]any)
	if v, ok := pb3["real_ready"]; !ok || v != nil {
		t.Errorf("unknown real_ready = %v (present %v), want null", v, ok)
	}
	if v, ok := pb3["real_final_kwh"]; !ok || v != nil {
		t.Errorf("unknown real_final_kwh = %v, want null", v)
	}
	ra := top["real_aggregate"].(map[string]any)
	for _, k := range []string{"buses", "with_outcome", "ready_pct", "energy_kwh", "peak_kw", "cost_brl"} {
		if _, ok := ra[k]; !ok {
			t.Errorf("real_aggregate lacks %q", k)
		}
	}
	// no key of the report may start with an upper-case letter
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if k == "" || (k[0] >= 'A' && k[0] <= 'Z') {
					t.Errorf("non snake_case key %q", k)
				}
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(top)
}

func TestReplayNoNonFinite(t *testing.T) {
	r := demoReplay(t, demoFiles(t), NightOptions{SoCNoiseKWh: 3}, 2)
	chk := func(name string, v float64) {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("%s = %v", name, v)
		}
	}
	for _, n := range r.Nights {
		for _, c := range n.Controllers {
			chk(n.Key+"/"+c.Name, c.Aggregate.ReadyPct+c.Aggregate.ShortfallKWh+c.Aggregate.PeakKW+c.Aggregate.EnergyKWh+c.Aggregate.CostBRL)
		}
		for _, b := range n.PerBus {
			chk(n.Key+"/"+b.ID, b.PlannerFinalKWh+b.NoSwapFinalKWh)
		}
	}
}

func TestWarningJSONKeys(t *testing.T) {
	raw, err := json.Marshal(Warning{File: "a.csv", Line: 2, Message: "m"})
	if err != nil || string(raw) != `{"file":"a.csv","line":2,"message":"m"}` {
		t.Errorf("%s %v", raw, err)
	}
}
