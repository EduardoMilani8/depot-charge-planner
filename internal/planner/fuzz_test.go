package planner

import (
	"fmt"
	"math"
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
		pl := New(DefaultConfig())
		check := func(label string, in Input, plan Plan) {
			if v := Violations(in, plan); len(v) > 0 {
				t.Fatalf("%s: invariants broken: %v\ninput: %+v\nplan: %+v", label, v, in, plan)
			}
			for _, s := range plan.Setpoints {
				if !finite(s.KW) {
					t.Fatalf("%s: non-finite setpoint %+v", label, s)
				}
			}
		}
		check("first plan", in, pl.Plan(in))

		// Second cycle on the SAME planner (so the last-valid layer can replay the
		// first plan) with one charger spec perturbed by the fuzz-provided floats.
		if count > 0 {
			in2 := in
			in2.Now++
			in2.Chargers = append([]model.Charger(nil), in.Chargers...)
			i := int(uint8(dep)) % count
			switch {
			case n%2 != 0:
				in2.Chargers[i].MaxKW = math.NaN()
			case minKW-maxKW != maxKW:
				in2.Chargers[i].MaxKW = minKW - maxKW
			default:
				in2.Chargers[i].MaxKW = battKW
			}
			if n%3 == 0 {
				in2.Chargers[i].MinKW = math.NaN()
			}
			check("second plan", in2, pl.Plan(in2))
		}
	})
}
