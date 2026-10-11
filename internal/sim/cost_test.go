package sim

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func costTestConfig(pol *planner.CostPolicy) planner.Config {
	c := planner.DefaultConfig()
	c.Timeout = time.Second
	c.Cost = pol
	return c
}

// The caller supplies only the margin and the demand target; the scenario fills the
// clock and the peak window, on a copy.
func TestScenarioConfigFillsThePolicyFromTheScenarioOnACopy(t *testing.T) {
	sc := baseScenario() // 18:00 at minute 0, peak 1080..1260
	caller := &planner.CostPolicy{ClockAtZeroMin: 5, PeakFromMin: 7, PeakToMin: 9, DeferMarginMin: 30, DemandTargetKW: 400}
	before := *caller
	cfg := ScenarioConfig(costTestConfig(caller), sc)
	if *caller != before {
		t.Errorf("the caller's policy was modified: %+v, want %+v", *caller, before)
	}
	want := planner.CostPolicy{ClockAtZeroMin: 1080, PeakFromMin: 1080, PeakToMin: 1260, DeferMarginMin: 30, DemandTargetKW: 400}
	if cfg.Cost == nil || *cfg.Cost != want {
		t.Errorf("policy = %+v, want %+v", cfg.Cost, want)
	}
	if cfg.Cost == caller {
		t.Error("the policy must be a copy")
	}
	// Without a policy the configuration is untouched.
	plain := planner.DefaultConfig()
	if got := ScenarioConfig(plain, sc); got.Cost != nil || !reflect.DeepEqual(got, plain) {
		t.Errorf("a nil policy must stay nil and the config unchanged: %+v", got)
	}
}

// The planner controller defers with the scenario's window whatever the caller put in
// the policy, and ForController passes the configuration through.
func TestPlannerControllerDefersInsideTheScenarioPeak(t *testing.T) {
	sc := baseScenario() // bus B1 needs 150 kWh and leaves at minute 300; the peak ends at minute 180
	for name, pol := range map[string]*planner.CostPolicy{
		"only the margin": {DeferMarginMin: 45},
		"stale window":    {ClockAtZeroMin: 5, PeakFromMin: 7, PeakToMin: 9, DeferMarginMin: 45},
	} {
		cfg := costTestConfig(pol)
		for _, build := range map[string]func() Controller{
			"NewPlannerController": func() Controller { return NewPlannerController(cfg, sc) },
			"ForController": func() Controller {
				_, c, err := ForController("planner", cfg, sc)
				if err != nil {
					t.Fatal(err)
				}
				return c
			},
		} {
			tr := NewTrace()
			m := RunTraced(sc, build(), nil, tr)
			first := tr.Decisions[0]
			if len(first.Buses) != 1 || !strings.HasPrefix(first.Buses[0].Reason, "adiado para depois da ponta") {
				t.Errorf("%s: first decision = %+v, want B1 deferred", name, first.Buses)
			}
			if m.Ready != 1 || m.PlanViolations != 0 {
				t.Errorf("%s: ready %d violations %d, want 1 and 0", name, m.Ready, m.PlanViolations)
			}
			if tr.Commanded[100] != 0 || tr.Commanded[200] <= 0 {
				t.Errorf("%s: commanded %.1f kW at minute 100 and %.1f at 200, want 0 and > 0", name, tr.Commanded[100], tr.Commanded[200])
			}
		}
	}
	// Cheaper than charging at once, and no later than the departure.
	with := Run(sc, NewPlannerController(costTestConfig(&planner.CostPolicy{}), sc), nil)
	without := Run(sc, NewPlannerController(costTestConfig(nil), sc), nil)
	if with.CostBRL >= without.CostBRL || with.Ready != without.Ready {
		t.Errorf("with the policy R$ %.2f ready %d, without R$ %.2f ready %d", with.CostBRL, with.Ready, without.CostBRL, without.Ready)
	}
}

// A nil policy keeps the decision log header as it was: no Cost key at all. A policy
// is written and read back, and a log of a cost-aware run replays without differences
// when its header carries the policy the controller really used.
func TestDecisionLogCarriesTheCostPolicy(t *testing.T) {
	var plain bytes.Buffer
	NewDecisionLogWithConfig(&plain, planner.DefaultConfig())
	if plain.Len() == 0 || strings.Contains(plain.String(), "Cost") {
		t.Errorf("a nil policy must not appear in the header: %s", plain.String())
	}

	sc := baseScenario()
	cfg := ScenarioConfig(costTestConfig(&planner.CostPolicy{DeferMarginMin: 45}), sc)
	var buf bytes.Buffer
	log := NewDecisionLogWithConfig(&buf, cfg)
	Run(sc, NewPlannerController(costTestConfig(&planner.CostPolicy{DeferMarginMin: 45}), sc), log)
	if err := log.Err(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	h, _, err := ReadDecisionLogWithHeader(bytes.NewReader(data))
	if err != nil || h == nil || h.Config.Cost == nil || *h.Config.Cost != *cfg.Cost {
		t.Fatalf("header policy = %+v (err %v), want %+v", h, err, cfg.Cost)
	}
	diffs, replayed, _, err := ReplayFile(bytes.NewReader(data))
	if err != nil || replayed == 0 || len(diffs) != 0 {
		t.Errorf("replayed %d records, %d diffs, err %v: want >0, 0, nil", replayed, len(diffs), err)
	}
}
