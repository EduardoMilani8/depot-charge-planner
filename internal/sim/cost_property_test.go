package sim

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// The cost-aware planner on nights where every bus arrives inside the peak window
// (scenario minutes 0..180 = 18:00-21:00 at the default energy prices), cost policy
// {DeferMarginMin: 45}, the schedule announced (ForController does it), seeds 1..8
// (1..3 with -short).
//
// Strict property, checked on every seed of every run in the profiles none and mild:
// no plan violation, ready% not lower than without the policy and, in the profile
// none, energy cost not above the cost without it. It is checked on the geometries in
// costGeometries below (not on "any depot"), at the limits listed there.
//
// In the profiles severe and random the strict per-seed property does NOT hold (SoC
// noise, consumption faults and failures after the peak hit buses that charged late), so
// those cells are held to pinned bounds instead (faultBounds), always on: total ready
// points lost over the seeds, the worst single seed and the number of worse seeds. The
// bounds are the measured worst values rounded up; COST_REPORT=1 prints every cell, and
// COST_STRICT_FAULTS=1 turns the bounds into the strict per-seed property (a diagnostic;
// it fails today).

type costGeometry struct {
	name   string
	set    func(*GenParams)
	limits []float64
}

// The geometries the strict property is checked on. The first is the simulator default.
var costGeometries = []costGeometry{
	{"50 buses, 25 chargers of 150 kW", func(p *GenParams) {}, []float64{600, 1200, 2000}},
	{"5 buses, 5 chargers", func(p *GenParams) { p.NumBuses, p.NumChargers = 5, 5 }, []float64{300, 750, 1500}},
	{"50 buses, 25 chargers of 60 kW", func(p *GenParams) { p.ChargerMaxKW = 60 }, []float64{1200, 1500}},
	{"50 buses, 15 chargers", func(p *GenParams) { p.NumChargers = 15 }, []float64{600, 1000}},
	{"20 buses, 25 chargers", func(p *GenParams) { p.NumBuses = 20 }, []float64{400, 800}},
	{"50 buses, 50 chargers", func(p *GenParams) { p.NumChargers = 50 }, []float64{1200, 2000}},
}

// faultBounds pins the measured worst cell in the profiles severe and random (8 seeds):
// 60 points lost in all (5 buses, 5 chargers, 1500 kW severe: three seeds lose a bus of
// five), a single seed losing 32 points (50 buses, 2000 kW random, seed 7: every sensor
// freezes at minute 154), 6 seeds worse (50 buses, 50 chargers, 2000 kW severe).
var faultBounds = struct {
	sumLoss, worstLoss float64
	worse              int
}{sumLoss: 60, worstLoss: 32, worse: 6}

type costCell struct {
	seeds, worse            int
	sumLoss, worstLoss      float64 // ready points lost on the worse seeds
	withReady, withoutReady float64 // sums over the seeds
	withCost, withoutCost   float64
}

// runWith runs the planner on the scenario: through ForController (the schedule is
// announced when a cost policy is set) or, when blind, directly on the given scenario.
func runWith(sc Scenario, cfg planner.Config, blind bool) Metrics {
	if blind {
		return Run(sc, NewPlannerController(cfg, sc), nil)
	}
	sc, ctrl, _ := ForController("planner", cfg, sc)
	return Run(sc, ctrl, nil)
}

func runCostCell(t *testing.T, geo costGeometry, limit float64, profile FaultProfile, seeds int, blind, strict bool) costCell {
	t.Helper()
	var cell costCell
	cell.seeds = seeds
	for seed := int64(1); seed <= int64(seeds); seed++ {
		p := DefaultGenParams()
		geo.set(&p)
		p.Profile, p.LimitKW = profile, limit
		p.ArrivalFromMin, p.ArrivalToMin = 0, 180
		sc := Generate(p, seed)
		base := propertyConfig()
		cost := base
		cost.Cost = &planner.CostPolicy{DeferMarginMin: 45}
		without := runWith(sc, base, blind)
		with := runWith(sc, cost, blind)
		if with.PlanViolations != 0 {
			t.Errorf("seed %d: %d plan violations", seed, with.PlanViolations)
		}
		if loss := without.ReadyPct - with.ReadyPct; loss > 0 {
			cell.worse++
			cell.sumLoss += loss
			cell.worstLoss = math.Max(cell.worstLoss, loss)
			if strict {
				t.Errorf("seed %d: ready%% %.1f with the policy < %.1f without", seed, with.ReadyPct, without.ReadyPct)
			}
		}
		if profile == ProfileNone && with.CostBRL > without.CostBRL*(1+1e-9) {
			t.Errorf("seed %d: energy cost R$ %.2f with the policy > R$ %.2f without", seed, with.CostBRL, without.CostBRL)
		}
		cell.withReady += with.ReadyPct
		cell.withoutReady += without.ReadyPct
		cell.withCost += with.CostBRL
		cell.withoutCost += without.CostBRL
	}
	return cell
}

func (c costCell) String() string {
	n := float64(c.seeds)
	return fmt.Sprintf("ready %.1f%% with, %.1f%% without (%d of %d seeds worse, %.0f points lost in all, worst %.0f); energy R$ %.0f with, %.0f without (%.1f%% less)",
		c.withReady/n, c.withoutReady/n, c.worse, c.seeds, c.sumLoss, c.worstLoss, c.withCost/n, c.withoutCost/n, 100*(1-c.withCost/c.withoutCost))
}

// checkFaultCell holds a severe/random cell to faultBounds.
func checkFaultCell(t *testing.T, c costCell) {
	t.Helper()
	if c.sumLoss > faultBounds.sumLoss {
		t.Errorf("%.0f ready points lost over the seeds, bound %.0f", c.sumLoss, faultBounds.sumLoss)
	}
	if c.worstLoss > faultBounds.worstLoss {
		t.Errorf("worst seed lost %.0f points, bound %.0f", c.worstLoss, faultBounds.worstLoss)
	}
	if c.worse > faultBounds.worse {
		t.Errorf("%d seeds worse, bound %d", c.worse, faultBounds.worse)
	}
}

// costGrid runs every geometry at its limits for the given profiles.
func costGrid(t *testing.T, geos []costGeometry, profiles []FaultProfile, blind bool) {
	seeds := seedCount(8, 3)
	report := os.Getenv("COST_REPORT") == "1"
	strictFaults := os.Getenv("COST_STRICT_FAULTS") == "1"
	for _, geo := range geos {
		for _, limit := range geo.limits {
			for _, profile := range profiles {
				geo, limit, profile := geo, limit, profile
				t.Run(fmt.Sprintf("%s/%.0fkW/%s", geo.name, limit, profile), func(t *testing.T) {
					faulty := profile == ProfileSevere || profile == ProfileRandom
					c := runCostCell(t, geo, limit, profile, seeds, blind, !faulty || strictFaults)
					if report || testing.Verbose() {
						t.Log(c)
					}
					if faulty && !strictFaults {
						checkFaultCell(t, c)
					}
				})
			}
		}
	}
}

// Strict property on all the geometries, profiles none and mild.
func TestCostAwareNotLessReadyAndNotDearer(t *testing.T) {
	costGrid(t, costGeometries, []FaultProfile{ProfileNone, ProfileMild}, false)
}

// The four profiles on the default depot (50 buses, 25 chargers of 150 kW) and severe on
// the others: strict where the data is clean, pinned bounds where it is not.
func TestCostAwareUnderFaults(t *testing.T) {
	costGrid(t, costGeometries[:1], []FaultProfile{ProfileSevere, ProfileRandom}, false)
	costGrid(t, costGeometries[1:], []FaultProfile{ProfileSevere}, false)
}

// Without the schedule (blind: Input has no future buses, policy.ScheduleKnown false) the
// planner keeps a bigger reserve and guesses the queue from the free chargers. Strict on
// the default depot, where buses and chargers are balanced; with more buses than chargers
// and slow chargers it can lose buses (no property is claimed there, see cost.go).
func TestCostAwareBlindOnTheDefaultDepot(t *testing.T) {
	costGrid(t, costGeometries[:1], []FaultProfile{ProfileNone, ProfileMild}, true)
}

// plugTurnoverScenario is one 100 kW charger shared by bus A (leaves late, plenty of
// slack) and bus W (waiting for the plug, leaves at wDeparture). Both need 110 kWh
// (100 kWh plus the planner's 10 kWh margin); the peak is minutes 0..180. Charging A
// first takes 66 minutes and then W, so W is ready around minute 137.
func plugTurnoverScenario(wDeparture int) Scenario {
	bus := func(id string, dep int) BusSpec {
		return BusSpec{Bus: model.Bus{ID: id, CapacityKWh: 300, SoCKWh: 100, SoCConfidence: 1, TargetKWh: 200,
			ArrivalMin: 0, DepartureMin: dep, MaxBatteryKW: 150}}
	}
	return Scenario{
		Name: "plug-turnover", Seed: 1, StartClockMin: 1080, Horizon: 700, BaseLimitKW: 1000,
		Chargers:    []model.Charger{{ID: "C1", MaxKW: 100, MinKW: 5, Efficiency: 1, Status: model.ChargerOK}},
		Buses:       []BusSpec{bus("A", 600), bus("W", wDeparture)},
		Tariff:      Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9},
		FollowSwaps: true,
	}
}

// A deferred bus keeps its plug without drawing power, so a bus waiting for that plug
// would start only after the peak. Without protecting the queue this lost a bus (1 of 2
// ready); with it the plan is the baseline's, 2 of 2, at the same cost.
func TestCostPlugTurnoverKeepsTheWaitingBusReady(t *testing.T) {
	for _, dep := range []int{150, 170, 200} {
		sc := plugTurnoverScenario(dep)
		base := propertyConfig()
		cost := base
		cost.Cost = &planner.CostPolicy{DeferMarginMin: 45}
		for name, blind := range map[string]bool{"announced": false, "blind": true} {
			without := runWith(sc, base, blind)
			with := runWith(sc, cost, blind)
			if without.Ready != 2 || with.Ready != 2 || with.PlanViolations != 0 {
				t.Errorf("W leaves at %d (%s): ready %d with the policy, %d without; want 2 and 2", dep, name, with.Ready, without.Ready)
			}
			if with.CostBRL > without.CostBRL*(1+1e-9) {
				t.Errorf("W leaves at %d (%s): R$ %.2f with the policy > R$ %.2f without", dep, name, with.CostBRL, without.CostBRL)
			}
		}
	}
}

// Announcing the schedule changes nothing for a planner without a cost policy, nor for
// the baselines.
func TestAnnouncedArrivalsDoNotChangeOtherControllers(t *testing.T) {
	for _, profile := range []FaultProfile{ProfileNone, ProfileSevere} {
		p := DefaultGenParams()
		p.Profile, p.LimitKW = profile, 1200
		p.ArrivalFromMin, p.ArrivalToMin = 0, 180
		for seed := int64(1); seed <= 2; seed++ {
			sc := Generate(p, seed)
			announced := sc
			announced.AnnounceArrivals = true
			for _, name := range ControllerNames {
				_, c1, _ := ForController(name, propertyConfig(), sc)
				_, c2, _ := ForController(name, propertyConfig(), announced)
				a, b := Run(sc, c1, nil), Run(announced, c2, nil)
				a.PlanP99Micros, b.PlanP99Micros = 0, 0
				if fmt.Sprint(a) != fmt.Sprint(b) {
					t.Errorf("%s %s seed %d: metrics differ with announced arrivals\n%+v\n%+v", name, profile, seed, a, b)
				}
			}
		}
	}
}
