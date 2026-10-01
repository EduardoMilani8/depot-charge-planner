package planner

import (
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func validInput() Input {
	return Input{
		Now:      0,
		Site:     model.Site{LimitKW: 500, StepMin: 1},
		Chargers: []model.Charger{testCharger("C1")},
		Buses:    []model.Bus{testBus("B1", "C1", 100, 200, 300)},
	}
}

func TestValidateInput(t *testing.T) {
	if err := ValidateInput(validInput()); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	cases := map[string]func(*Input){
		"negative limit":        func(in *Input) { in.Site.LimitKW = -1 },
		"nan limit":             func(in *Input) { in.Site.LimitKW = math.NaN() },
		"zero step":             func(in *Input) { in.Site.StepMin = 0 },
		"duplicate charger":     func(in *Input) { in.Chargers = append(in.Chargers, testCharger("C1")) },
		"empty charger id":      func(in *Input) { in.Chargers[0].ID = "" },
		"charger min above max": func(in *Input) { in.Chargers[0].MinKW = 200 },
		"bad efficiency":        func(in *Input) { in.Chargers[0].Efficiency = 0 },
		"duplicate bus":         func(in *Input) { in.Buses = append(in.Buses, testBus("B1", "", 0, 100, 300)) },
		"unknown charger ref":   func(in *Input) { in.Buses[0].ChargerID = "X" },
		"two buses one charger": func(in *Input) { in.Buses = append(in.Buses, testBus("B2", "C1", 0, 100, 300)) },
		"zero capacity":         func(in *Input) { in.Buses[0].CapacityKWh = 0 },
		"zero battery power":    func(in *Input) { in.Buses[0].MaxBatteryKW = 0 },
	}
	for name, mutate := range cases {
		in := validInput()
		mutate(&in)
		if ValidateInput(in) == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestValidateInputAcceptsBadSoC(t *testing.T) {
	// Out-of-range SoC is handled as an unreliable reading, not as invalid input.
	for _, soc := range []float64{math.NaN(), -5, 9999} {
		in := validInput()
		in.Buses[0].SoCKWh = soc
		if err := ValidateInput(in); err != nil {
			t.Errorf("soc %v rejected: %v", soc, err)
		}
	}
}

func TestValidateEmptyInput(t *testing.T) {
	in := Input{Site: model.Site{LimitKW: 0, StepMin: 1}}
	if err := ValidateInput(in); err != nil {
		t.Fatalf("empty depot must be valid: %v", err)
	}
}
