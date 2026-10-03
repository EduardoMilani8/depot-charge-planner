package sim

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

var allProfiles = []FaultProfile{ProfileNone, ProfileMild, ProfileSevere, ProfileRandom}

func propertyConfig() planner.Config {
	c := planner.DefaultConfig()
	c.Timeout = 50 * time.Millisecond
	return c
}

// seedCount returns how many seeds a property test should run. With -short it returns short.
// Otherwise it returns normal, unless the environment variable PROPERTY_SEEDS is a positive
// integer, which overrides normal for every property test, e.g. for a long run:
//
//	PROPERTY_SEEDS=100 go test ./internal/sim
func seedCount(normal, short int) int {
	if testing.Short() {
		return short
	}
	if n, err := strconv.Atoi(os.Getenv("PROPERTY_SEEDS")); err == nil && n > 0 {
		return n
	}
	return normal
}

func TestPlannerNeverViolatesLimit(t *testing.T) {
	for _, profile := range allProfiles {
		for seed := int64(1); seed <= int64(seedCount(20, 3)); seed++ {
			p := DefaultGenParams()
			p.Profile = profile
			sc := Generate(p, seed)
			m := Run(sc, NewPlannerController(propertyConfig(), sc), nil)
			if m.PlanViolations != 0 {
				t.Fatalf("profile %s seed %d: %d plan violations (reproduce with Generate and this seed)", profile, seed, m.PlanViolations)
			}
		}
	}
}

func TestBaselinesNeverViolateLimit(t *testing.T) {
	for _, profile := range allProfiles {
		for seed := int64(1); seed <= int64(seedCount(8, 2)); seed++ {
			p := DefaultGenParams()
			p.Profile = profile
			sc := Generate(p, seed)
			for name, c := range map[string]Controller{"fifo": NewFIFO(), "edf": NewEDF(), "safe": NewSafeOnly()} {
				if m := Run(sc, c, nil); m.PlanViolations != 0 {
					t.Fatalf("%s profile %s seed %d: %d violations", name, profile, seed, m.PlanViolations)
				}
			}
		}
	}
}

// If this fails it is a FINDING about the algorithm, not a reason to loosen the test:
// investigate with `go run ./cmd/simrun -profile <p> -limit <kW>` before changing anything.
//
// Spec §12: the planner's mean ready% must be >= every baseline's under the SAME site
// limit, for every fault profile. Checked at a loose (2000 kW), a medium (1200 kW) and
// a tight (600 kW) limit, where before the budget-aware swaps the planner lost to FIFO.
// Besides the spec's FIFO and EDF it is also compared with fifo-unplug (FIFO plus
// operators unplugging charged buses), since the planner's own runs assume operators
// follow its swaps.
func TestPlannerNotWorseThanBaselines(t *testing.T) {
	for _, limit := range []float64{2000, 1200, 600} {
		for _, profile := range allProfiles {
			t.Run(fmt.Sprintf("%.0fkW/%s", limit, profile), func(t *testing.T) {
				t.Parallel()
				var pl, fifo, edf, unplug []Metrics
				for seed := int64(1); seed <= int64(seedCount(8, 2)); seed++ {
					p := DefaultGenParams()
					p.LimitKW = limit
					p.Profile = profile
					sc := Generate(p, seed)
					pl = append(pl, Run(sc, NewPlannerController(propertyConfig(), sc), nil))
					fifo = append(fifo, Run(sc, NewFIFO(), nil))
					edf = append(edf, Run(sc, NewEDF(), nil))
					manual := sc
					manual.UnplugFull = true
					unplug = append(unplug, Run(manual, NewFIFO(), nil))
				}
				a, f, e, u := Aggregate(pl).ReadyPct, Aggregate(fifo).ReadyPct, Aggregate(edf).ReadyPct, Aggregate(unplug).ReadyPct
				t.Logf("ready%%: planner %.1f, fifo %.1f, edf %.1f, fifo-unplug %.1f", a, f, e, u)
				if a < f || a < e || a < u {
					t.Errorf("planner ready%% %.1f is below fifo %.1f, edf %.1f or fifo-unplug %.1f", a, f, e, u)
				}
			})
		}
	}
}

// checkingController asserts, every cycle, that the planner's plan satisfies the
// invariants for the observation it was given (not only the commanded total).
type checkingController struct {
	t     *testing.T
	inner Controller
	label string
}

func (c checkingController) Plan(in planner.Input) planner.Plan {
	p := c.inner.Plan(in)
	if v := planner.Violations(in, p); len(v) != 0 {
		c.t.Fatalf("%s minute %d: invalid plan (layer %v): %v", c.label, in.Now, p.Layer, v)
	}
	return p
}

// Spec §7 fuzz mode: random combinations and intensities of every fault kind. The
// planner must return a valid plan on every cycle and never command above the limit.
func TestPlannerRandomFaultsKeepInvariants(t *testing.T) {
	for seed := int64(1); seed <= int64(seedCount(20, 3)); seed++ {
		p := DefaultGenParams()
		p.Profile = ProfileRandom
		sc := Generate(p, seed)
		label := fmt.Sprintf("random seed %d", seed)
		m := Run(sc, checkingController{t: t, inner: NewPlannerController(propertyConfig(), sc), label: label}, nil)
		if m.PlanViolations != 0 {
			t.Fatalf("%s: %d plan violations (reproduce with Generate and this seed)", label, m.PlanViolations)
		}
		if m.LayerTicks["normal"] == 0 {
			t.Errorf("%s: the normal layer never ran: %v", label, m.LayerTicks)
		}
	}
}
