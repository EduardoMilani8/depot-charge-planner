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

// Operators read the same SoC as the controllers: without a reading they cannot tell
// that B1 is charged, so they do not unplug it.
func TestUnplugFullUsesTheObservedReading(t *testing.T) {
	sc := oneChargerTwoBuses()
	sc.UnplugFull = true
	sc.Faults = []Fault{{Kind: FaultSoCMissing, Target: "B1", From: 0, To: forever}}
	rc := &recordingController{inner: NewFIFO()}
	Run(sc, rc, nil)
	for _, in := range rc.ins {
		for _, b := range in.Buses {
			if b.ID == "B1" && b.ChargerID != "C1" {
				t.Fatalf("minute %d: B1 unplugged although its SoC was never readable", in.Now)
			}
		}
	}
}

// OperatorMoves counts the manual moves operators made in a run: swaps executed for
// the planner (only when they follow them) and buses unplugged for fifo-unplug.
func TestOperatorMovesCountsExecutedSwapsAndUnplugs(t *testing.T) {
	sc := oneChargerTwoBuses()
	if m := Run(sc, NewFIFO(), nil); m.OperatorMoves != 0 {
		t.Errorf("fifo without operators: %v moves, want 0", m.OperatorMoves)
	}
	sc.UnplugFull = true
	if m := Run(sc, NewFIFO(), nil); m.OperatorMoves != 1 {
		t.Errorf("fifo-unplug: %v moves, want 1 (B1 unplugged once)", m.OperatorMoves)
	}

	sc = twoBusScenario()
	sc.Buses[0].Bus.SoCKWh = 270
	sc.Buses[0].Bus.DepartureMin = 400
	sc.Buses = append(sc.Buses, BusSpec{Bus: model.Bus{
		ID: "B3", CapacityKWh: 300, SoCKWh: 0, SoCConfidence: 1, TargetKWh: 240,
		ArrivalMin: 0, DepartureMin: 200, MaxBatteryKW: 150,
	}})
	sc.FollowSwaps = true
	rc := &recordingController{inner: plannerCtrl(sc)}
	m := Run(sc, rc, nil)
	if rc.swaps == 0 || m.OperatorMoves < 1 || m.OperatorMoves > float64(rc.swaps) {
		t.Errorf("planner with swaps followed: %v moves for %d recommended swaps", m.OperatorMoves, rc.swaps)
	}
	sc.FollowSwaps = false
	if m := Run(sc, plannerCtrl(sc), nil); m.OperatorMoves != 0 {
		t.Errorf("swaps not followed: %v moves, want 0", m.OperatorMoves)
	}
	if a := Aggregate([]Metrics{{OperatorMoves: 1}, {OperatorMoves: 2}}); a.OperatorMoves != 1.5 {
		t.Errorf("Aggregate must average the moves per run: %v", a.OperatorMoves)
	}
}
