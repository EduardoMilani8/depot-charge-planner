package sim

import (
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// oneChargerTwoBuses: B1 is on the only charger and needs little; B2 waits and needs a
// lot. B2 can only be served if someone unplugs B1 once it is charged.
func oneChargerTwoBuses() Scenario {
	sc := baseScenario()
	sc.Buses = []BusSpec{
		{Bus: model.Bus{ID: "B1", CapacityKWh: 300, SoCKWh: 50, SoCConfidence: 1, TargetKWh: 100,
			ArrivalMin: 0, DepartureMin: 390, MaxBatteryKW: 150}},
		{Bus: model.Bus{ID: "B2", CapacityKWh: 300, SoCKWh: 50, SoCConfidence: 1, TargetKWh: 200,
			ArrivalMin: 1, DepartureMin: 300, MaxBatteryKW: 150}},
	}
	return sc
}

func TestUnplugFullLetsTheWaitingBusCharge(t *testing.T) {
	sc := oneChargerTwoBuses()
	if m := Run(sc, NewFIFO(), nil); m.Ready != 1 {
		t.Fatalf("without operators unplugging, only B1 can be ready: %+v", m)
	}
	sc.UnplugFull = true
	rc := &recordingController{inner: NewFIFO()}
	m := Run(sc, rc, nil)
	if m.Ready != 2 {
		t.Errorf("unplugging the charged B1 must let B2 finish too: %+v", m)
	}
	// B1 leaves the charger only once charged to its forecast target, and B2 takes it.
	for _, in := range rc.ins {
		for _, b := range in.Buses {
			if b.ID == "B1" && b.ChargerID == "" && b.SoCKWh < 100-1e-6 {
				t.Fatalf("minute %d: B1 unplugged at %.1f kWh, before its 100 kWh target", in.Now, b.SoCKWh)
			}
		}
	}
	if rc.ins[len(rc.ins)-1].Now < 1 {
		t.Fatal("no observations")
	}
}

func TestUnplugFullTakesTheMoveTime(t *testing.T) {
	sc := oneChargerTwoBuses()
	sc.UnplugFull = true
	rc := &recordingController{inner: NewFIFO()}
	Run(sc, rc, nil)
	plugged := -1
	var socAt = map[int]float64{}
	for _, in := range rc.ins {
		for _, b := range in.Buses {
			if b.ID == "B2" {
				socAt[in.Now] = b.SoCKWh
				if b.ChargerID == "C1" && plugged < 0 {
					plugged = in.Now
				}
			}
		}
	}
	if plugged < 0 {
		t.Fatal("B2 was never plugged in")
	}
	if socAt[plugged+swapDurationMin] != socAt[plugged] {
		t.Errorf("B2 charged during the %d-minute move: %.1f -> %.1f kWh", swapDurationMin, socAt[plugged], socAt[plugged+swapDurationMin])
	}
	if socAt[plugged+swapDurationMin+3] <= socAt[plugged] {
		t.Errorf("B2 never started charging after the move")
	}
}

func TestUnplugFullOnlyWhenSomeoneWaits(t *testing.T) {
	sc := oneChargerTwoBuses()
	sc.Buses = sc.Buses[:1]
	sc.UnplugFull = true
	rc := &recordingController{inner: NewFIFO()}
	Run(sc, rc, nil)
	for _, in := range rc.ins {
		if len(in.Buses) == 1 && in.Buses[0].ChargerID != "C1" {
			t.Fatalf("minute %d: B1 unplugged although nobody was waiting", in.Now)
		}
	}
}
