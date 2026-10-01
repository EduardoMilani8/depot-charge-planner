package planner

import (
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func fourBusInput(limit float64) Input {
	in := Input{Now: 0, Site: model.Site{LimitKW: limit, StepMin: 1}}
	for _, id := range []string{"1", "2", "3", "4"} {
		in.Chargers = append(in.Chargers, testCharger("C"+id))
		in.Buses = append(in.Buses, testBus("B"+id, "C"+id, 100, 200, 300))
	}
	return in
}

func TestPlanSafeEqualShare(t *testing.T) {
	p := PlanSafe(fourBusInput(400))
	if p.Layer != LayerSafe {
		t.Errorf("layer = %v", p.Layer)
	}
	for _, id := range []string{"C1", "C2", "C3", "C4"} {
		if got := sp(p, id); got != 100 {
			t.Errorf("%s = %v, want 100", id, got)
		}
	}
}

func TestPlanSafeBelowFloorTurnsOff(t *testing.T) {
	p := PlanSafe(fourBusInput(10)) // share 2.5 kW < 5 kW floor
	for _, s := range p.Setpoints {
		if s.KW != 0 {
			t.Errorf("%s = %v, want 0 (below floor)", s.ChargerID, s.KW)
		}
	}
}

func TestPlanSafeIgnoresSoC(t *testing.T) {
	in := fourBusInput(100)
	in.Buses = in.Buses[:1]
	in.Chargers = in.Chargers[:1]
	in.Buses[0].SoCKWh = math.NaN()
	if got := sp(PlanSafe(in), "C1"); got != 100 {
		t.Errorf("got %v, want 100", got)
	}
}

func TestPlanSafeSkipsUnhealthyCharger(t *testing.T) {
	in := fourBusInput(300)
	in.Chargers[0].Status = model.ChargerFaulted
	p := PlanSafe(in)
	if len(p.Setpoints) != 3 {
		t.Fatalf("got %d setpoints, want 3", len(p.Setpoints))
	}
	if got := sp(p, "C2"); got != 100 {
		t.Errorf("C2 = %v, want 100", got)
	}
}

func TestPlanSafeReservesOfflineDraw(t *testing.T) {
	in := fourBusInput(100)
	in.Buses = in.Buses[:2]
	in.Chargers = in.Chargers[:2]
	in.Chargers[0].Status = model.ChargerOffline
	in.Chargers[0].LastCommandedKW = 60
	if got := sp(PlanSafe(in), "C2"); got != 40 {
		t.Errorf("C2 = %v, want 40 (limit minus offline draw)", got)
	}
}

func TestPlanSafeEmpty(t *testing.T) {
	p := PlanSafe(Input{Site: model.Site{LimitKW: 100, StepMin: 1}})
	if len(p.Setpoints) != 0 {
		t.Errorf("empty depot should have no setpoints")
	}
}
