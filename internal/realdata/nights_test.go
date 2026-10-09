package realdata

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

func loadFiles(t *testing.T, files map[string][]byte) *Dataset {
	t.Helper()
	d, err := Load(files)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return d
}

func demoNights(t *testing.T, files map[string][]byte, o NightOptions) ([]Night, []Warning) {
	t.Helper()
	return loadFiles(t, files).Nights(o)
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func wantPtr(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %v", name, want)
		return
	}
	if !near(*got, want) {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

func wantNil(t *testing.T, name string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %v, want nil", name, *got)
	}
}

// setRow replaces the whole line of file that starts with prefix.
func setRow(t *testing.T, m map[string][]byte, file, prefix, row string) map[string][]byte {
	t.Helper()
	lines := strings.Split(string(m[file]), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) {
			lines[i] = row
			out := map[string][]byte{}
			for k, v := range m {
				out[k] = v
			}
			out[file] = []byte(strings.Join(lines, "\n"))
			return out
		}
	}
	t.Fatalf("no line starting with %q in %s", prefix, file)
	return nil
}

func withFile(m map[string][]byte, file, content string) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range m {
		out[k] = v
	}
	out[file] = []byte(content)
	return out
}

func without(m map[string][]byte, files ...string) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range m {
		out[k] = v
	}
	for _, f := range files {
		delete(out, f)
	}
	return out
}

func TestNightsDemoFirstNight(t *testing.T) {
	ns, ws := demoNights(t, demoFiles(t), NightOptions{})
	// the only warning: night 2 has sessions for 2 of its 3 buses (TestNightsPartialCoverage)
	if len(ws) != 1 || !strings.Contains(ws[0].Message, "1 de 3 ônibus sem sessão") {
		t.Errorf("warnings: %v", ws)
	}
	if len(ns) != 2 || ns[0].Key != "2026-03-04" || ns[1].Key != "2026-03-05" {
		t.Fatalf("nights = %+v", ns)
	}
	n := ns[0]
	sc := n.Scenario
	if sc.Name != "real-2026-03-04" || sc.Seed != 1 || !sc.FollowSwaps || sc.UnplugFull {
		t.Errorf("scenario header: %+v", sc)
	}
	if len(sc.Buses) != 3 || sc.StartClockMin != 1140 || sc.Horizon != 690 || sc.BaseLimitKW != 400 {
		t.Errorf("scenario: buses=%d start=%d horizon=%d limit=%v", len(sc.Buses), sc.StartClockMin, sc.Horizon, sc.BaseLimitKW)
	}
	if len(sc.Chargers) != 3 || len(sc.Faults) != 0 {
		t.Errorf("chargers=%d faults=%d", len(sc.Chargers), len(sc.Faults))
	}
	for _, c := range sc.Chargers {
		if c.MaxKW != 150 || c.MinKW != 5 || c.Efficiency != 0.94 || c.Status != model.ChargerOK || c.LastCommandedKW != 0 {
			t.Errorf("charger %+v", c)
		}
	}
	if sc.Tariff != (sim.Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.70, OffPeakPrice: 0.90}) {
		t.Errorf("tariff = %+v", sc.Tariff)
	}
	b := sc.Buses[0]
	if b.Bus.ID != "B01" || b.Bus.ArrivalMin != 60 || b.Bus.DepartureMin != 600 {
		t.Errorf("B01 = %+v", b.Bus)
	}
	if b.Bus.CapacityKWh != 300 || b.Bus.SoCKWh != 90 || b.Bus.TargetKWh != 270 || b.TrueTargetKWh != 270 ||
		b.Bus.SoCConfidence != 1 || b.Bus.SoCAgeMin != 0 || b.Bus.MaxBatteryKW != 150 {
		t.Errorf("B01 = %+v true=%v", b.Bus, b.TrueTargetKWh)
	}
	if sc.Buses[1].Bus.ID != "B02" || sc.Buses[1].Bus.ArrivalMin != 180 || sc.Buses[1].Bus.DepartureMin != 660 {
		t.Errorf("B02 = %+v", sc.Buses[1].Bus)
	}
	if sc.Buses[2].Bus.ID != "B03" || sc.Buses[2].Bus.ArrivalMin != 270 || sc.Buses[2].Bus.DepartureMin != 630 {
		t.Errorf("B03 = %+v", sc.Buses[2].Bus)
	}

	r := n.Real
	if r.Buses != 3 || r.WithOutcome != 3 || r.Ready != 2 || !near(r.ReadyPct, 200.0/3) || !near(r.ShortfallKWh, 45) {
		t.Errorf("real = %+v", r)
	}
	wantPtr(t, "EnergyKWh", r.EnergyKWh, 480)
	wantPtr(t, "PeakKW", r.PeakKW, 120)
	if !r.PeakEstimated || !r.CostEstimated {
		t.Errorf("estimated flags: peak=%v cost=%v", r.PeakEstimated, r.CostEstimated)
	}
	wantPtr(t, "CostBRL", r.CostBRL, 522)

	if len(n.BusReal) != 3 {
		t.Fatalf("BusReal = %+v", n.BusReal)
	}
	wantBus := []struct {
		id    string
		final float64
		ready bool
	}{{"B01", 273, true}, {"B02", 210, false}, {"B03", 270, true}}
	for i, w := range wantBus {
		br := n.BusReal[i]
		if br.ID != w.id || br.Ready == nil || *br.Ready != w.ready {
			t.Errorf("BusReal[%d] = %+v, want %+v", i, br, w)
		}
		wantPtr(t, w.id+".FinalKWh", br.FinalKWh, w.final)
	}
}

func TestNightsDemoSecondNight(t *testing.T) {
	ns, _ := demoNights(t, demoFiles(t), NightOptions{})
	n := ns[1]
	sc := n.Scenario
	// first arrival 21:30 -> hour 21:00 -> origin 20:00 -> clock 1200
	if len(sc.Buses) != 3 || sc.StartClockMin != 1200 || sc.Horizon != 630 {
		t.Errorf("scenario: buses=%d start=%d horizon=%d", len(sc.Buses), sc.StartClockMin, sc.Horizon)
	}
	// the 00:20 arrival of the 6th belongs to this night: 4h20 after 20:00
	var b03 sim.BusSpec
	for _, b := range sc.Buses {
		if b.Bus.ID == "B03" {
			b03 = b
		}
	}
	if b03.Bus.ArrivalMin != 260 || b03.Bus.DepartureMin != 600 || b03.Bus.SoCKWh != 75 {
		t.Errorf("B03 = %+v", b03.Bus)
	}
	r := n.Real
	if r.Buses != 3 || r.WithOutcome != 2 || r.Ready != 1 || !near(r.ReadyPct, 50) || !near(r.ShortfallKWh, 6) {
		t.Errorf("real = %+v", r)
	}
	wantPtr(t, "EnergyKWh", r.EnergyKWh, 65)
	wantPtr(t, "PeakKW", r.PeakKW, 140)
	if r.PeakEstimated || r.CostEstimated {
		t.Errorf("measured night flagged estimated: peak=%v cost=%v", r.PeakEstimated, r.CostEstimated)
	}
	wantPtr(t, "CostBRL", r.CostBRL, 58.5)

	byID := map[string]BusReal{}
	for _, b := range n.BusReal {
		byID[b.ID] = b
	}
	if byID["B03"].Ready != nil || byID["B03"].FinalKWh != nil {
		t.Errorf("B03 has no outcome, got %+v", byID["B03"])
	}
	if r := byID["B01"].Ready; r == nil || *r {
		t.Errorf("B01 should be not ready: %+v", byID["B01"])
	}
	if r := byID["B02"].Ready; r == nil || !*r {
		t.Errorf("B02 (80%% of 300 = target) should be ready: %+v", byID["B02"])
	}
}

func TestNightsNoTariff(t *testing.T) {
	m := withFile(demoFiles(t), FileGaragem, "limite_kw\n400\n")
	ns, _ := demoNights(t, m, NightOptions{})
	for _, n := range ns {
		wantNil(t, n.Key+" CostBRL", n.Real.CostBRL)
		if n.Real.CostEstimated {
			t.Errorf("%s: CostEstimated without cost", n.Key)
		}
		if n.Scenario.Tariff != (sim.Tariff{}) {
			t.Errorf("%s: tariff = %+v, want zero", n.Key, n.Scenario.Tariff)
		}
		if n.Real.EnergyKWh == nil || n.Real.PeakKW == nil {
			t.Errorf("%s: energy/peak must not depend on tariff", n.Key)
		}
	}
}

func TestNightsNoSessionsNoPower(t *testing.T) {
	ns, _ := demoNights(t, without(demoFiles(t), FileSessoes, FilePotencia), NightOptions{})
	for _, n := range ns {
		wantNil(t, "EnergyKWh", n.Real.EnergyKWh)
		wantNil(t, "PeakKW", n.Real.PeakKW)
		wantNil(t, "CostBRL", n.Real.CostBRL)
		if n.Real.PeakEstimated || n.Real.CostEstimated {
			t.Errorf("%s: flags set without data: %+v", n.Key, n.Real)
		}
		if n.Real.WithOutcome == 0 {
			t.Errorf("%s: outcome must not depend on sessions/power", n.Key)
		}
	}
}

func TestNightsSessionsOnly(t *testing.T) {
	ns, _ := demoNights(t, without(demoFiles(t), FilePotencia), NightOptions{})
	r := ns[1].Real
	// B01: 50 kWh / 0.5 h = 100 kW; B02: 15 kWh / 0.5 h = 30 kW; both 22:00-22:30
	wantPtr(t, "EnergyKWh", r.EnergyKWh, 65)
	wantPtr(t, "PeakKW", r.PeakKW, 130)
	wantPtr(t, "CostBRL", r.CostBRL, 58.5)
	if !r.PeakEstimated || !r.CostEstimated {
		t.Errorf("sessions-only must be estimated: %+v", r)
	}
}

func TestNightsPowerOnlyIntegralAndHeldValues(t *testing.T) {
	// C01 reads once (100 kW at 22:00) and keeps that value; C02 ramps 20 -> 50.
	// Instants: 22:00 -> 100, 22:30 -> 120, 23:00 -> 150. Energy: C02 20 kW x 0.5 h = 10 kWh
	// (the last sample of each charger counts for 0 min, so C01 adds nothing).
	power := "instante,carregador_id,potencia_kw\n" +
		"2026-03-05 22:00,C01,100\n" +
		"2026-03-05 22:30,C02,20\n" +
		"2026-03-05 23:00,C02,50\n"
	ns, _ := demoNights(t, withFile(without(demoFiles(t), FileSessoes), FilePotencia, power), NightOptions{})
	r := ns[1].Real
	wantPtr(t, "EnergyKWh", r.EnergyKWh, 10)
	wantPtr(t, "PeakKW", r.PeakKW, 150)
	wantPtr(t, "CostBRL", r.CostBRL, 9)
	if r.PeakEstimated || r.CostEstimated {
		t.Errorf("measured: %+v", r)
	}
	// night 1 has no power and no sessions any more
	wantNil(t, "night1 EnergyKWh", ns[0].Real.EnergyKWh)
}

func TestNightsPowerNotGloballySorted(t *testing.T) {
	// Charger blocks in file order: C01 entirely before C02 (sorted per charger,
	// not globally). Result must equal the interleaved demo file.
	power := "instante,carregador_id,potencia_kw\n" +
		"2026-03-05 22:00,C01,100\n" +
		"2026-03-05 22:15,C01,100\n" +
		"2026-03-05 22:30,C01,0\n" +
		"2026-03-05 22:00,C02,20\n" +
		"2026-03-05 22:15,C02,40\n" +
		"2026-03-05 22:30,C02,0\n"
	d := loadFiles(t, withFile(demoFiles(t), FilePotencia, power))
	if d.Power[0].ChargerID != "C01" || d.Power[3].ChargerID != "C02" || !d.Power[3].At.Before(d.Power[2].At) {
		t.Fatalf("fixture is not in the intended non-global order: %+v", d.Power)
	}
	ns, _ := d.Nights(NightOptions{})
	r := ns[1].Real
	wantPtr(t, "EnergyKWh", r.EnergyKWh, 65)
	wantPtr(t, "PeakKW", r.PeakKW, 140)
	wantPtr(t, "CostBRL", r.CostBRL, 58.5)
}

func TestNightsSoCNoiseFault(t *testing.T) {
	ns, _ := demoNights(t, demoFiles(t), NightOptions{SoCNoiseKWh: 3})
	for _, n := range ns {
		if len(n.Scenario.Faults) != 1 {
			t.Fatalf("%s: faults = %+v", n.Key, n.Scenario.Faults)
		}
		f := n.Scenario.Faults[0]
		if f.Kind != sim.FaultSoCNoise || f.Target != "*" || f.Value != 3 || f.From != 0 || f.To != math.MaxInt32 {
			t.Errorf("fault = %+v", f)
		}
	}
	for _, v := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		ns, _ := demoNights(t, demoFiles(t), NightOptions{SoCNoiseKWh: v})
		if len(ns[0].Scenario.Faults) != 0 {
			t.Errorf("SoCNoiseKWh=%v added a fault", v)
		}
	}
}

func TestNightsScenariosRunInSimulator(t *testing.T) {
	for _, o := range []NightOptions{{}, {SoCNoiseKWh: 3}} {
		ns, _ := demoNights(t, demoFiles(t), o)
		for _, n := range ns {
			sc := n.Scenario
			m := sim.Run(sc, sim.NewPlannerController(planner.DefaultConfig(), sc), nil)
			if m.PlanViolations != 0 {
				t.Errorf("%s noise=%v: PlanViolations = %d", n.Key, o.SoCNoiseKWh, m.PlanViolations)
			}
			if m.Buses != 3 {
				t.Errorf("%s: simulated %d buses", n.Key, m.Buses)
			}
		}
	}
}

func TestNightsHorizonCap(t *testing.T) {
	// B01 arrives 12:30 on the 4th: origin = 11:00. B02 departs `dep`.
	build := func(dep string) map[string][]byte {
		m := setRow(t, demoFiles(t), FileOnibus, "B01,300,2026-03-04 20:00",
			"B01,300,2026-03-04 12:30,30,2026-03-05 05:00,90,150,91,2026-03-05 05:02")
		return setRow(t, m, FileOnibus, "B02,300,2026-03-04 22:00",
			"B02,300,2026-03-04 22:00,40,"+dep+",85,150,70,2026-03-05 06:00")
	}
	// 11:00 on the 4th + 2970 min = 12:30 on the 6th -> horizon exactly 3000: kept
	ns, ws := demoNights(t, build("2026-03-06 12:30"), NightOptions{})
	if len(ns) != 2 || ns[0].Scenario.Horizon != 3000 || len(withoutPartial(ws)) != 0 {
		t.Fatalf("horizon 3000: nights=%d ws=%v", len(ns), ws)
	}
	// one minute more -> 3001: omitted with a warning, the other night stays
	ns, ws = demoNights(t, build("2026-03-06 12:31"), NightOptions{})
	if len(ns) != 1 || ns[0].Key != "2026-03-05" {
		t.Fatalf("nights = %+v", ns)
	}
	if ws = withoutPartial(ws); len(ws) != 1 || ws[0].File != FileOnibus || ws[0].Line == 0 ||
		!strings.Contains(ws[0].Message, "2026-03-04") || !strings.Contains(ws[0].Message, "3000") {
		t.Errorf("warnings = %+v", ws)
	}
}

func TestNightsRealDepartureExtendsHorizon(t *testing.T) {
	// B01 left (real) 2 h after the planned 05:00: 07:00 = 720 min after 19:00 -> 750
	m := setRow(t, demoFiles(t), FileOnibus, "B01,300,2026-03-04 20:00",
		"B01,300,2026-03-04 20:00,30,2026-03-05 05:00,90,150,91,2026-03-05 07:00")
	ns, _ := demoNights(t, m, NightOptions{})
	if ns[0].Scenario.Horizon != 750 {
		t.Errorf("horizon = %d, want 750", ns[0].Scenario.Horizon)
	}
	if ns[0].Scenario.Buses[0].Bus.DepartureMin != 600 {
		t.Errorf("the scenario keeps the planned departure, got %d", ns[0].Scenario.Buses[0].Bus.DepartureMin)
	}
}

func TestNightsDefaultBatteryPower(t *testing.T) {
	chargers := "carregador_id,potencia_max_kw\nC01,100\nC02,100\nC03,150\n"
	m := withFile(demoFiles(t), FileCarregadores, chargers)
	m = setRow(t, m, FileOnibus, "B01,300,2026-03-04 20:00",
		"B01,300,2026-03-04 20:00,30,2026-03-05 05:00,90,,91,2026-03-05 05:02")
	ns, _ := demoNights(t, m, NightOptions{})
	// default = the most common charger maximum (ties: the larger); explicit values kept
	if got := ns[0].Scenario.Buses[0].Bus.MaxBatteryKW; got != 100 {
		t.Errorf("default MaxBatteryKW = %v, want 100", got)
	}
	if got := ns[0].Scenario.Buses[1].Bus.MaxBatteryKW; got != 150 {
		t.Errorf("explicit MaxBatteryKW = %v, want 150", got)
	}
	// tie between 100 and 150 -> 150
	m = withFile(m, FileCarregadores, "carregador_id,potencia_max_kw\nC01,100\nC02,150\n")
	m = without(m, FileSessoes, FilePotencia)
	ns, _ = demoNights(t, m, NightOptions{})
	if got := ns[0].Scenario.Buses[0].Bus.MaxBatteryKW; got != 150 {
		t.Errorf("tie default MaxBatteryKW = %v, want 150", got)
	}
}

func TestNightsPeakWindowCrossingMidnight(t *testing.T) {
	// 22:00-06:00 is peak. The simulator's Tariff cannot express it (PriceAt would
	// never be peak), so the real cost is computed correctly and Nights warns.
	m := mutate(t, demoFiles(t), FileGaragem, "18:00,21:00", "22:00,06:00")
	ns, ws := demoNights(t, m, NightOptions{})
	wantPtr(t, "measured CostBRL", ns[1].Real.CostBRL, 65*2.70)
	found := false
	for _, w := range ws {
		if w.File == FileGaragem && strings.Contains(w.Message, "meia-noite") {
			found = true
		}
	}
	if !found {
		t.Errorf("no midnight-crossing warning: %v", ws)
	}
	// sessions-only: B01 20:00-24:00 = 50 kWh/h, 20-22 off-peak, 22-24 peak;
	// B02 22:00-02:00 and B03 23:30-03:30 lie entirely inside the 22:00-06:00 peak
	ns, _ = demoNights(t, without(m, FilePotencia), NightOptions{})
	wantPtr(t, "estimated CostBRL night1", ns[0].Real.CostBRL, 100*0.90+100*2.70+120*2.70+160*2.70)
}

func TestNightsPowerWithoutBusesWarns(t *testing.T) {
	power := string(demoFiles(t)[FilePotencia]) + "2026-04-01 22:00,C01,10\n2026-04-01 22:15,C01,0\n"
	ns, ws := demoNights(t, withFile(demoFiles(t), FilePotencia, power), NightOptions{})
	if len(ns) != 2 {
		t.Fatalf("nights = %d", len(ns))
	}
	found := false
	for _, w := range ws {
		if w.File == FilePotencia && strings.Contains(w.Message, "2 leituras") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning for readings outside any night: %v", ws)
	}
}

func TestNightsNoOutcomeAtAll(t *testing.T) {
	m := mutate(t, demoFiles(t), FileOnibus, "2026-03-05 05:00,90,150,91,2026-03-05 05:02", "2026-03-05 05:00,90,150,,")
	m = mutate(t, m, FileOnibus, "2026-03-05 06:00,85,150,70,2026-03-05 06:00", "2026-03-05 06:00,85,150,,")
	m = mutate(t, m, FileOnibus, "2026-03-05 05:30,90,150,90,2026-03-05 05:30", "2026-03-05 05:30,90,150,,")
	ns, _ := demoNights(t, m, NightOptions{})
	r := ns[0].Real
	if r.Buses != 3 || r.WithOutcome != 0 || r.Ready != 0 || r.ReadyPct != 0 || r.ShortfallKWh != 0 {
		t.Errorf("real = %+v", r)
	}
	for _, b := range ns[0].BusReal {
		if b.Ready != nil || b.FinalKWh != nil {
			t.Errorf("BusReal = %+v", b)
		}
	}
}

func TestNightsJSON(t *testing.T) {
	ns, _ := demoNights(t, without(demoFiles(t), FileSessoes, FilePotencia), NightOptions{})
	b, err := json.Marshal(ns[0])
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["Scenario"]; ok {
		t.Error("Scenario must not be serialized")
	}
	for k := range got {
		if k != strings.ToLower(k) {
			t.Errorf("non snake_case key %q", k)
		}
	}
	if got["key"] != "2026-03-04" {
		t.Errorf("key = %v", got["key"])
	}
	real, ok := got["real"].(map[string]any)
	if !ok {
		t.Fatalf("real = %v", got["real"])
	}
	for _, k := range []string{"buses", "with_outcome", "ready", "ready_pct", "shortfall_kwh", "energy_kwh", "peak_kw", "peak_estimated", "cost_brl", "cost_estimated",
		"partial", "covered_buses", "partial_note"} {
		if _, ok := real[k]; !ok {
			t.Errorf("real.%s missing: %v", k, real)
		}
	}
	for _, k := range []string{"energy_kwh", "peak_kw", "cost_brl"} {
		if real[k] != nil {
			t.Errorf("real.%s = %v, want null", k, real[k])
		}
	}
	br, ok := got["bus_real"].([]any)
	if !ok || len(br) != 3 {
		t.Fatalf("bus_real = %v", got["bus_real"])
	}
	first := br[0].(map[string]any)
	if first["id"] != "B01" || first["ready"] != true || first["final_kwh"] != 273.0 {
		t.Errorf("bus_real[0] = %v", first)
	}
}

func TestNightsRealNeverNonFinite(t *testing.T) {
	ns, _ := demoNights(t, demoFiles(t), NightOptions{})
	for _, n := range ns {
		r := n.Real
		for _, v := range []float64{r.ReadyPct, r.ShortfallKWh} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Errorf("%s: non-finite %v", n.Key, v)
			}
		}
		for _, p := range []*float64{r.EnergyKWh, r.PeakKW, r.CostBRL} {
			if p != nil && (math.IsNaN(*p) || math.IsInf(*p, 0)) {
				t.Errorf("%s: non-finite %v", n.Key, *p)
			}
		}
	}
}

func TestNightsDoesNotMutateDataset(t *testing.T) {
	d := loadFiles(t, demoFiles(t))
	before := len(d.Warnings)
	power := append([]PowerSample(nil), d.Power...)
	d.Nights(NightOptions{})
	d.Nights(NightOptions{SoCNoiseKWh: 1})
	if len(d.Warnings) != before {
		t.Error("Nights must not append to d.Warnings")
	}
	for i := range power {
		if power[i] != d.Power[i] {
			t.Fatalf("Power[%d] changed", i)
		}
	}
}

func TestNightsBusesSortedByArrivalThenID(t *testing.T) {
	// shuffled input rows (and an arrival tie) never change the scenario
	orig := demoFiles(t)
	m := mutate(t, orig, FileOnibus, "B02,300,2026-03-04 22:00", "B02,300,2026-03-04 20:00")
	lines := strings.Split(strings.TrimRight(string(m[FileOnibus]), "\n"), "\n")
	shuffled := []string{lines[0]}
	for i := len(lines) - 1; i >= 1; i-- {
		shuffled = append(shuffled, lines[i])
	}
	sh := withFile(m, FileOnibus, strings.Join(shuffled, "\n")+"\n")

	a, _ := demoNights(t, m, NightOptions{})
	b, _ := demoNights(t, sh, NightOptions{})
	for i := range a {
		if !reflect.DeepEqual(a[i].Scenario, b[i].Scenario) {
			t.Errorf("night %s: scenario depends on row order", a[i].Key)
		}
	}
	got := []string{}
	for _, bs := range b[0].Scenario.Buses {
		got = append(got, bs.Bus.ID)
	}
	if !reflect.DeepEqual(got, []string{"B01", "B02", "B03"}) { // B01 and B02 tie at 20:00
		t.Errorf("order = %v", got)
	}
	// ArrivalMin is non-decreasing even when IDs say otherwise
	m2 := mutate(t, orig, FileOnibus, "B01,300,2026-03-04 20:00", "B01,300,2026-03-04 23:00")
	c, _ := demoNights(t, m2, NightOptions{})
	got = got[:0]
	for _, bs := range c[0].Scenario.Buses {
		got = append(got, bs.Bus.ID)
	}
	if !reflect.DeepEqual(got, []string{"B02", "B01", "B03"}) {
		t.Errorf("order by arrival = %v", got)
	}
}

// withoutPartial drops the "N de M ônibus sem sessão" warnings of the demo's night 2.
func withoutPartial(ws []Warning) []Warning {
	var out []Warning
	for _, w := range ws {
		if !strings.Contains(w.Message, "sem sessão") {
			out = append(out, w)
		}
	}
	return out
}

func TestNightsPartialCoverage(t *testing.T) {
	ns, ws := demoNights(t, demoFiles(t), NightOptions{})
	n1, n2 := ns[0].Real, ns[1].Real
	if n1.Partial || n1.CoveredBuses != 3 || n1.PartialNote != "" {
		t.Errorf("night 1 has a session for each of its 3 buses: %+v", n1)
	}
	// night 2: sessions for B01 and B02 only; the power readings do not name buses
	if !n2.Partial || n2.CoveredBuses != 2 || n2.Buses != 3 || n2.PartialNote != "parcial: 2 de 3 ônibus" {
		t.Errorf("night 2: %+v", n2)
	}
	// the values stay (hand-computed in TestNightsDemoSecondNight), they are sealed, not nulled
	wantPtr(t, "EnergyKWh", n2.EnergyKWh, 65)
	wantPtr(t, "PeakKW", n2.PeakKW, 140)
	if len(ws) != 1 || ws[0].File != FileSessoes ||
		ws[0].Message != "noite 2026-03-05: 1 de 3 ônibus sem sessão: energia, pico e custo reais cobrem só os demais" {
		t.Errorf("warnings = %+v", ws)
	}
}

func TestNightsPartialNeedsSomethingToSeal(t *testing.T) {
	// no sessoes.csv at all: nothing is known to be missing
	ns, ws := demoNights(t, without(demoFiles(t), FileSessoes), NightOptions{})
	for _, n := range ns {
		if n.Real.Partial || n.Real.CoveredBuses != n.Real.Buses {
			t.Errorf("%s without sessoes.csv: %+v", n.Key, n.Real)
		}
	}
	if len(ws) != 0 {
		t.Errorf("warnings = %+v", ws)
	}
	// neither sessions nor power: no figure, nothing to seal
	ns, ws = demoNights(t, without(demoFiles(t), FileSessoes, FilePotencia), NightOptions{})
	for _, n := range ns {
		if n.Real.Partial {
			t.Errorf("%s has no figure to be partial: %+v", n.Key, n.Real)
		}
	}
	if len(ws) != 0 {
		t.Errorf("warnings = %+v", ws)
	}
}

func TestNightsPartialWhenSessionsFileHasNothingForTheNight(t *testing.T) {
	// sessoes.csv only has night 1; night 2 gets its energy from the readings alone
	m := withFile(demoFiles(t), FileSessoes, "onibus_id,carregador_id,inicio,fim,energia_kwh\n"+
		"B01,C01,2026-03-04 20:00,2026-03-05 00:00,200\nB02,C02,2026-03-04 22:00,2026-03-05 02:00,120\nB03,C03,2026-03-04 23:30,2026-03-05 03:30,160\n")
	ns, ws := demoNights(t, m, NightOptions{})
	r := ns[1].Real
	if !r.Partial || r.CoveredBuses != 0 || r.PartialNote != "parcial: 0 de 3 ônibus" {
		t.Errorf("night 2: %+v", r)
	}
	wantPtr(t, "EnergyKWh", r.EnergyKWh, 65) // from the readings
	if len(ws) != 1 || !strings.Contains(ws[0].Message, "nenhuma sessão") {
		t.Errorf("warnings = %+v", ws)
	}
	// the same file without readings: no figure for night 2, nothing to warn about
	ns, ws = demoNights(t, without(m, FilePotencia), NightOptions{})
	if ns[1].Real.Partial || len(ws) != 0 {
		t.Errorf("night 2 without data: %+v / %+v", ns[1].Real, ws)
	}
}

func TestNightsPartialWhenReadingsAndSessionsDisagree(t *testing.T) {
	// B03 gets a session too, so all 3 buses are covered; the sessions then add up
	// to 165 kWh against 65 kWh integrated from the readings.
	sess := string(demoFiles(t)[FileSessoes]) + "B03,C03,2026-03-06 01:00,2026-03-06 02:00,100\n"
	m := withFile(demoFiles(t), FileSessoes, sess)
	ns, ws := demoNights(t, m, NightOptions{})
	r := ns[1].Real
	if !r.Partial || r.CoveredBuses != 3 || r.PartialNote != "parcial: sessões e leituras de potência divergem" {
		t.Errorf("night 2: %+v", r)
	}
	if len(ws) != 1 || !strings.Contains(ws[0].Message, "165 kWh") || !strings.Contains(ws[0].Message, "65 kWh") || !strings.Contains(ws[0].Message, "20%") {
		t.Errorf("warnings = %+v", ws)
	}
	// a small difference (70 vs 65 kWh, 7%) is fine
	sess = string(demoFiles(t)[FileSessoes]) + "B03,C03,2026-03-06 01:00,2026-03-06 02:00,5\n"
	ns, ws = demoNights(t, withFile(demoFiles(t), FileSessoes, sess), NightOptions{})
	if ns[1].Real.Partial || len(ws) != 0 {
		t.Errorf("7%% apart must not warn: %+v / %+v", ns[1].Real, ws)
	}
	// both gaps at once: the note lists both
	sess = string(demoFiles(t)[FileSessoes]) + "B01,C03,2026-03-06 01:00,2026-03-06 02:00,100\n"
	ns, ws = demoNights(t, withFile(demoFiles(t), FileSessoes, sess), NightOptions{})
	if n := ns[1].Real.PartialNote; n != "parcial: 2 de 3 ônibus; sessões e leituras de potência divergem" || len(ws) != 2 {
		t.Errorf("note = %q, warnings = %+v", n, ws)
	}
}
