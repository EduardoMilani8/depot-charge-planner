package planner

import (
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
