package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/billing"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func nearf(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.IsNaN(got) || math.IsInf(got, 0) || math.Abs(got-want) > 1e-6*math.Max(1, math.Abs(want)) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

// With the default parameters nothing may change: legacyMetrics (legacy_metrics_test.go)
// was recorded from the code before the billing change.
func TestDefaultsKeepEveryOldMetric(t *testing.T) {
	for _, name := range ControllerNames {
		for seed := int64(1); seed <= 5; seed++ {
			key := fmt.Sprintf("%s/%d", name, seed)
			sc, ctrl, _ := ForController(name, planner.DefaultConfig(), Generate(DefaultGenParams(), seed))
			got := Run(sc, ctrl, nil)
			if got.Bill != nil {
				t.Errorf("%s: Bill = %+v, want nil without demand prices or contract", key, *got.Bill)
			}
			got.PlanP99Micros = 0
			if want := legacyMetrics[key]; !reflect.DeepEqual(got, want) {
				t.Errorf("%s: metrics changed\n got  %+v\n want %+v", key, got, want)
			}
		}
	}
}

func TestDefaultMetricsJSONHasNoBill(t *testing.T) {
	sc, ctrl, _ := ForController("fifo", planner.DefaultConfig(), Generate(DefaultGenParams(), 1))
	b, err := json.Marshal(Run(sc, ctrl, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "bill") {
		t.Errorf("default metrics JSON mentions bill: %s", b)
	}
}

func TestTariffIsTheBillingTariff(t *testing.T) {
	var a Tariff = billing.Tariff{PeakFromMin: 1, PeakToMin: 2}
	_ = a
	tf := Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9}
	if tf.PriceAt(1100) != 2.7 || tf.PriceAt(1300) != 0.9 || tf.PriceAt(1100+1440) != 2.7 {
		t.Error("PriceAt through the alias is wrong")
	}
}

func TestArrivalWindowDefaultIsIdentical(t *testing.T) {
	for _, prof := range []FaultProfile{ProfileNone, ProfileSevere, ProfileRandom} {
		p := DefaultGenParams()
		p.Profile = prof
		base := Generate(p, 3)
		q := p
		q.ArrivalFromMin, q.ArrivalToMin = 120, 360
		if !reflect.DeepEqual(base, Generate(q, 3)) {
			t.Errorf("%s: window 120..360 differs from the default", prof)
		}
		q.ArrivalFromMin, q.ArrivalToMin = 0, 0
		if !reflect.DeepEqual(base, Generate(q, 3)) {
			t.Errorf("%s: window 0..0 differs from the default", prof)
		}
	}
}

func TestArrivalWindowOnlyMovesArrivals(t *testing.T) {
	p := DefaultGenParams()
	p.Profile = ProfileSevere
	p.ArrivalFromMin, p.ArrivalToMin = 0, 180
	sc := Generate(p, 4)
	d := DefaultGenParams()
	d.Profile = ProfileSevere
	ref := Generate(d, 4)
	lo, hi := math.MaxInt32, -1
	for i, b := range sc.Buses {
		a := b.Bus.ArrivalMin
		if a < 0 || a > 180 {
			t.Errorf("%s arrives at %d, want 0..180", b.Bus.ID, a)
		}
		lo, hi = min(lo, a), max(hi, a)
		// exactly one rng draw per bus: everything else, faults included, is unchanged
		ref.Buses[i].Bus.ArrivalMin = a
	}
	if hi-lo < 100 {
		t.Errorf("arrivals span only %d..%d: the window is not being used", lo, hi)
	}
	if !reflect.DeepEqual(sc.Buses, ref.Buses) || !reflect.DeepEqual(sc.Faults, ref.Faults) {
		t.Error("the window changed something besides the arrival minute (extra rng draws?)")
	}
}

func TestArrivalWindowEdges(t *testing.T) {
	p := DefaultGenParams()
	p.ArrivalFromMin, p.ArrivalToMin = 200, 201 // two values: 200 and 201
	seen := map[int]bool{}
	for _, b := range Generate(p, 1).Buses {
		seen[b.Bus.ArrivalMin] = true
	}
	if len(seen) != 2 || !seen[200] || !seen[201] {
		t.Errorf("arrivals = %v, want both ends 200 and 201 (inclusive window)", seen)
	}
}

func TestGenerateCopiesDemandToTariff(t *testing.T) {
	p := DefaultGenParams()
	p.Demand = DemandParams{PeakPrice: 50, OffPeakPrice: 20, ContractedKW: 1500, Verde: true}
	tf := Generate(p, 1).Tariff
	if tf.DemandPeakPrice != 50 || tf.DemandOffPeakPrice != 20 || tf.ContractedKW != 1500 || !tf.DemandVerde {
		t.Errorf("tariff = %+v", tf)
	}
	if tf.PeakFromMin != 1080 || tf.PeakToMin != 1260 || tf.PeakPrice != 2.70 || tf.OffPeakPrice != 0.90 {
		t.Errorf("energy part of the tariff changed: %+v", tf)
	}
}

// blockMax computes, independently of billing.Meter, the largest 15-minute average of
// the per-minute total physical kW, with blocks aligned to the clock.
func blockMax(tf Tariff, startClock int, physical []float64) (all, peak, off float64) {
	sum := map[int]float64{}
	for i, kw := range physical {
		sum[(startClock+i)/15] += kw
	}
	for idx, s := range sum {
		kw := s / 15 // kWh/0.25 h with kWh = sum(kW)/60
		all = math.Max(all, kw)
		if tf.InPeak(idx * 15) {
			peak = math.Max(peak, kw)
		} else {
			off = math.Max(off, kw)
		}
	}
	return
}

func TestBillMatchesIndependentBlocks(t *testing.T) {
	p := DefaultGenParams()
	p.Demand = DemandParams{PeakPrice: 50, OffPeakPrice: 20}
	for _, name := range []string{"fifo", "planner"} {
		sc, ctrl, _ := ForController(name, planner.DefaultConfig(), Generate(p, 1))
		tr := NewTrace()
		m := RunTraced(sc, ctrl, nil, tr)
		if m.Bill == nil {
			t.Fatalf("%s: Bill == nil with demand prices", name)
		}
		all, pk, off := blockMax(sc.Tariff, sc.StartClockMin, tr.Physical)
		if all < 100 {
			t.Fatalf("%s: demand %v kW: the scenario does not exercise billing", name, all)
		}
		nearf(t, name+" DemandKW", m.Bill.DemandKW, all)
		nearf(t, name+" DemandPeakKW", m.Bill.DemandPeakKW, pk)
		nearf(t, name+" DemandOffPeakKW", m.Bill.DemandOffPeakKW, off)
		nearf(t, name+" BilledPeakKW", m.Bill.BilledPeakKW, pk)
		nearf(t, name+" BilledOffPeakKW", m.Bill.BilledOffPeakKW, off)
		nearf(t, name+" DemandBRL", m.Bill.DemandBRL, pk*50+off*20)
		nearf(t, name+" OverrunKW", m.Bill.OverrunKW, 0)
		nearf(t, name+" MonthlyBRL", m.Bill.MonthlyBRL, m.CostBRL*30+m.Bill.DemandBRL)
		// CostBRL is still the per-charger energy cost, same as without demand prices
		q := p
		q.Demand = DemandParams{}
		sc0, ctrl0, _ := ForController(name, planner.DefaultConfig(), Generate(q, 1))
		m0 := Run(sc0, ctrl0, nil)
		if m.CostBRL != m0.CostBRL || m.EnergyKWh != m0.EnergyKWh || m.PeakKW != m0.PeakKW {
			t.Errorf("%s: demand prices changed the energy metrics: cost %v vs %v", name, m.CostBRL, m0.CostBRL)
		}
	}
}

func TestBillVerdeAndContract(t *testing.T) {
	p := DefaultGenParams()
	p.Demand = DemandParams{PeakPrice: 50, OffPeakPrice: 20, ContractedKW: 800, Verde: true}
	sc, ctrl, _ := ForController("fifo", planner.DefaultConfig(), Generate(p, 1))
	tr := NewTrace()
	m := RunTraced(sc, ctrl, nil, tr)
	all, _, _ := blockMax(sc.Tariff, sc.StartClockMin, tr.Physical)
	if m.Bill == nil || all <= 800*1.05 {
		t.Fatalf("scenario does not exceed the contract: bill %v, demand %v", m.Bill, all)
	}
	nearf(t, "BilledOffPeakKW", m.Bill.BilledOffPeakKW, 800)
	nearf(t, "OverrunKW", m.Bill.OverrunKW, all-800)
	nearf(t, "DemandBRL", m.Bill.DemandBRL, 800*20+(all-800)*billing.OverrunFactor*20)
}

func TestBillContractOnlyAppears(t *testing.T) {
	p := DefaultGenParams()
	p.Demand = DemandParams{ContractedKW: 5000}
	sc, ctrl, _ := ForController("fifo", planner.DefaultConfig(), Generate(p, 1))
	m := Run(sc, ctrl, nil)
	if m.Bill == nil {
		t.Fatal("a contract alone must produce a Bill")
	}
	nearf(t, "DemandBRL (free demand, within contract)", m.Bill.DemandBRL, 0)
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), `"bill":{"demand_kw":`) {
		t.Errorf("JSON does not carry the bill block: %s", b)
	}
}

func TestBillIsDeterministic(t *testing.T) {
	p := DefaultGenParams()
	p.Demand = DemandParams{PeakPrice: 50, OffPeakPrice: 20}
	run := func() BillMetrics {
		sc, ctrl, _ := ForController("edf", planner.DefaultConfig(), Generate(p, 2))
		return *Run(sc, ctrl, nil).Bill
	}
	if a, b := run(), run(); a != b {
		t.Errorf("not deterministic: %+v vs %+v", a, b)
	}
}

func TestAggregateAveragesBill(t *testing.T) {
	a := Aggregate([]Metrics{
		{Buses: 1, Bill: &BillMetrics{DemandKW: 100, DemandPeakKW: 80, DemandOffPeakKW: 100, BilledPeakKW: 80, BilledOffPeakKW: 100,
			OverrunKW: 10, DemandBRL: 1000, OverrunBRL: 100, MonthlyBRL: 5000}},
		{Buses: 1},
		{Buses: 1, Bill: &BillMetrics{DemandKW: 200, DemandPeakKW: 120, DemandOffPeakKW: 200, BilledPeakKW: 120, BilledOffPeakKW: 200,
			OverrunKW: 30, DemandBRL: 3000, OverrunBRL: 300, MonthlyBRL: 7000}},
	})
	want := BillMetrics{DemandKW: 150, DemandPeakKW: 100, DemandOffPeakKW: 150, BilledPeakKW: 100, BilledOffPeakKW: 150,
		OverrunKW: 20, DemandBRL: 2000, OverrunBRL: 200, MonthlyBRL: 6000}
	if a.Bill == nil || *a.Bill != want {
		t.Errorf("Aggregate Bill = %+v, want %+v (mean over the runs that have one)", a.Bill, want)
	}
	if n := Aggregate([]Metrics{{Buses: 1}, {Buses: 2}}); n.Bill != nil {
		t.Errorf("Bill = %+v, want nil when no run has one", n.Bill)
	}
	// the inputs are not modified or aliased
	in := []Metrics{{Bill: &BillMetrics{DemandKW: 10}}}
	out := Aggregate(in)
	out.Bill.DemandKW = 99
	if in[0].Bill.DemandKW != 10 {
		t.Error("Aggregate aliased the input Bill")
	}
}

func TestBillSnakeCaseJSON(t *testing.T) {
	b, err := json.Marshal(BillMetrics{})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"demand_kw":0,"demand_peak_kw":0,"demand_offpeak_kw":0,"billed_peak_kw":0,"billed_offpeak_kw":0,"overrun_kw":0,"demand_brl":0,"overrun_brl":0,"monthly_brl":0}`
	if string(b) != want {
		t.Errorf("JSON = %s\nwant   %s", b, want)
	}
}

func TestValidateParamsDemandAndArrival(t *testing.T) {
	cfg := planner.DefaultConfig()
	ok := DefaultGenParams()
	for name, mut := range map[string]func(*GenParams){
		"window":          func(p *GenParams) { p.ArrivalFromMin, p.ArrivalToMin = 0, 180 },
		"window max":      func(p *GenParams) { p.ArrivalFromMin, p.ArrivalToMin = 599, 600 },
		"prices at max":   func(p *GenParams) { p.Demand = DemandParams{PeakPrice: 1e6, OffPeakPrice: 1e6} },
		"contract at max": func(p *GenParams) { p.Demand.ContractedKW = MaxLimitKW },
	} {
		p := ok
		mut(&p)
		if err := ValidateParams(p, cfg, 5); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	cases := []struct {
		name  string
		mut   func(*GenParams)
		field string
	}{
		{"from negative", func(p *GenParams) { p.ArrivalFromMin, p.ArrivalToMin = -1, 100 }, "arrival-from"},
		{"from == to", func(p *GenParams) { p.ArrivalFromMin, p.ArrivalToMin = 100, 100 }, "arrival-to"},
		{"from > to", func(p *GenParams) { p.ArrivalFromMin, p.ArrivalToMin = 200, 100 }, "arrival-to"},
		{"to zero with from", func(p *GenParams) { p.ArrivalFromMin = 50 }, "arrival-to"},
		{"to too large", func(p *GenParams) { p.ArrivalFromMin, p.ArrivalToMin = 0, 601 }, "arrival-to"},
		{"from at the end", func(p *GenParams) { p.ArrivalFromMin, p.ArrivalToMin = 600, 601 }, "arrival-from"},
		{"far beyond the horizon", func(p *GenParams) { p.ArrivalFromMin, p.ArrivalToMin = 1400, 1500 }, "arrival-from"},
		{"to negative", func(p *GenParams) { p.ArrivalToMin = -5 }, "arrival-to"},
		{"peak price negative", func(p *GenParams) { p.Demand.PeakPrice = -1 }, "demand-peak-price"},
		{"peak price NaN", func(p *GenParams) { p.Demand.PeakPrice = math.NaN() }, "demand-peak-price"},
		{"peak price Inf", func(p *GenParams) { p.Demand.PeakPrice = math.Inf(1) }, "demand-peak-price"},
		{"peak price huge", func(p *GenParams) { p.Demand.PeakPrice = 1e6 + 1 }, "demand-peak-price"},
		{"offpeak price negative", func(p *GenParams) { p.Demand.OffPeakPrice = -0.01 }, "demand-offpeak-price"},
		{"offpeak price NaN", func(p *GenParams) { p.Demand.OffPeakPrice = math.NaN() }, "demand-offpeak-price"},
		{"offpeak price huge", func(p *GenParams) { p.Demand.OffPeakPrice = 1e7 }, "demand-offpeak-price"},
		{"contract negative", func(p *GenParams) { p.Demand.ContractedKW = -1 }, "demand-contracted"},
		{"contract NaN", func(p *GenParams) { p.Demand.ContractedKW = math.NaN() }, "demand-contracted"},
		{"contract Inf", func(p *GenParams) { p.Demand.ContractedKW = math.Inf(1) }, "demand-contracted"},
		{"contract huge", func(p *GenParams) { p.Demand.ContractedKW = MaxLimitKW * 2 }, "demand-contracted"},
	}
	for _, tc := range cases {
		p := ok
		tc.mut(&p)
		err := ValidateParams(p, cfg, 5)
		var fe *FieldError
		if !errors.As(err, &fe) {
			t.Errorf("%s: want *FieldError, got %v", tc.name, err)
			continue
		}
		if fe.Field != tc.field {
			t.Errorf("%s: field %q, want %q", tc.name, fe.Field, tc.field)
		}
	}
}

func TestMaxArrivalWindowStaysBeforeEveryDeparture(t *testing.T) {
	if MaxArrivalMin != 600 {
		t.Fatalf("MaxArrivalMin = %d, want 600 (below the earliest generated departure, 630)", MaxArrivalMin)
	}
	p := DefaultGenParams()
	p.ArrivalFromMin, p.ArrivalToMin = 0, MaxArrivalMin
	if err := ValidateParams(p, planner.DefaultConfig(), 5); err != nil {
		t.Fatalf("0..%d must be valid: %v", MaxArrivalMin, err)
	}
	p.ArrivalToMin = MaxArrivalMin + 1
	var fe *FieldError
	if err := ValidateParams(p, planner.DefaultConfig(), 5); !errors.As(err, &fe) || fe.Field != "arrival-to" {
		t.Fatalf("To=%d: err = %v, want arrival-to", p.ArrivalToMin, err)
	}
	p.ArrivalToMin = MaxArrivalMin
	for seed := int64(1); seed <= 20; seed++ {
		sc := Generate(p, seed)
		for _, b := range sc.Buses {
			if b.Bus.ArrivalMin >= b.Bus.DepartureMin || b.Bus.DepartureMin > sc.Horizon {
				t.Fatalf("seed %d: %s arrives %d, departs %d (horizon %d)", seed, b.Bus.ID, b.Bus.ArrivalMin, b.Bus.DepartureMin, sc.Horizon)
			}
		}
	}
	for _, w := range [][2]int{{100, 100}, {200, 100}} {
		p.ArrivalFromMin, p.ArrivalToMin = w[0], w[1]
		if err := ValidateParams(p, planner.DefaultConfig(), 5); err == nil {
			t.Errorf("window %v accepted, want rejected", w)
		}
	}
}

func TestCompareAggregateHasBillForEveryController(t *testing.T) {
	p := DefaultGenParams()
	p.NumBuses, p.NumChargers = 8, 4
	p.Demand = DemandParams{PeakPrice: 50, OffPeakPrice: 20}
	res, err := Compare(context.Background(), p, planner.DefaultConfig(), 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Aggregate.Bill == nil || r.Aggregate.Bill.DemandKW <= 0 {
			t.Errorf("%s: aggregate Bill = %+v", r.Name, r.Aggregate.Bill)
		}
		for _, s := range r.Seeds {
			if s.Metrics.Bill == nil {
				t.Errorf("%s seed %d: Bill == nil", r.Name, s.Seed)
			}
		}
	}
}
