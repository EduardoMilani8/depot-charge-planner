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

// Swap thrash under noisy SoC readings (fix round 3): with the anti-thrash rule off,
// the planner asked for over 1000 moves per night at 2000 kW in the mild and severe
// profiles (a bus that gave way was swapped back whenever noise made it look short).
// With the defaults it stays near the noise-free 25 moves per night.
func TestPlannerSwapsDoNotThrashUnderNoisySoC(t *testing.T) {
	for _, profile := range []FaultProfile{ProfileMild, ProfileSevere} {
		var runs []Metrics
		for seed := int64(1); seed <= int64(seedCount(3, 1)); seed++ {
			p := DefaultGenParams()
			p.LimitKW = 2000
			p.Profile = profile
			sc := Generate(p, seed)
			runs = append(runs, Run(sc, NewPlannerController(propertyConfig(), sc), nil))
		}
		if moves := Aggregate(runs).OperatorMoves; moves > 100 {
			t.Errorf("%s at 2000 kW: %.1f moves per night, want at most 100 (noise-free: 25)", profile, moves)
		} else {
			t.Logf("%s at 2000 kW: %.1f moves per night", profile, moves)
		}
	}
}

// Round 4: with a gateway that reports every reading 1 minute old (still reliable),
// the anti-thrash rule once never counted a reading and never let a bus that gave way
// back (1200 kW severe, 20 seeds: 60.9 vs 70.9 ready% with the rule off). With
// readings counted by timestamp it must not lose readiness against the rule off.
func TestAntiThrashKeepsReadinessWithAgedReadings(t *testing.T) {
	off := propertyConfig()
	off.SwapBackCooldownMin, off.SwapBackMinNeedKWh = 0, 0
	var on, base []Metrics
	// Enough seeds for a mean: with 2 seeds the rule is 5 points below the rule off
	// (68.0 vs 73.0) and with 20 it is 1.2 above (72.1 vs 70.9); the claim is about the
	// mean over many seeds, so a few seeds are noise (6 seeds: 70.0 vs 69.7).
	for seed := int64(1); seed <= int64(seedCount(8, 6)); seed++ {
		p := DefaultGenParams()
		p.LimitKW = 1200
		p.Profile = ProfileSevere
		p.ReadingAgeMin = 1
		sc := Generate(p, seed)
		on = append(on, Run(sc, NewPlannerController(propertyConfig(), sc), nil))
		base = append(base, Run(sc, NewPlannerController(off, sc), nil))
	}
	a, b := Aggregate(on), Aggregate(base)
	t.Logf("1200 kW severe, readings 1 min old: ready%% %.1f with the rule, %.1f without; moves %.1f vs %.1f", a.ReadyPct, b.ReadyPct, a.OperatorMoves, b.OperatorMoves)
	if a.ReadyPct < b.ReadyPct-0.5 {
		t.Errorf("the anti-thrash rule loses readiness with aged readings: %.1f vs %.1f without it", a.ReadyPct, b.ReadyPct)
	}
}

func TestReadingAgeMinIsReportedAsTheReadingAge(t *testing.T) {
	p := DefaultGenParams()
	p.ReadingAgeMin = 3
	sc := Generate(p, 1)
	if sc.ReadingAgeMin != 3 {
		t.Fatalf("Generate must copy ReadingAgeMin: %d", sc.ReadingAgeMin)
	}
	rc := &recordingController{inner: NewFIFO()}
	Run(sc, rc, nil)
	seen := false
	for _, in := range rc.ins {
		for _, b := range in.Buses {
			seen = true
			if b.SoCAgeMin != 3 {
				t.Fatalf("minute %d bus %s: age %d, want 3", in.Now, b.ID, b.SoCAgeMin)
			}
		}
	}
	if !seen {
		t.Fatal("no bus observed")
	}
}
