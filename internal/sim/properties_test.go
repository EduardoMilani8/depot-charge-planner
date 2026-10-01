package sim

import (
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

func seedCount(normal, short int) int {
	if testing.Short() {
		return short
	}
	return normal
}

func TestPlannerNeverViolatesLimit(t *testing.T) {
	for _, profile := range allProfiles {
		for seed := int64(1); seed <= int64(seedCount(50, 5)); seed++ {
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
		for seed := int64(1); seed <= int64(seedCount(15, 3)); seed++ {
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
// investigate with `go run ./cmd/simrun -profile <p>` before changing anything.
func TestPlannerNotWorseThanBaselines(t *testing.T) {
	for _, profile := range allProfiles {
		var pl, fifo, edf []Metrics
		for seed := int64(1); seed <= int64(seedCount(15, 3)); seed++ {
			p := DefaultGenParams()
			p.Profile = profile
			sc := Generate(p, seed)
			pl = append(pl, Run(sc, NewPlannerController(propertyConfig(), sc), nil))
			fifo = append(fifo, Run(sc, NewFIFO(), nil))
			edf = append(edf, Run(sc, NewEDF(), nil))
		}
		a, f, e := Aggregate(pl).ReadyPct, Aggregate(fifo).ReadyPct, Aggregate(edf).ReadyPct
		if a < f || a < e {
			t.Errorf("profile %s: planner ready%% %.1f is below fifo %.1f or edf %.1f", profile, a, f, e)
		}
	}
}
