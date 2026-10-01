package sim

import (
	"reflect"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func TestRunSingleBusReady(t *testing.T) {
	sc := baseScenario()
	m := Run(sc, plannerCtrl(sc), nil)
	if m.Ready != 1 || m.ReadyPct != 100 {
		t.Errorf("bus should leave ready: %+v", m)
	}
	if m.PlanViolations != 0 || m.OvershootMin != 0 {
		t.Errorf("no violations expected: %+v", m)
	}
	if m.EnergyKWh < 150 {
		t.Errorf("energy delivered %.1f kWh is below the 150 kWh the battery gained", m.EnergyKWh)
	}
	if m.CostBRL <= 0 || m.PeakKW <= 0 {
		t.Errorf("cost/peak not measured: %+v", m)
	}
}

func TestRunFIFOReady(t *testing.T) {
	if m := Run(baseScenario(), NewFIFO(), nil); m.Ready != 1 {
		t.Errorf("FIFO should also finish: %+v", m)
	}
}

func TestRunEDFAndSafeOnlyReady(t *testing.T) {
	for name, c := range map[string]Controller{"edf": NewEDF(), "safe": NewSafeOnly()} {
		if m := Run(baseScenario(), c, nil); m.Ready != 1 {
			t.Errorf("%s: %+v", name, m)
		}
	}
}

func TestRunDeterministic(t *testing.T) {
	sc := baseScenario()
	a := Run(sc, plannerCtrl(sc), nil)
	b := Run(sc, plannerCtrl(sc), nil)
	a.PlanP99Micros, b.PlanP99Micros = 0, 0
	if !reflect.DeepEqual(a, b) {
		t.Errorf("same seed must give identical metrics:\n%+v\n%+v", a, b)
	}
}

func TestRunCountsPlanViolations(t *testing.T) {
	m := Run(baseScenario(), stubController{kw: 1000}, nil)
	if m.PlanViolations == 0 {
		t.Error("commanding 1000 kW on a 500 kW site must be counted")
	}
	if m.PeakKW > 150+1e-6 {
		t.Errorf("physical power %.1f kW exceeds the charger max", m.PeakKW)
	}
}

func TestRunNoPowerMeansShortfall(t *testing.T) {
	m := Run(baseScenario(), stubController{kw: 0}, nil)
	if m.Ready != 0 || m.ShortfallKWh < 149 || m.ShortfallKWh > 151 {
		t.Errorf("expected a 150 kWh shortfall: %+v", m)
	}
}

func TestRunWaitingBusTakesFreedCharger(t *testing.T) {
	sc := baseScenario()
	sc.Buses[0].Bus.DepartureMin = 100
	sc.Buses = append(sc.Buses, BusSpec{Bus: model.Bus{
		ID: "B2", CapacityKWh: 300, SoCKWh: 50, SoCConfidence: 1, TargetKWh: 200,
		ArrivalMin: 0, DepartureMin: 400, MaxBatteryKW: 150,
	}})
	m := Run(sc, plannerCtrl(sc), nil)
	if m.Ready != 2 {
		t.Errorf("both buses should be served in turn: %+v", m)
	}
}

func TestTariffPriceAt(t *testing.T) {
	tf := Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 3, OffPeakPrice: 1}
	cases := map[int]float64{0: 1, 1079: 1, 1080: 3, 1259: 3, 1260: 1, 1440 + 1100: 3}
	for clock, want := range cases {
		if got := tf.PriceAt(clock); got != want {
			t.Errorf("PriceAt(%d) = %v, want %v", clock, got, want)
		}
	}
}
