package sim

import (
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// minutesToReach charges one battery-limited bus (charger power = battery power,
// efficiency 1) at full power from socKWh and returns the simulated minutes until the
// true SoC reaches target. Commands take effect one step later, so charging starts at
// minute 1.
func minutesToReach(t *testing.T, socKWh, target float64) int {
	t.Helper()
	sc := baseScenario()
	sc.Chargers[0].Efficiency = 1
	sc.Buses[0].Bus.SoCKWh = socKWh
	sc.Buses[0].Bus.TargetKWh = target
	rc := &recordingController{inner: stubController{kw: 150}}
	Run(sc, rc, nil)
	for _, in := range rc.ins {
		if len(in.Buses) == 1 && in.Buses[0].SoCKWh >= target-1e-9 {
			return in.Now - 1
		}
	}
	t.Fatalf("SoC never reached %v kWh", target)
	return 0
}

// The planner budgets the energy above the taper knee at PlannerTailCostFactor times
// its plain duration. This measures the real extra time in the simulator's physics.
func TestTaperExtraTimeAgainstPlannerAssumption(t *testing.T) {
	const capKWh, powerKW = 300.0, 150.0
	knee := model.TaperStartFrac * capKWh
	plainMin := func(kwh float64) float64 { return kwh / powerKW * 60 }

	// 80% -> 100%: the continuous-time factor is ln(5)/0.8 = 2.012, slightly above 2.0.
	full := float64(minutesToReach(t, knee, capKWh))
	plain := plainMin(capKWh - knee) // 24 min
	exact := math.Log(5) / 0.8
	t.Logf("80->100%%: %.0f min simulated, %.0f min without taper (factor %.3f); planner assumes %.1f; continuous maths %.3f",
		full, plain, full/plain, model.PlannerTailCostFactor, exact)
	// The 1-minute discretisation can only shift the result by about one step.
	if math.Abs(full-exact*plain) > 1.5 {
		t.Errorf("simulated 80->100%% took %.0f min, continuous maths says %.1f", full, exact*plain)
	}
	assumed := plainMin(model.EffectiveEnergy(knee, capKWh, capKWh))
	if full > assumed+1.5 {
		t.Errorf("planner assumes %.1f min for 80->100%%, the simulator needs %.0f", assumed, full)
	}

	// For targets up to 95% (the generator's highest route target) the constant is
	// conservative: the planner never assumes less time than the physics needs.
	for _, frac := range []float64{0.85, 0.90, 0.95} {
		target := frac * capKWh
		got := float64(minutesToReach(t, knee, target))
		assumed := plainMin(model.EffectiveEnergy(knee, target, capKWh))
		t.Logf("80->%.0f%%: %.0f min simulated, planner assumes %.1f", frac*100, got, assumed)
		if got > math.Ceil(assumed) {
			t.Errorf("80->%.0f%%: simulated %.0f min exceeds the planner's %.1f min", frac*100, got, assumed)
		}
	}
}
