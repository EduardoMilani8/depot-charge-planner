package sim

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// The cost-aware planner defers charging past the peak window only when every bus can
// have its own plug (planner/cost.go). These tests run it on nights where buses arrive
// inside the peak (scenario minutes 0..arrivalTo, the peak being 0..180 = 18:00-21:00
// at the default energy prices) with cost policy {DeferMarginMin: 45}.
//
// Strict property, checked on every seed of every run in the profiles none and mild
// (the energy cost only in none, see below):
//   - no plan violation;
//   - ready% not lower than without the policy;
//   - in the profile none only: energy cost not above the cost without it by more than
//     costTolerance (runCostCell checks the cost for ProfileNone; with mild data the cost
//     is measured and reported but not asserted here; the wide sweep asserts it for both). The tolerance is not zero: a deferral
//     that ends inside the peak can leave a little more energy in it than the plain plan
//     (tapering buses; up to about 0.14% was measured).
//
// In the profiles severe and random the strict per-seed property does NOT hold (SoC noise,
// consumption faults, failures after the peak hit buses that charged late), so those cells
// are held to pinned bounds instead (faultBounds), always on: total ready points lost over
// the seeds, the worst single seed and the number of worse seeds. COST_STRICT_FAULTS=1
// turns the bounds into the strict per-seed property (a diagnostic; it fails today),
// COST_REPORT=1 prints every cell and COST_SWEEP=1 runs the wide random sweep.
const costTolerance = 0.002

// costSeeds is how many seeds (1..n) the cost grids run. It does NOT follow PROPERTY_SEEDS:
// the fault bounds below were measured on seeds 1..8, and the strict property is claimed
// for the geometries listed, not for every seed of every random night. Under -short or the
// race detector it is 3 (the first three of the same seeds, so every bound still holds);
// only COST_SWEEP=1 runs more.
func costSeeds() int {
	if testing.Short() || raceEnabled {
		return 3
	}
	return 8
}

// thin keeps every second geometry under the race detector, to keep the package well
// inside the CI time limit; the full lists run without it.
func thin(geos []costGeo) []costGeo {
	if !raceEnabled {
		return geos
	}
	var out []costGeo
	for i := 0; i < len(geos); i += 2 {
		out = append(out, geos[i])
	}
	return out
}

// costGeo is a depot and a night: buses and chargers, charger ceiling in kW, site limit,
// and the minute the last bus can arrive.
type costGeo struct {
	buses, chargers int
	chargerKW       float64
	limit           float64
	arrivalTo       int
}

func (g costGeo) String() string {
	return fmt.Sprintf("%d buses/%d chargers/%.0f kW/%.0f kW limit/arrivals 0..%d", g.buses, g.chargers, g.chargerKW, g.limit, g.arrivalTo)
}

func (g costGeo) params(profile FaultProfile) GenParams {
	p := DefaultGenParams()
	p.NumBuses, p.NumChargers, p.ChargerMaxKW, p.LimitKW = g.buses, g.chargers, g.chargerKW, g.limit
	p.Profile = profile
	p.ArrivalFromMin, p.ArrivalToMin = 0, g.arrivalTo
	return p
}

// pluggedGeometries have a plug for every bus: the depots where the policy acts.
var pluggedGeometries = []costGeo{
	{30, 30, 150, 800, 180}, {30, 30, 150, 1500, 180}, {30, 30, 150, 2500, 180},
	{50, 50, 150, 1200, 180}, {50, 50, 150, 2000, 180},
	{60, 60, 50, 600, 180}, {60, 60, 50, 1500, 180},
	{5, 5, 150, 300, 180}, {5, 5, 150, 750, 180}, {5, 5, 150, 1500, 180},
	{20, 25, 150, 400, 180}, {20, 25, 150, 800, 180},
}

// scarceGeometries have fewer plugs than buses (the project's swap scenario): the policy
// must do nothing there. The first six are the ones a reviewer found losing buses when
// the policy tried to cope with a queue.
var scarceGeometries = []costGeo{
	{3, 2, 50, 100, 60}, {4, 3, 50, 150, 60}, {10, 7, 50, 350, 120}, {40, 30, 50, 1500, 180},
	{80, 72, 60, 3456, 180}, {12, 8, 50, 400, 120},
	{50, 25, 150, 600, 180}, {50, 25, 150, 1200, 180}, {50, 25, 150, 2000, 180},
	{50, 25, 60, 1200, 180}, {50, 15, 150, 600, 180},
}

// faultBounds pins the measured worst cell in the profiles severe and random (8 seeds) on
// the depots with a plug for every bus: 40 points lost in all and 20 on a single seed (5
// buses, 5 chargers: one bus of five), 7 seeds worse (50 buses, 50 chargers, 2000 kW
// severe). The depots with scarce plugs, where the earlier bounds were 60/32/6, no longer
// take part: the policy does nothing there.
var faultBounds = struct {
	sumLoss, worstLoss float64
	worse              int
}{sumLoss: 40, worstLoss: 20, worse: 7}

type costCell struct {
	seeds, worse            int
	sumLoss, worstLoss      float64 // ready points lost on the worse seeds
	worstCostExcess         float64 // largest relative energy-cost excess over the baseline
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

// runCostCell compares the planner with and without the policy over seeds 1..seeds.
// With t == nil nothing is reported (the sweep checks the cell itself).
func runCostCell(t *testing.T, base planner.Config, g costGeo, profile FaultProfile, seeds int, blind, strict bool) costCell {
	if t != nil {
		t.Helper()
	}
	var cell costCell
	cell.seeds = seeds
	cost := base
	cost.Cost = &planner.CostPolicy{DeferMarginMin: 45}
	for seed := int64(1); seed <= int64(seeds); seed++ {
		sc := Generate(g.params(profile), seed)
		without := runWith(sc, base, blind)
		with := runWith(sc, cost, blind)
		if with.PlanViolations != 0 && t != nil {
			t.Errorf("seed %d: %d plan violations", seed, with.PlanViolations)
		}
		if loss := without.ReadyPct - with.ReadyPct; loss > 0 {
			cell.worse++
			cell.sumLoss += loss
			cell.worstLoss = math.Max(cell.worstLoss, loss)
			if strict && t != nil {
				t.Errorf("seed %d: ready%% %.1f with the policy < %.1f without", seed, with.ReadyPct, without.ReadyPct)
			}
		}
		if without.CostBRL > 0 {
			excess := with.CostBRL/without.CostBRL - 1
			cell.worstCostExcess = math.Max(cell.worstCostExcess, excess)
			if strict && profile == ProfileNone && excess > costTolerance && t != nil {
				t.Errorf("seed %d: energy cost R$ %.2f with the policy > R$ %.2f without by more than %.1f%%", seed, with.CostBRL, without.CostBRL, 100*costTolerance)
			}
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
	return fmt.Sprintf("ready %.1f%% with, %.1f%% without (%d of %d seeds worse, %.0f points lost in all, worst %.0f); energy R$ %.0f with, %.0f without (%.1f%% less, worst seed %+.2f%%)",
		c.withReady/n, c.withoutReady/n, c.worse, c.seeds, c.sumLoss, c.worstLoss, c.withCost/n, c.withoutCost/n, 100*(1-c.withCost/c.withoutCost), 100*c.worstCostExcess)
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

// costGrid runs every geometry for the given profiles, announced and (when blindToo)
// blind as well.
func costGrid(t *testing.T, geos []costGeo, profiles []FaultProfile, blindToo bool) {
	costGridCells(t, geos, profiles, blindToo, nil)
}

// costGridCells is costGrid that also hands every cell, once it has run, to onCell.
func costGridCells(t *testing.T, geos []costGeo, profiles []FaultProfile, blindToo bool, onCell func(t *testing.T, g costGeo, profile FaultProfile, c costCell)) {
	seeds := costSeeds()
	report := os.Getenv("COST_REPORT") == "1"
	strictFaults := os.Getenv("COST_STRICT_FAULTS") == "1"
	modes := []bool{false}
	if blindToo {
		modes = append(modes, true)
	}
	for _, g := range thin(geos) {
		for _, profile := range profiles {
			for _, blind := range modes {
				g, profile, blind := g, profile, blind
				mode := "announced"
				if blind {
					mode = "blind"
				}
				t.Run(fmt.Sprintf("%s/%s/%s", g, profile, mode), func(t *testing.T) {
					faulty := profile == ProfileSevere || profile == ProfileRandom
					c := runCostCell(t, propertyConfig(), g, profile, seeds, blind, !faulty || strictFaults)
					if report || testing.Verbose() {
						t.Log(c)
					}
					if faulty && !strictFaults {
						checkFaultCell(t, c)
					}
					if onCell != nil {
						onCell(t, g, profile, c)
					}
				})
			}
		}
	}
}

// Strict property on the depots with a plug for every bus (schedule announced).
func TestCostAwareNotLessReadyAndNotDearerWithPlugsForAllBuses(t *testing.T) {
	costGridCells(t, pluggedGeometries, []FaultProfile{ProfileNone, ProfileMild}, false, func(t *testing.T, g costGeo, profile FaultProfile, c costCell) {
		// An inert policy would pass the strict property trivially, so the cells where
		// it is known to act must show it: 30 buses/30 chargers/150 kW at 1500 and 2500 kW
		// saved 53.8% and 60.2% of the energy cost over seeds 1..8 (none), 50/50 at 2000 kW
		// 39.2%; at least a quarter of the cost must go.
		if profile != ProfileNone || g.chargerKW != 150 || g.buses < 30 || g.limit < 1500 {
			return // the others save little (60 buses of 50 kW at 1500 kW: 6.4%) or nothing (power-bound)
		}
		if c.withCost > 0.75*c.withoutCost {
			t.Errorf("the policy saved only %.1f%% of the energy cost, want at least 25%%: it looks inert", 100*(1-c.withCost/c.withoutCost))
		}
	})
}

// Strict property where plugs are scarce (the policy must do nothing, so ready% and
// energy are those of the plain planner), including the geometries a reviewer found
// losing buses with an earlier version.
func TestCostAwareNotLessReadyAndNotDearerWithScarcePlugs(t *testing.T) {
	costGrid(t, scarceGeometries, []FaultProfile{ProfileNone, ProfileMild}, false)
}

// With scarce plugs the plan is exactly the plain one: same metrics, same plan changes.
func TestCostAwareDoesNothingWithScarcePlugs(t *testing.T) {
	for _, g := range thin(scarceGeometries[:6]) {
		for seed := int64(1); seed <= 2; seed++ {
			sc := Generate(g.params(ProfileNone), seed)
			base := propertyConfig()
			cost := base
			cost.Cost = &planner.CostPolicy{DeferMarginMin: 45}
			for _, blind := range []bool{false, true} {
				a, b := runWith(sc, base, blind), runWith(sc, cost, blind)
				a.PlanP99Micros, b.PlanP99Micros = 0, 0
				if fmt.Sprint(a) != fmt.Sprint(b) {
					t.Errorf("%s seed %d blind=%v: metrics differ\n%+v\n%+v", g, seed, blind, a, b)
				}
			}
		}
	}
}

// Severe and random faults, on depots with a plug for every bus: pinned bounds.
func TestCostAwareUnderFaults(t *testing.T) {
	costGrid(t, pluggedGeometries, []FaultProfile{ProfileSevere}, false)
	costGrid(t, []costGeo{{50, 50, 150, 2000, 180}, {30, 30, 150, 2500, 180}, {60, 60, 50, 1500, 180}}, []FaultProfile{ProfileRandom}, false)
}

// The set of deferred buses must not flap: plan changes with the policy stay within 1.5
// times the plain planner's plus three per bus (deferring a bus and starting it again
// after the peak cost about two changes each, the rest is re-sharing the post-peak
// budget). Measured over seeds 1..16: 30 buses/30 chargers/2500 kW 98..151 against 51..89
// without (worst case 66 above 1.5 times the plain count), 50/50 at 2000 kW 319..394
// against 287..319, 60/60 at 1500 kW 420..488 against 439..485.
func TestCostAwareDoesNotFlap(t *testing.T) {
	for _, g := range thin([]costGeo{{10, 12, 50, 600, 120}, {10, 10, 150, 1500, 120}, {30, 30, 150, 2500, 180}, {50, 50, 150, 2000, 180}, {60, 60, 50, 1500, 180}}) {
		for seed := int64(1); seed <= int64(costSeeds()/2+1); seed++ {
			sc := Generate(g.params(ProfileNone), seed)
			base := propertyConfig()
			cost := base
			cost.Cost = &planner.CostPolicy{DeferMarginMin: 45}
			a, b := runWith(sc, base, false), runWith(sc, cost, false)
			if limit := 1.5*float64(a.PlanChanges) + 3*float64(g.buses); float64(b.PlanChanges) > limit {
				t.Errorf("%s seed %d: %d plan changes with the policy, %d without (limit %.0f)", g, seed, b.PlanChanges, a.PlanChanges, limit)
			}
		}
	}
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

// Two buses, one plug: the case where deferring A makes W miss its departure (1 of 2
// ready with a policy that ignored plugs). With scarce plugs the policy does nothing, so
// 2 of 2 are ready at the same cost as without it.
func TestCostMicroTwoBusesOnePlug(t *testing.T) {
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
			if with.CostBRL != without.CostBRL {
				t.Errorf("W leaves at %d (%s): R$ %.2f with the policy != R$ %.2f without", dep, name, with.CostBRL, without.CostBRL)
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

// The wide random sweep behind COST_SWEEP=1 (COST_SWEEP_GEOS and COST_SWEEP_SEEDS
// override 150 geometries and 14 seeds): random depots (3..100 buses, 2..100 chargers
// of 50..150 kW, a site limit from 10% to 120% of the plug capacity, so binding or not,
// arrivals ending at minute 60, 120, 180 or 240), announced or blind, profiles none and
// mild. The ready% with the policy must never be lower than without it and the energy
// cost never more than costTolerance higher. Prints a summary and the worst cases.
func TestCostAwareWideSweep(t *testing.T) {
	if os.Getenv("COST_SWEEP") != "1" {
		t.Skip("set COST_SWEEP=1 to run the wide random sweep")
	}
	nGeo, nSeeds := 150, 14
	fmt.Sscan(os.Getenv("COST_SWEEP_GEOS"), &nGeo)
	fmt.Sscan(os.Getenv("COST_SWEEP_SEEDS"), &nSeeds)
	rng := rand.New(rand.NewSource(20261011))
	type job struct {
		g       costGeo
		blind   bool
		profile FaultProfile
	}
	var jobs []job
	for i := 0; i < nGeo; i++ {
		g := costGeo{
			buses: 3 + rng.Intn(98), chargers: 2 + rng.Intn(99), chargerKW: float64(50 + rng.Intn(101)),
			arrivalTo: []int{60, 120, 180, 240}[rng.Intn(4)],
		}
		g.limit = math.Round(float64(g.chargers) * g.chargerKW * (0.1 + 1.1*rng.Float64()))
		blind := rng.Intn(2) == 0
		jobs = append(jobs, job{g, blind, ProfileNone}, job{g, blind, ProfileMild})
	}
	type result struct {
		job
		cell costCell
	}
	results := make([]result, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, j job) {
			defer wg.Done()
			defer func() { <-sem }()
			cfg := planner.DefaultConfig() // the default timeout: a loaded machine must not trip the fallback
			results[i] = result{j, runCostCell(nil, cfg, j.g, j.profile, nSeeds, j.blind, false)}
		}(i, j)
	}
	wg.Wait()
	acting, readyWorse, costOver := 0, 0, 0
	worstExcess, saved, base := 0.0, 0.0, 0.0
	var lines []string
	for _, r := range results {
		c := r.cell
		if c.withCost < c.withoutCost-1e-6 {
			acting++
		}
		saved += c.withoutCost - c.withCost
		base += c.withoutCost
		worstExcess = math.Max(worstExcess, c.worstCostExcess)
		if c.worse > 0 {
			readyWorse++
			lines = append(lines, fmt.Sprintf("READY WORSE: %s blind=%v %s: %s", r.g, r.blind, r.profile, c))
		}
		if c.worstCostExcess > costTolerance {
			costOver++
			lines = append(lines, fmt.Sprintf("COST OVER: %s blind=%v %s: %s", r.g, r.blind, r.profile, c))
		}
	}
	sort.Strings(lines)
	t.Logf("%d cells (%d geometries x none/mild x %d seeds): policy acted in %d, ready worse in %d, cost over %.1f%% in %d; worst cost excess %+.3f%%; mean saving over all cells %.1f%%\n%s",
		len(results), nGeo, nSeeds, acting, readyWorse, 100*costTolerance, costOver, 100*worstExcess, 100*saved/base, strings.Join(lines, "\n"))
	if readyWorse > 0 || costOver > 0 {
		t.Errorf("%d cells with lower ready%%, %d with cost over the tolerance", readyWorse, costOver)
	}
}
