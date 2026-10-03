package planner

import (
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func swapInput(donor, waiting model.Bus, extraChargers ...model.Charger) Input {
	in := Input{
		Site:     model.Site{LimitKW: 500, StepMin: 1},
		Chargers: append([]model.Charger{testCharger("C1")}, extraChargers...),
		Buses:    []model.Bus{donor, waiting},
	}
	return in
}

func TestSwapRecommendedWhenWaitingBusIsUrgent(t *testing.T) {
	donor := testBus("D", "C1", 220, 220, 600) // already at target, laxity 600
	waiting := testBus("W", "", 100, 220, 90)  // laxity 42 < 60
	p := PlanNormal(testConfig(), swapInput(donor, waiting))
	if len(p.Swaps) != 1 {
		t.Fatalf("expected 1 swap, got %+v", p.Swaps)
	}
	s := p.Swaps[0]
	if s.ChargerID != "C1" || s.OutBusID != "D" || s.InBusID != "W" || s.Reason == "" {
		t.Errorf("unexpected swap: %+v", s)
	}
}

func TestNoSwapWhenAFreeChargerExists(t *testing.T) {
	donor := testBus("D", "C1", 220, 220, 600)
	waiting := testBus("W", "", 100, 220, 90)
	p := PlanNormal(testConfig(), swapInput(donor, waiting, testCharger("C2")))
	if len(p.Swaps) != 0 {
		t.Errorf("a free charger exists, expected no swap: %+v", p.Swaps)
	}
}

func TestNoSwapWhenWaitingBusIsNotUrgent(t *testing.T) {
	donor := testBus("D", "C1", 220, 220, 600)
	waiting := testBus("W", "", 100, 220, 600)
	if p := PlanNormal(testConfig(), swapInput(donor, waiting)); len(p.Swaps) != 0 {
		t.Errorf("unexpected swap: %+v", p.Swaps)
	}
}

func TestNoSwapWhenDonorHasNoSpareLaxity(t *testing.T) {
	donor := testBus("D", "C1", 100, 220, 150) // laxity 102
	waiting := testBus("W", "", 100, 220, 90)  // laxity 42: gap 60 < 120
	if p := PlanNormal(testConfig(), swapInput(donor, waiting)); len(p.Swaps) != 0 {
		t.Errorf("unexpected swap: %+v", p.Swaps)
	}
}

func TestNoSwapWithoutHealthyCharger(t *testing.T) {
	donor := testBus("D", "C1", 220, 220, 600)
	waiting := testBus("W", "", 100, 220, 90)
	in := swapInput(donor, waiting)
	in.Chargers[0].Status = model.ChargerFaulted
	if p := PlanNormal(testConfig(), in); len(p.Swaps) != 0 {
		t.Errorf("unexpected swap: %+v", p.Swaps)
	}
}

// The waiting bus looks urgent and savable by laxity (which ignores the budget), but
// the depot budget cannot deliver what it needs: a swap would only move buses around.
func TestNoSwapWhenTheBudgetCannotSaveTheWaitingBus(t *testing.T) {
	donor := testBus("D", "C1", 220, 220, 600) // full
	waiting := testBus("W", "", 100, 220, 90)  // needs 120 kWh in 90 min: >= 80 kW
	in := swapInput(donor, waiting)
	in.Site.LimitKW = 40
	if p := PlanNormal(testConfig(), in); len(p.Swaps) != 0 {
		t.Errorf("40 kW cannot save W, expected no swap: %+v", p.Swaps)
	}
}

// The donor still needs charge and is on track: unplugging it loses one ready bus to
// gain one, which is not an improvement (and costs a manual move).
func TestNoSwapThatStripsAnOnTrackDonor(t *testing.T) {
	donor := testBus("D", "C1", 100, 220, 600) // needs 120 kWh, laxity 552: on track
	waiting := testBus("W", "", 100, 220, 90)  // laxity 42
	if p := PlanNormal(testConfig(), swapInput(donor, waiting)); len(p.Swaps) != 0 {
		t.Errorf("unexpected swap: %+v", p.Swaps)
	}
}

// Two full donors, three urgent waiting buses, budget for two: exactly two swaps, on
// different chargers, with different buses.
func TestSwapsLimitedByBudget(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 200, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2"), testCharger("C3")},
		Buses: []model.Bus{
			testBus("D1", "C1", 220, 220, 600),
			testBus("D2", "C2", 220, 220, 600),
			testBus("D3", "C3", 220, 220, 600),
			testBus("W1", "", 100, 220, 90), // each needs about 85 kW (5 min move)
			testBus("W2", "", 100, 220, 90),
			testBus("W3", "", 100, 220, 90),
		},
	}
	p := PlanNormal(testConfig(), in)
	if len(p.Swaps) != 2 {
		t.Fatalf("budget fits two incoming buses, got %d swaps: %+v", len(p.Swaps), p.Swaps)
	}
	if p.Swaps[0].ChargerID == p.Swaps[1].ChargerID || p.Swaps[0].InBusID == p.Swaps[1].InBusID ||
		p.Swaps[0].OutBusID == p.Swaps[1].OutBusID {
		t.Errorf("swaps must use distinct chargers and buses: %+v", p.Swaps)
	}
}

// The swap is judged on the donor's own charger: a full donor on a weak charger is not
// useful to a bus that needs high power, a full donor on a strong charger is.
func TestSwapUsesTheDonorsOwnChargerPower(t *testing.T) {
	weak := testCharger("C2")
	weak.MaxKW = 20
	in := Input{
		Site:     model.Site{LimitKW: 500, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), weak},
		Buses: []model.Bus{
			testBus("D1", "C1", 220, 220, 500), // full, strong charger
			testBus("D2", "C2", 220, 220, 700), // full, more laxity, weak charger
			testBus("W", "", 100, 220, 90),
		},
	}
	p := PlanNormal(testConfig(), in)
	if len(p.Swaps) != 1 || p.Swaps[0].ChargerID != "C1" || p.Swaps[0].OutBusID != "D1" {
		t.Fatalf("expected W to take C1 from D1, got %+v", p.Swaps)
	}
	if !strings.Contains(p.Swaps[0].Reason, "C1") {
		t.Errorf("the reason must name the charger: %q", p.Swaps[0].Reason)
	}
}

// The move itself takes Config.SwapMoveMin minutes; a bus whose laxity is smaller
// cannot be saved by a swap.
func TestNoSwapWhenTheMoveTakesLongerThanTheLaxity(t *testing.T) {
	donor := testBus("D", "C1", 220, 220, 600)
	waiting := testBus("W", "", 100, 220, 51) // laxity 3 min < 5 min move
	if p := PlanNormal(testConfig(), swapInput(donor, waiting)); len(p.Swaps) != 0 {
		t.Errorf("unexpected swap: %+v", p.Swaps)
	}
}

// A bus stuck on a faulted charger waits like an unplugged one; the donor goes to the
// waiting area (the faulted charger is not offered to it).
func TestSwapForBusOnAFaultedCharger(t *testing.T) {
	broken := testCharger("C2")
	broken.Status = model.ChargerFaulted
	donor := testBus("D", "C1", 220, 220, 600)
	waiting := testBus("W", "C2", 100, 220, 90)
	p := PlanNormal(testConfig(), swapInput(donor, waiting, broken))
	if len(p.Swaps) != 1 || p.Swaps[0].InBusID != "W" || p.Swaps[0].ChargerID != "C1" {
		t.Errorf("expected W to take C1, got %+v", p.Swaps)
	}
}

// A donor with too little spare laxity is skipped, but a later donor may still serve.
func TestSwapSkipsDonorWithoutGapButTriesOthers(t *testing.T) {
	in := Input{
		Site:     model.Site{LimitKW: 500, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1"), testCharger("C2")},
		Buses: []model.Bus{
			testBus("D1", "C1", 220, 220, 100), // full, laxity 100: gap 58 < 120
			testBus("D2", "C2", 220, 220, 400), // full, laxity 400
			testBus("W", "", 100, 220, 90),
		},
	}
	p := PlanNormal(testConfig(), in)
	if len(p.Swaps) != 1 || p.Swaps[0].OutBusID != "D2" {
		t.Errorf("expected D2 to give way, got %+v", p.Swaps)
	}
}
