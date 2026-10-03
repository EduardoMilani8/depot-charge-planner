package sim

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

var allProfiles = []FaultProfile{ProfileNone, ProfileMild, ProfileSevere}

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
func TestPlannerNotWorseThanBaselines(t *testing.T) {
	for _, limit := range []float64{2000, 1200, 600} {
		for _, profile := range allProfiles {
			t.Run(fmt.Sprintf("%.0fkW/%s", limit, profile), func(t *testing.T) {
				t.Parallel()
				var pl, fifo, edf []Metrics
				for seed := int64(1); seed <= int64(seedCount(8, 2)); seed++ {
					p := DefaultGenParams()
					p.LimitKW = limit
					p.Profile = profile
					sc := Generate(p, seed)
					pl = append(pl, Run(sc, NewPlannerController(propertyConfig(), sc), nil))
					fifo = append(fifo, Run(sc, NewFIFO(), nil))
					edf = append(edf, Run(sc, NewEDF(), nil))
				}
				a, f, e := Aggregate(pl).ReadyPct, Aggregate(fifo).ReadyPct, Aggregate(edf).ReadyPct
				t.Logf("ready%%: planner %.1f, fifo %.1f, edf %.1f", a, f, e)
				if a < f || a < e {
					t.Errorf("planner ready%% %.1f is below fifo %.1f or edf %.1f", a, f, e)
				}
			})
		}
	}
}
