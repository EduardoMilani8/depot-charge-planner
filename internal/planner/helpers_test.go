package planner

import (
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func testCharger(id string) model.Charger {
	return model.Charger{ID: id, MaxKW: 150, MinKW: 5, Efficiency: 1, Status: model.ChargerOK}
}

func testBus(id, chargerID string, soc, target float64, dep int) model.Bus {
	return model.Bus{ID: id, CapacityKWh: 300, SoCKWh: soc, SoCConfidence: 1, TargetKWh: target,
		ArrivalMin: 0, DepartureMin: dep, MaxBatteryKW: 150, ChargerID: chargerID}
}

func testConfig() Config {
	c := DefaultConfig()
	c.MarginKWh = 0
	return c
}

func sp(p Plan, chargerID string) float64 {
	for _, s := range p.Setpoints {
		if s.ChargerID == chargerID {
			return s.KW
		}
	}
	return 0
}

func statusOf(p Plan, busID string) BusStatus {
	for _, b := range p.Buses {
		if b.BusID == busID {
			return b
		}
	}
	return BusStatus{}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestAvailableKW(t *testing.T) {
	in := Input{Site: model.Site{LimitKW: 100, StepMin: 1}}
	if got := AvailableKW(in); got != 100 {
		t.Errorf("got %v want 100", got)
	}
	off := testCharger("C1")
	off.Status = model.ChargerOffline
	off.LastCommandedKW = 60
	in.Chargers = []model.Charger{off, testCharger("C2")}
	if got := AvailableKW(in); got != 40 {
		t.Errorf("offline draw must be reserved: got %v want 40", got)
	}
	off.LastCommandedKW = 500
	in.Chargers = []model.Charger{off}
	if got := AvailableKW(in); got != 0 {
		t.Errorf("never negative: got %v", got)
	}
	in.Site.LimitKW = math.NaN()
	if got := AvailableKW(in); got != 0 {
		t.Errorf("NaN limit must yield 0, got %v", got)
	}
}

func TestMaxGridKW(t *testing.T) {
	c := testCharger("C1")
	c.Efficiency = 0.5
	b := testBus("B1", "C1", 0, 100, 60)
	b.MaxBatteryKW = 50 // grid side = 100
	if got := MaxGridKW(c, b); got != 100 {
		t.Errorf("got %v want 100", got)
	}
}
