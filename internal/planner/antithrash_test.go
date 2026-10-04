package planner

import (
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// thrashDepot: two chargers held by full buses D1 and D2, and W waiting with a large
// need. Call 1 recommends that W takes a charger from a full bus.
func thrashDepot() Input {
	return Input{
		Site:     model.Site{LimitKW: 500, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
		Buses: []model.Bus{
			testBus("D1", "C1", 220, 220, 600),
			testBus("D2", "C2", 220, 220, 610),
			testBus("W", "", 100, 220, 300),
		},
	}
}

// swapOnce runs call 1, checks the swap and applies it as operators would: the
// incoming bus takes the charger and the outgoing one waits unplugged.
func swapOnce(t *testing.T, pl *Planner) (Input, Swap) {
	t.Helper()
	in := thrashDepot()
	p := pl.Plan(in)
	if len(p.Swaps) != 1 || p.Swaps[0].InBusID != "W" {
		t.Fatalf("call 1 must recommend W in: %+v", p.Swaps)
	}
	s := p.Swaps[0]
	in.Buses = append([]model.Bus(nil), in.Buses...)
	for i := range in.Buses {
		switch in.Buses[i].ID {
		case s.InBusID:
			in.Buses[i].ChargerID = s.ChargerID
		case s.OutBusID:
			in.Buses[i].ChargerID = ""
		}
	}
	return in, s
}

func setSoC(in *Input, id string, soc float64) {
	for i := range in.Buses {
		if in.Buses[i].ID == id {
			in.Buses[i].SoCKWh = soc
		}
	}
}

func hasNote(p Plan, prefix string) bool {
	for _, n := range p.Notes {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

// A bus that just gave way is not swapped back by noise alone: for two hours its
// readings jitter around its (full) target, single dips 13-14 kWh below it, mean 0.
// A single snapshot (PlanNormal), or a threshold on the latest reading alone,
// would swap it back on such a dip.
func TestAntiThrashNoiseDoesNotBringBackABusThatGaveWay(t *testing.T) {
	pl := New(testConfig())
	in, s := swapOnce(t, pl)
	out := s.OutBusID
	noise := []float64{2, -14, 12, -3, 3, -13, 13, 0} // single dips of 13-14 kWh, mean 0
	snapshotSwaps := 0
	for k := 1; k <= 120; k++ {
		in.Now = k
		setSoC(&in, out, 220+noise[k%len(noise)])
		if ps := PlanNormal(testConfig(), in).Swaps; len(ps) > 0 && ps[0].InBusID == out {
			snapshotSwaps++
		}
		p := pl.Plan(in)
		if v := Violations(in, p); len(v) != 0 {
			t.Fatalf("minute %d: violations %v", k, v)
		}
		for _, sw := range p.Swaps {
			if sw.InBusID == out {
				t.Fatalf("minute %d: %s gave way at minute 0 and is swapped back on noise alone: %+v", k, out, sw)
			}
		}
	}
	if snapshotSwaps == 0 {
		t.Fatalf("test is vacuous: the snapshot planner never wanted %s back", out)
	}
	t.Logf("a single snapshot would have swapped %s back in %d of 120 minutes", out, snapshotSwaps)
}

// A genuinely needy bus still comes back: its readings since it was unplugged average
// 40 kWh short of target (it was let go on a noisy high reading). It waits out the
// cooldown and is then swapped back.
func TestAntiThrashGenuinelyNeedyBusStillComesBack(t *testing.T) {
	cfg := testConfig()
	pl := New(cfg)
	in, s := swapOnce(t, pl)
	out := s.OutBusID
	back := -1
	for k := 1; k <= 60 && back < 0; k++ {
		in.Now = k
		setSoC(&in, out, 180+float64(k%3)) // about 40 kWh short
		p := pl.Plan(in)
		for _, sw := range p.Swaps {
			if sw.InBusID == out {
				back = k
			}
		}
		if back < 0 && k > 1 && !hasNote(p, "rodízio não recomendado") {
			t.Fatalf("minute %d: the swap back was dropped without a note: %+v", k, p.Notes)
		}
	}
	if back != cfg.SwapBackCooldownMin {
		t.Errorf("%s swapped back at minute %d, want %d (right after the cooldown)", out, back, cfg.SwapBackCooldownMin)
	}
}

// A waiting bus that never gave way is not affected (W in call 1), and with both
// knobs at 0 the rule is off: the noisy reading brings the bus straight back.
func TestAntiThrashOffSwapsBackOnASingleReading(t *testing.T) {
	cfg := testConfig()
	cfg.SwapBackCooldownMin, cfg.SwapBackMinNeedKWh = 0, 0
	pl := New(cfg)
	in, s := swapOnce(t, pl)
	in.Now = 1
	setSoC(&in, s.OutBusID, 215)
	p := pl.Plan(in)
	if len(p.Swaps) != 1 || p.Swaps[0].InBusID != s.OutBusID {
		t.Errorf("with the rule off a 5 kWh dip must bring %s back (the old behaviour): %+v", s.OutBusID, p.Swaps)
	}
}

// The memory ends when the bus is plugged into a healthy charger again (it charges,
// so its readings no longer average one constant SoC) or leaves the depot.
func TestAntiThrashMemoryEndsWhenTheBusChargesOrLeaves(t *testing.T) {
	pl := New(testConfig())
	in, s := swapOnce(t, pl)
	in.Now = 1
	pl.Plan(in)
	if _, ok := pl.swaps.gaveWay[s.OutBusID]; !ok {
		t.Fatalf("%s must be remembered while it waits", s.OutBusID)
	}
	in.Now = 2
	in.Chargers = append(in.Chargers, testCharger("C3"))
	setCharger := func(id, c string) {
		for i := range in.Buses {
			if in.Buses[i].ID == id {
				in.Buses[i].ChargerID = c
			}
		}
	}
	setCharger(s.OutBusID, "C3")
	pl.Plan(in)
	if _, ok := pl.swaps.gaveWay[s.OutBusID]; ok {
		t.Errorf("%s is charging on C3 and must be forgotten", s.OutBusID)
	}
}

// Operators may take a few minutes: while the bus is still on the charger it was told
// to give away, its readings are not averaged and the memory is kept.
func TestAntiThrashWaitsForTheOperatorsToUnplug(t *testing.T) {
	pl := New(testConfig())
	in := thrashDepot()
	p := pl.Plan(in)
	if len(p.Swaps) != 1 {
		t.Fatalf("call 1 must recommend a swap: %+v", p.Swaps)
	}
	out := p.Swaps[0].OutBusID
	in.Now = 1 // nobody moved yet
	pl.Plan(in)
	g, ok := pl.swaps.gaveWay[out]
	if !ok || g.left || g.count != 0 {
		t.Errorf("%s has not been unplugged yet: memory %+v (present %v)", out, g, ok)
	}
}
