package sim

import (
	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func baseScenario() Scenario {
	return Scenario{
		Name: "base", Seed: 1, StartClockMin: 1080, Horizon: 400, BaseLimitKW: 500,
		Chargers: []model.Charger{{ID: "C1", MaxKW: 150, MinKW: 5, Efficiency: 0.95, Status: model.ChargerOK}},
		Buses: []BusSpec{{Bus: model.Bus{
			ID: "B1", CapacityKWh: 300, SoCKWh: 50, SoCConfidence: 1, TargetKWh: 200,
			ArrivalMin: 0, DepartureMin: 300, MaxBatteryKW: 150,
		}}},
		Tariff:      Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9},
		FollowSwaps: true,
	}
}

// stubController always commands the same power on C1.
type stubController struct{ kw float64 }

func (s stubController) Plan(in planner.Input) planner.Plan {
	return planner.Plan{Setpoints: []planner.Setpoint{{ChargerID: "C1", KW: s.kw}}}
}

func plannerCtrl(sc Scenario) Controller {
	cfg := planner.DefaultConfig()
	return NewPlannerController(cfg, sc)
}
