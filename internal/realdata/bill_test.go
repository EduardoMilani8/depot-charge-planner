package realdata

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/billing"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// demandHeader / demandRow: the demo garage (400 kW, peak 18:00-21:00, R$ 2.70 / 0.90 per kWh)
// plus the optional demand columns. Empty cells are allowed.
const demandHeader = "limite_kw,ponta_inicio,ponta_fim,preco_ponta,preco_fora_ponta," +
	"demanda_preco_ponta,demanda_preco_fora_ponta,demanda_contratada_kw,modo_demanda,noites_por_mes\n"

func garageWith(t *testing.T, demand string) map[string][]byte {
	t.Helper()
	return withFile(demoFiles(t), FileGaragem, demandHeader+"400,18:00,21:00,2.70,0.90,"+demand+"\n")
}

func wantPtrTol(t *testing.T, name string, got *float64, want, tol float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %v", name, want)
		return
	}
	if math.Abs(*got-want) > tol {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

// Demo night 2 (2026-03-05), measured by potencia.csv:
//
//	22:00 C01 100 + C02 20 = 120 kW; 22:15 C01 100 + C02 40 = 140 kW; 22:30 both 0.
//	Clock-aligned 15-min blocks: 22:00-22:15 = 120 kW x 0.25 h = 30 kWh -> 120 kW;
//	22:15-22:30 = 140 kW x 0.25 h = 35 kWh -> 140 kW. The demand is the largest block: 140 kW.
//	All of it is off-peak (peak 18:00-21:00), so Azul bills 140 kW x R$ 20 = R$ 2800 and the
//	peak window (no use, no contract) bills 0. Energy 65 kWh x 0.90 = 58.5; month = 58.5 x 30 + 2800.
//
// Demo night 1 has sessions only (spread uniformly): B01 200 kWh / 240 min = 50 kW 20:00-00:00,
// B02 120 kWh / 240 min = 30 kW 22:00-02:00, B03 160 kWh / 240 min = 40 kW 23:30-03:30.
//
//	Largest block 23:30-00:00: 50 + 30 + 40 = 120 kW (off-peak). Peak window 20:00-21:00: B01 alone, 50 kW.
//	Azul: 50 x 40 + 120 x 20 = 2000 + 2400 = 4400. Energy 522 -> month = 522 x 30 + 4400 = 20060.
func TestBillDemoNightsWithDemandPrices(t *testing.T) {
	ns, _ := demoNights(t, garageWith(t, "40,20,,,"), NightOptions{})
	n1, n2 := ns[0].Real, ns[1].Real

	wantPtr(t, "night 2 DemandKW", n2.DemandKW, 140)
	wantPtr(t, "night 2 DemandBRL", n2.DemandBRL, 140*20)
	wantPtr(t, "night 2 MonthlyBRL", n2.MonthlyBRL, 58.5*30+2800)
	if n2.DemandEstimated {
		t.Errorf("night 2 comes from the power series: DemandEstimated must be false")
	}
	// the numbers that existed before do not move
	wantPtr(t, "night 2 EnergyKWh", n2.EnergyKWh, 65)
	wantPtr(t, "night 2 PeakKW", n2.PeakKW, 140)
	wantPtr(t, "night 2 CostBRL", n2.CostBRL, 58.5)

	wantPtr(t, "night 1 DemandKW", n1.DemandKW, 120)
	wantPtr(t, "night 1 DemandBRL", n1.DemandBRL, 50*40+120*20)
	wantPtr(t, "night 1 MonthlyBRL", n1.MonthlyBRL, 522*30+4400)
	if !n1.DemandEstimated {
		t.Errorf("night 1 comes from sessions: DemandEstimated must be true")
	}
	wantPtr(t, "night 1 EnergyKWh", n1.EnergyKWh, 480)
	wantPtr(t, "night 1 PeakKW", n1.PeakKW, 120)
	wantPtr(t, "night 1 CostBRL", n1.CostBRL, 522)

	// the scenario carries the whole tariff, so the simulated rows are billed the same way
	if tf := ns[1].Scenario.Tariff; tf.DemandPeakPrice != 40 || tf.DemandOffPeakPrice != 20 || tf.DemandVerde || tf.Nights() != 30 {
		t.Errorf("scenario tariff = %+v", tf)
	}
}

func TestBillNightsPerMonthAndMode(t *testing.T) {
	// 22 nights: month = 58.5 x 22 + 2800
	ns, _ := demoNights(t, garageWith(t, "40,20,,azul,22"), NightOptions{})
	wantPtr(t, "MonthlyBRL 22 nights", ns[1].Real.MonthlyBRL, 58.5*22+2800)
	// a mode in capitals, with spaces, is accepted
	ns, _ = demoNights(t, garageWith(t, "40,20,, Verde ,"), NightOptions{})
	if tf := ns[1].Scenario.Tariff; !tf.DemandVerde {
		t.Errorf("modo_demanda ' Verde ' was not read as verde: %+v", tf)
	}
}

// Readings that do not sit on the 15-minute clock: C01 draws 200 kW from 22:10 to 22:20.
// Energy 200 kW x 10/60 h = 33.33 kWh; blocks 22:00-22:15 and 22:15-22:30 get 5 minutes each,
// 200 x 5/60 = 16.667 kWh -> 66.667 kW each. The instantaneous peak is 200 kW, the demand 66.667 kW.
func TestBillDemandIsBlockAverageNotPeak(t *testing.T) {
	power := "instante,carregador_id,potencia_kw\n2026-03-05 22:10,C01,200\n2026-03-05 22:20,C01,0\n"
	m := withFile(without(garageWith(t, "40,20,,,"), FileSessoes), FilePotencia, power)
	ns, _ := demoNights(t, m, NightOptions{})
	r := ns[1].Real
	wantPtr(t, "PeakKW", r.PeakKW, 200)
	wantPtr(t, "EnergyKWh", r.EnergyKWh, 200.0*10/60)
	wantPtrTol(t, "DemandKW", r.DemandKW, 200.0*5/60/0.25, 1e-9)
	wantPtrTol(t, "DemandBRL", r.DemandBRL, 200.0*5/60/0.25*20, 1e-7)
	if r.DemandEstimated {
		t.Errorf("measured readings must not be estimated")
	}
}

func TestBillVerdeIgnoresPeakDemandPrice(t *testing.T) {
	withPeak, wsA := demoNights(t, garageWith(t, "40,20,,verde,"), NightOptions{})
	noPeak, wsB := demoNights(t, garageWith(t, "0,20,,verde,"), NightOptions{})
	for i := range withPeak {
		a, b := withPeak[i].Real, noPeak[i].Real
		if *a.DemandBRL != *b.DemandBRL || *a.DemandKW != *b.DemandKW || *a.MonthlyBRL != *b.MonthlyBRL {
			t.Errorf("night %d: peak demand price changed a Verde bill: %+v vs %+v", i, a, b)
		}
	}
	// Verde: one charge on the largest block of the whole night, at the off-peak price
	wantPtr(t, "night 1 Verde DemandBRL", withPeak[0].Real.DemandBRL, 120*20)
	wantPtr(t, "night 2 Verde DemandBRL", withPeak[1].Real.DemandBRL, 140*20)
	has := func(ws []Warning) bool {
		for _, w := range ws {
			if w.File == FileGaragem && strings.Contains(w.Message, "demanda_preco_ponta") && strings.Contains(w.Message, "ignorado") {
				return true
			}
		}
		return false
	}
	d := loadFiles(t, garageWith(t, "40,20,,verde,"))
	if !has(d.Warnings) {
		t.Errorf("Verde with a peak demand price must warn that it is ignored: %v", d.Warnings)
	}
	if has(wsA) || has(wsB) {
		t.Errorf("the warning belongs to Load, not to Nights")
	}
	if d := loadFiles(t, garageWith(t, "0,20,,verde,")); has(d.Warnings) {
		t.Errorf("no peak price, no warning: %v", d.Warnings)
	}
	if d := loadFiles(t, garageWith(t, "40,20,,azul,")); has(d.Warnings) {
		t.Errorf("Azul uses the peak price, no warning: %v", d.Warnings)
	}
}

// Contract 100 kW, Azul 40 / 20 R$/kW, night 2 (140 kW measured, all off-peak):
//
//	off-peak: 140 > 1.05 x 100, so billed 100 x 20 = 2000 plus the 40 kW above the contract
//	at twice the price: 40 x 2 x 20 = 1600; peak window: no use, but the contract is billed:
//	100 x 40 = 4000. Total 7600. Verde (one window at 20): 2000 + 1600 = 3600.
func TestBillContractOverrunIsBilledAtTwice(t *testing.T) {
	ns, _ := demoNights(t, garageWith(t, "40,20,100,azul,"), NightOptions{})
	wantPtr(t, "Azul DemandBRL", ns[1].Real.DemandBRL, 4000+2000+1600)
	wantPtr(t, "Azul DemandKW (measured, not billed)", ns[1].Real.DemandKW, 140)
	wantPtr(t, "Azul MonthlyBRL", ns[1].Real.MonthlyBRL, 58.5*30+7600)

	ns, _ = demoNights(t, garageWith(t, "40,20,100,verde,"), NightOptions{})
	wantPtr(t, "Verde DemandBRL", ns[1].Real.DemandBRL, 2000+1600)

	// within the 5% tolerance (contract 135: 140 <= 141.75) the contract is billed, no overrun
	ns, _ = demoNights(t, garageWith(t, "0,20,135,verde,"), NightOptions{})
	wantPtr(t, "Verde in tolerance", ns[1].Real.DemandBRL, 135*20)
}

func TestBillContractAloneBills(t *testing.T) {
	// a contract without prices still counts as demand billing, with zero prices -> R$ 0
	ns, _ := demoNights(t, garageWith(t, ",,100,,"), NightOptions{})
	wantPtr(t, "DemandBRL", ns[1].Real.DemandBRL, 0)
	wantPtr(t, "DemandKW", ns[1].Real.DemandKW, 140)
}

func TestBillWithoutDemandColumnsHasNoDemandFields(t *testing.T) {
	d := loadFiles(t, demoFiles(t))
	if d.Garage != (Garage{LimitKW: 400, PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.70, OffPeakPrice: 0.90, HasTariff: true}) {
		t.Errorf("garage = %+v", d.Garage)
	}
	for _, w := range d.Warnings {
		if w.File == FileGaragem {
			t.Errorf("garagem.csv warning on the old format: %v", w)
		}
	}
	ns, _ := d.Nights(NightOptions{})
	for _, n := range ns {
		r := n.Real
		if r.DemandKW != nil || r.DemandBRL != nil || r.MonthlyBRL != nil || r.DemandEstimated {
			t.Errorf("%s: demand fields without demand prices: %+v", n.Key, r)
		}
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"demand", "monthly"} {
			if strings.Contains(string(b), key) {
				t.Errorf("%s: JSON gained %q: %s", n.Key, key, b)
			}
		}
	}
	rep := demoReplay(t, demoFiles(t), NightOptions{}, 2)
	b, _ := json.Marshal(rep)
	for _, key := range []string{"demand", "monthly", "bill", "nights_per_month"} {
		if strings.Contains(string(b), key) {
			t.Errorf("report JSON gained %q without demand columns", key)
		}
	}
	// the same columns present but empty change nothing either
	e, _ := demoNights(t, garageWith(t, ",,,,"), NightOptions{})
	for i := range ns {
		a, _ := json.Marshal(e[i].Real)
		b, _ := json.Marshal(ns[i].Real)
		if string(a) != string(b) {
			t.Errorf("empty demand cells changed night %d: %s vs %s", i, a, b)
		}
	}
}

func TestBillDemandNeedsACompleteTariff(t *testing.T) {
	m := withFile(demoFiles(t), FileGaragem, "limite_kw,demanda_preco_fora_ponta\n400,20\n")
	d := loadFiles(t, m)
	found := false
	for _, w := range d.Warnings {
		if w.File == FileGaragem && strings.Contains(w.Message, "demanda") && strings.Contains(w.Message, "tarifa") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning that demand needs the energy tariff: %v", d.Warnings)
	}
	ns, _ := d.Nights(NightOptions{})
	for _, n := range ns {
		if n.Real.DemandKW != nil || n.Real.CostBRL != nil {
			t.Errorf("%s: %+v", n.Key, n.Real)
		}
	}
}

func TestBillInvalidGarageColumns(t *testing.T) {
	cases := []struct {
		name, demand, col, want string
	}{
		{"peak price negative", "-1,20,,,", "demanda_preco_ponta", "negativ"},
		{"offpeak price negative", "40,-0.5,,,", "demanda_preco_fora_ponta", "negativ"},
		{"peak price above the limit", "1000001,20,,,", "demanda_preco_ponta", "1000000"},
		{"offpeak price above the limit", "40,2000000,,,", "demanda_preco_fora_ponta", "1000000"},
		{"price not a number", "abc,20,,,", "demanda_preco_ponta", "não é um número"},
		{"contract negative", "40,20,-5,,", "demanda_contratada_kw", "negativ"},
		{"contract above the limit", "40,20,10000001,,", "demanda_contratada_kw", "10000000"},
		{"mode unknown", "40,20,,amarela,", "modo_demanda", "azul ou verde"},
		{"nights above the limit", "40,20,,,32", "noites_por_mes", "31"},
		{"nights negative", "40,20,,,-1", "noites_por_mes", "0 a 31"},
		{"nights fractional", "40,20,,,22.5", "noites_por_mes", "inteiro"},
		{"nights not a number", "40,20,,,muitas", "noites_por_mes", "não é um número"},
	}
	for _, c := range cases {
		_, err := Load(garageWith(t, c.demand))
		var fe *FieldError
		if !errors.As(err, &fe) {
			t.Errorf("%s: err = %v, want a *FieldError", c.name, err)
			continue
		}
		if fe.File != FileGaragem || fe.Line != 2 || fe.Column != c.col || !strings.Contains(fe.Message, c.want) {
			t.Errorf("%s: %+v (want line 2, column %s, message with %q)", c.name, fe, c.col, c.want)
		}
		if !strings.Contains(fe.Error(), "linha 2") || !strings.Contains(fe.Error(), "coluna "+c.col) {
			t.Errorf("%s: text %q lacks line/column", c.name, fe.Error())
		}
	}
	// the edges are accepted
	for _, ok := range []string{"0,0,0,azul,0", "1000000,1000000,10000000,verde,31", "40,20,,AZUL,1"} {
		if _, err := Load(garageWith(t, ok)); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}

func TestBillTariffConversion(t *testing.T) {
	d := loadFiles(t, garageWith(t, "40,20,300,verde,22"))
	want := billing.Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.70, OffPeakPrice: 0.90,
		DemandPeakPrice: 40, DemandOffPeakPrice: 20, DemandVerde: true, ContractedKW: 300, NightsPerMonth: 22}
	if got := d.Garage.Tariff(); got != want {
		t.Errorf("tariff = %+v, want %+v", got, want)
	}
	if err := d.Garage.Tariff().Validate(); err != nil {
		t.Error(err)
	}
	if got := (Garage{LimitKW: 400}).Tariff(); got != (billing.Tariff{}) {
		t.Errorf("no tariff must convert to the zero tariff, got %+v", got)
	}
}

// A window crossing midnight is no longer a reason to refuse the simulated cost:
// the shared billing library prices it. 20:00 to 19:00 is peak for 23 hours, so all the
// energy of night 1 (it starts at 20:00) is at R$ 2.70.
func TestBillWindowCrossingMidnightIsComparable(t *testing.T) {
	m := mutate(t, demoFiles(t), FileGaragem, "18:00,21:00", "20:00,19:00")
	d := loadFiles(t, m)
	ns, ws := d.Nights(NightOptions{})
	for _, w := range ws {
		if strings.Contains(w.Message, "meia-noite") {
			t.Errorf("stale midnight warning: %v", w)
		}
	}
	rep, err := Replay(context.Background(), d, planner.DefaultConfig(), NightOptions{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.CostComparable {
		t.Errorf("a tariff whose window crosses midnight must be comparable")
	}
	for _, a := range rep.Assumptions {
		if strings.Contains(a, "meia-noite") || strings.Contains(a, "não é comparável") {
			t.Errorf("stale assumption: %q", a)
		}
	}
	// the real figures: night 1 sessions at 2.70 for all 480 kWh
	wantPtr(t, "real night 1 cost", ns[0].Real.CostBRL, 480*2.70)
	// the simulated rows too: fifo night 1 bills every kWh at the peak price
	for _, c := range rep.Nights[0].Controllers {
		if c.Name != "fifo" {
			continue
		}
		a := c.Aggregate
		if a.EnergyKWh <= 0 || math.Abs(a.CostBRL-a.EnergyKWh*2.70) > 1e-6 {
			t.Errorf("fifo: cost %v for %v kWh, want %v", a.CostBRL, a.EnergyKWh, a.EnergyKWh*2.70)
		}
	}
}

func TestBillReplayCarriesDemand(t *testing.T) {
	rep := demoReplay(t, garageWith(t, "40,20,,,22"), NightOptions{}, 2)
	if !rep.DemandBilled || rep.NightsPerMonth != 22 {
		t.Errorf("DemandBilled=%v NightsPerMonth=%d", rep.DemandBilled, rep.NightsPerMonth)
	}
	a := rep.RealAggregate
	// mean over the two nights: demand (120 + 140) / 2, demand R$ (4400 + 2800) / 2,
	// month (522 x 22 + 4400 + 58.5 x 22 + 2800) / 2
	wantPtr(t, "aggregate DemandKW", a.DemandKW, 130)
	wantPtr(t, "aggregate DemandBRL", a.DemandBRL, (4400+2800)/2.0)
	wantPtr(t, "aggregate MonthlyBRL", a.MonthlyBRL, (522*22+4400+58.5*22+2800)/2)
	if !a.DemandEstimated {
		t.Errorf("night 1 is estimated: the aggregate is too")
	}
	for _, c := range rep.Nights[1].Controllers {
		if c.Aggregate.Bill == nil {
			t.Errorf("%s: simulated rows must carry the bill", c.Name)
		}
	}
	if without := demoReplay(t, demoFiles(t), NightOptions{}, 2); without.DemandBilled || without.NightsPerMonth != 0 {
		t.Errorf("no demand prices: %v %d", without.DemandBilled, without.NightsPerMonth)
	}
}

// The replay is blind about the future arrivals unless told otherwise: a cost policy without
// AnnounceArrivals leaves the planner exactly as without the policy (deferral needs the schedule).
func TestReplayCostAwareIsBlindUnlessArrivalsAreAnnounced(t *testing.T) {
	files := garageWith(t, "40,20,,,")
	d := loadFiles(t, files)
	run := func(cfg planner.Config, o NightOptions) *Report {
		r, err := Replay(context.Background(), d, cfg, o, 2)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	plain := planner.DefaultConfig()
	costly := planner.DefaultConfig()
	costly.Cost = &planner.CostPolicy{}
	base := run(plain, NightOptions{})
	blind := run(costly, NightOptions{})
	known := run(costly, NightOptions{AnnounceArrivals: true})

	row := func(r *Report, night int, name string) float64 {
		for _, c := range r.Nights[night].Controllers {
			if c.Name == name {
				return c.Aggregate.CostBRL
			}
		}
		t.Fatalf("no row %s", name)
		return 0
	}
	for night := range base.Nights {
		if a, b := row(base, night, "planner"), row(blind, night, "planner"); a != b {
			t.Errorf("night %d: blind cost-aware planner costs %v, the plain one %v", night, b, a)
		}
	}
	// night 1: B01 arrives at 20:00, inside the peak (18:00-21:00), with hours of slack and a
	// plug for every bus: with the schedule known the planner waits for 21:00
	if a, b := row(known, 0, "planner"), row(blind, 0, "planner"); !(a < b-1) {
		t.Errorf("announced arrivals: cost %v, blind %v; deferral should make it cheaper", a, b)
	}
	// the other rows never change
	for night := range base.Nights {
		for _, name := range []string{"fifo", "edf", "fifo-unplug", "safe"} {
			if a, b := row(base, night, name), row(known, night, name); a != b {
				t.Errorf("night %d %s changed: %v vs %v", night, name, a, b)
			}
		}
	}
	// the assumptions say which mode ran
	has := func(r *Report, sub string) bool {
		for _, a := range r.Assumptions {
			if strings.Contains(a, sub) {
				return true
			}
		}
		return false
	}
	if has(base, "política de custo") {
		t.Errorf("no cost policy, no assumption about it: %q", base.Assumptions)
	}
	if !has(blind, "sem agenda") && !has(blind, "às cegas") {
		t.Errorf("blind assumption missing: %q", blind.Assumptions)
	}
	if !has(known, "chegadas anunciadas") {
		t.Errorf("announced assumption missing: %q", known.Assumptions)
	}
}

// Fix round 1: edge cases of the demand blocks, hand-computed.

func writeSessions(t *testing.T, rows string) map[string][]byte {
	t.Helper()
	m := withFile(garageWith(t, "40,20,,,"), FileSessoes, "onibus_id,carregador_id,inicio,fim,energia_kwh\n"+rows)
	return without(m, FilePotencia)
}

// B03 charges 40 kWh from 23:50 to 00:10: 20 minutes, 120 kW. The 23:45 block holds 10 of
// those minutes (120 x 10/60 = 20 kWh -> 80 kW) and the 00:00 block the other 10 (80 kW).
// Demand 80 kW, off-peak: 80 x 20 = 1600 (the peak window has no use).
func TestBillSessionCrossingMidnightSplitsAcrossBlocks(t *testing.T) {
	ns, _ := demoNights(t, writeSessions(t, "B03,C03,2026-03-04 23:50,2026-03-05 00:10,40\n"), NightOptions{})
	r := ns[0].Real
	wantPtrTol(t, "DemandKW", r.DemandKW, 80, 1e-9)
	wantPtrTol(t, "DemandBRL", r.DemandBRL, 1600, 1e-7)
	wantPtr(t, "EnergyKWh", r.EnergyKWh, 40)
}

// A session of 30 seconds counts as one minute (as the cost always did): its 10 kWh all land
// in the 22:00 block, 10 kWh / 0.25 h = 40 kW. The energy is preserved, not dropped.
func TestBillSubMinuteSessionKeepsItsEnergy(t *testing.T) {
	ns, _ := demoNights(t, writeSessions(t, "B02,C02,2026-03-05 22:00:00,2026-03-05 22:00:30,10\n"), NightOptions{})
	r := ns[1].Real
	wantPtr(t, "EnergyKWh", r.EnergyKWh, 10)
	wantPtrTol(t, "DemandKW", r.DemandKW, 40, 1e-9)
}

// C01 reads 100 kW at 22:00:40 and 0 at 22:15:20. The demand floors both to the minute:
// 22:00-22:15 at 100 kW = 25 kWh in one block -> 100 kW. The energy keeps the seconds:
// 100 kW x 880 s / 3600 = 24.44 kWh.
func TestBillReadingsWithSecondsAreFlooredForDemandOnly(t *testing.T) {
	power := "instante,carregador_id,potencia_kw\n2026-03-05 22:00:40,C01,100\n2026-03-05 22:15:20,C01,0\n"
	m := withFile(without(garageWith(t, "40,20,,,"), FileSessoes), FilePotencia, power)
	ns, _ := demoNights(t, m, NightOptions{})
	r := ns[1].Real
	wantPtrTol(t, "DemandKW", r.DemandKW, 100, 1e-9)
	wantPtrTol(t, "EnergyKWh", r.EnergyKWh, 100.0*880/3600, 1e-9)
}

// The demo readings in a scrambled order, with a repeated 22:15 reading of C02 (the last
// one in the file wins, as Load documents): the same 140 kW demand and 65 kWh as the clean file.
func TestBillUnsortedAndDuplicateReadings(t *testing.T) {
	power := "instante,carregador_id,potencia_kw\n" +
		"2026-03-05 22:30,C02,0\n" +
		"2026-03-05 22:15,C02,999\n" +
		"2026-03-05 22:15,C01,100\n" +
		"2026-03-05 22:00,C02,20\n" +
		"2026-03-05 22:30,C01,0\n" +
		"2026-03-05 22:15,C02,40\n" +
		"2026-03-05 22:00,C01,100\n"
	m := withFile(without(garageWith(t, "40,20,,,"), FileSessoes), FilePotencia, power)
	ns, _ := demoNights(t, m, NightOptions{})
	r := ns[1].Real
	wantPtrTol(t, "DemandKW", r.DemandKW, 140, 1e-9)
	wantPtrTol(t, "EnergyKWh", r.EnergyKWh, 65, 1e-9)
	wantPtrTol(t, "DemandBRL", r.DemandBRL, 2800, 1e-7)
}

func TestBillModeIsCaseInsensitive(t *testing.T) {
	for _, mode := range []string{"vErDe", "VERDE", "Verde"} {
		d := loadFiles(t, garageWith(t, "40,20,,"+mode+","))
		if !d.Garage.DemandVerde {
			t.Errorf("%q was not read as verde", mode)
		}
	}
	for _, mode := range []string{"AzUl", "AZUL"} {
		if d := loadFiles(t, garageWith(t, "40,20,,"+mode+",")); d.Garage.DemandVerde {
			t.Errorf("%q was read as verde", mode)
		}
	}
}

func TestBillNaNAndInfAreRejectedWithLineAndColumn(t *testing.T) {
	cases := []struct{ demand, col string }{
		{"NaN,20,,,", "demanda_preco_ponta"},
		{"40,Inf,,,", "demanda_preco_fora_ponta"},
		{"40,20,-Inf,,", "demanda_contratada_kw"},
		{"40,20,+Inf,,", "demanda_contratada_kw"},
		{"40,20,,,NaN", "noites_por_mes"},
		{"1e999,20,,,", "demanda_preco_ponta"},
		{"40,20,1e999,,", "demanda_contratada_kw"},
	}
	for _, c := range cases {
		_, err := Load(garageWith(t, c.demand))
		var fe *FieldError
		if !errors.As(err, &fe) || fe.File != FileGaragem || fe.Line != 2 || fe.Column != c.col || fe.Message == "" {
			t.Errorf("%q: %v (want garagem.csv, linha 2, coluna %s)", c.demand, err, c.col)
			continue
		}
		if !strings.Contains(fe.Error(), "linha 2") || !strings.Contains(fe.Error(), "coluna "+c.col) {
			t.Errorf("%q: %q", c.demand, fe.Error())
		}
	}
}
