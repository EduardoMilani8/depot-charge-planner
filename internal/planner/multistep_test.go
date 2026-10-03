package planner

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// weird returns a usually sensible value, sometimes (about 1 in 5) a hostile one.
func weird(r *rand.Rand, normal float64) float64 {
	switch r.Intn(35) {
	case 0:
		return math.NaN()
	case 1:
		return math.Inf(1)
	case 2:
		return math.Inf(-1)
	case 3:
		return -normal - 1
	case 4:
		return 0
	case 5:
		return 1e15
	case 6:
		return normal * (0.5 + r.Float64()) // perturbed spec
	}
	return normal
}

func randomCharger(r *rand.Rand) model.Charger {
	ids := []string{"C0", "C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", ""}
	c := model.Charger{
		ID:              ids[r.Intn(len(ids))], // small pool: duplicates and empty IDs happen
		MaxKW:           weird(r, 150),
		MinKW:           weird(r, 5),
		Efficiency:      weird(r, 0.95),
		Status:          model.ChargerStatus(r.Intn(3) / 2), // mostly OK, some faulted
		LastCommandedKW: weird(r, 60*r.Float64()),
	}
	switch r.Intn(15) {
	case 0:
		c.Status = model.ChargerStatus(7) // unknown status
	case 1:
		c.Status = model.ChargerOffline
	}
	return c
}

func randomBus(r *rand.Rand, now int) model.Bus {
	ids := []string{"B0", "B1", "B2", "B3", "B4", "B5", "B6", "B7", "B8", "B9", "B10", "B11", ""}
	chargers := []string{"C0", "C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "", "X"}
	return model.Bus{
		ID:            ids[r.Intn(len(ids))],
		CapacityKWh:   weird(r, 300),
		SoCKWh:        weird(r, 300*r.Float64()),
		SoCAgeMin:     r.Intn(40) - 5,
		SoCConfidence: weird(r, r.Float64()),
		TargetKWh:     weird(r, 250),
		ArrivalMin:    now - r.Intn(200) + r.Intn(30),
		DepartureMin:  now + r.Intn(600) - 50,
		MaxBatteryKW:  weird(r, 150),
		ChargerID:     chargers[r.Intn(len(chargers))],
	}
}

func randomGarbagePlan(r *rand.Rand) Plan {
	var p Plan
	for i := 0; i < r.Intn(8); i++ {
		p.Setpoints = append(p.Setpoints, Setpoint{ChargerID: fmt.Sprintf("C%d", r.Intn(7)), KW: weird(r, 500*r.Float64())})
	}
	return p
}

// One Planner lives through consecutive cycles with hostile, changing inputs (non-finite
// and huge values, duplicate IDs, unknown statuses, perturbed specs, clock jumps) and
// a normal layer that alternately works, returns garbage and panics. Every plan must
// satisfy the invariants and Plan must never panic.
func TestPlannerMultiStepHostileInputs(t *testing.T) {
	seeds := 2000
	if testing.Short() {
		seeds = 200
	}
	layers := map[Layer]int{}
	powered, corrected := 0, 0
	for seed := 0; seed < seeds; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		call := 0
		pl := New(DefaultConfig()).WithNormal(func(c Config, in Input) Plan {
			call++
			switch call % 3 {
			case 1:
				return randomGarbagePlan(rand.New(rand.NewSource(int64(seed*100 + call))))
			case 2:
				panic("injected")
			}
			return PlanNormal(c, in)
		})
		now := r.Intn(100)
		in := Input{Now: now, Site: model.Site{LimitKW: 800, StepMin: 1}}
		for i := 0; i < r.Intn(9); i++ {
			in.Chargers = append(in.Chargers, randomCharger(r))
		}
		for i := 0; i < r.Intn(11); i++ {
			in.Buses = append(in.Buses, randomBus(r, now))
		}
		for step := 0; step < 6; step++ {
			switch r.Intn(5) { // clock: forward, backwards, big jump
			case 0:
				in.Now -= r.Intn(20)
			case 1:
				in.Now += 1000
			default:
				in.Now++
			}
			if r.Intn(4) == 0 {
				in.Site.LimitKW = weird(r, 800)
			} else {
				in.Site.LimitKW = 800 * r.Float64()
			}
			in.Site.StepMin = 1
			if r.Intn(10) == 0 {
				in.Site.StepMin = r.Intn(3) - 1
			}
			// Mutate copies: the planner may keep goroutines reading earlier inputs.
			in.Chargers = append([]model.Charger(nil), in.Chargers...)
			in.Buses = append([]model.Bus(nil), in.Buses...)
			for k := 0; k < 1+r.Intn(3); k++ {
				switch {
				case len(in.Chargers) > 0 && r.Intn(2) == 0:
					in.Chargers[r.Intn(len(in.Chargers))] = randomCharger(r)
				case len(in.Buses) > 0 && r.Intn(2) == 0:
					in.Buses[r.Intn(len(in.Buses))] = randomBus(r, in.Now)
				case r.Intn(2) == 0:
					in.Chargers = append(in.Chargers, randomCharger(r))
				default:
					in.Buses = append(in.Buses, randomBus(r, in.Now))
				}
			}
			plan, panicked := safePlan(pl, in)
			if panicked != nil {
				t.Fatalf("seed %d step %d: Plan panicked: %v\ninput: %+v", seed, step, panicked, in)
			}
			if v := Violations(in, plan); len(v) != 0 {
				t.Fatalf("seed %d step %d (layer %v): %v\ninput: %+v\nplan: %+v", seed, step, plan.Layer, v, in, plan.Setpoints)
			}
			layers[plan.Layer]++
			for _, s := range plan.Setpoints {
				if s.KW > 0 {
					powered++
					break
				}
			}
			for _, n := range plan.Notes {
				if strings.HasPrefix(n, "verificador corrigiu") {
					corrected++
				}
			}
		}
	}
	// Not vacuous: every layer was reached, a fair number of plans powered chargers (about
	// 1 in 9 cycles at the time of writing), and the verifier
	// had garbage to correct.
	t.Logf("layers %v, plans with power %d, verifier corrections %d", layers, powered, corrected)
	if layers[LayerNormal] == 0 || layers[LayerLastValid] == 0 || layers[LayerSafe] == 0 || powered*4 < seeds || corrected == 0 {
		t.Errorf("the hostile run did not exercise every path: layers %v, powered %d, corrected %d", layers, powered, corrected)
	}
}

func safePlan(pl *Planner, in Input) (p Plan, panicked any) {
	defer func() { panicked = recover() }()
	return pl.Plan(in), nil
}
