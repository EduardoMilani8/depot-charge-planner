package planner

import (
	"fmt"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

func FuzzPlannerRespectsInvariants(f *testing.F) {
	f.Add(1000.0, 150.0, 5.0, 0.95, 100.0, 250.0, 300.0, 150.0, int8(3), int16(120))
	f.Add(0.0, 150.0, 5.0, 1.0, 0.0, 300.0, 300.0, 150.0, int8(5), int16(0))
	f.Add(100.0, 10.0, 20.0, 0.5, -1.0, 50.0, 100.0, 3.0, int8(2), int16(-5))
	f.Fuzz(func(t *testing.T, limit, maxKW, minKW, eff, soc, target, capKWh, battKW float64, n int8, dep int16) {
		count := int(n) % 8
		if count < 0 {
			count = -count
		}
		in := Input{Site: model.Site{LimitKW: limit, StepMin: 1}}
		for i := 0; i < count; i++ {
			id := fmt.Sprint(i)
			in.Chargers = append(in.Chargers, model.Charger{ID: "C" + id, MaxKW: maxKW, MinKW: minKW, Efficiency: eff, Status: model.ChargerOK})
			in.Buses = append(in.Buses, model.Bus{ID: "B" + id, CapacityKWh: capKWh, SoCKWh: soc, SoCConfidence: 1,
				TargetKWh: target, DepartureMin: int(dep), MaxBatteryKW: battKW, ChargerID: "C" + id})
		}
		plan := New(DefaultConfig()).Plan(in)
		if v := Violations(in, plan); len(v) > 0 {
			t.Fatalf("invariants broken: %v\ninput: %+v\nplan: %+v", v, in, plan)
		}
		for _, s := range plan.Setpoints {
			if !finite(s.KW) {
				t.Fatalf("non-finite setpoint %+v", s)
			}
		}
	})
}
