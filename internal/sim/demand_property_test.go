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

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// The demand cap (planner.CostPolicy.DemandTargetKW, planner/cost.go) lowers the budget of
// the normal layer to the target while the buses on charge, the ones waiting for a plug
// and (with the schedule) the ones still to arrive can all finish under it. These tests
// compare, seed by seed, the planner with the cap against the same cost policy without a
// target (the deferral, when it acts, is in both), with demand prices set so that
// Metrics.Bill exists:
//   - no plan violation;
//   - ready% not lower;
//   - Bill.DemandKW not higher (1e-9) with the schedule announced. Without the schedule
//     the planner can only release the cap late, when the fleet turns out larger than it
//     saw, and the catch-up at the limit can land the largest 15-minute block a few kW
//     above the uncapped one when the night is limit-bound (measured up to 0.33% of the
//     limit on capGeometries): there the tolerance is demandTie of the limit.
//
// Strict means these three on every seed. They are asserted with the schedule announced
// (what sim.ForController does for a planner with a cost policy) on capGeometries, and
// without it (NewPlannerController on an unannounced scenario) on the same geometries.
// Depots with many more buses than chargers and no schedule are held to pinned bounds
// (capPoorBounds), as are the profiles severe and random (capFaultBounds). COST_CAP_SWEEP=1
// runs the wide random sweep and DEMAND_REPORT=1 prints the cells.
const demandTie = 0.005

// capGeo is a depot, a night and a demand target.
type capGeo struct {
	costGeo
	target float64
}

func (g capGeo) String() string {
	arr := "default arrivals"
	if g.arrivalTo > 0 {
		arr = fmt.Sprintf("arrivals 0..%d", g.arrivalTo)
	}
	return fmt.Sprintf("%d buses/%d chargers/%.0f kW/%.0f kW limit/%s/target %.0f", g.buses, g.chargers, g.chargerKW, g.limit, arr, g.target)
}

// capParams is the generator input with demand prices (illustrative R$/kW) so that
// Metrics.Bill exists.
func (g capGeo) params(profile FaultProfile) GenParams {
	p := g.costGeo.params(profile)
	p.Demand = DemandParams{PeakPrice: 40, OffPeakPrice: 20}
	if g.arrivalTo == 0 {
		p.ArrivalFromMin, p.ArrivalToMin = 0, 0 // the generator's default window, 20:00-24:00
	}
	return p
}

type capCell struct {
	seeds, worse, violations, demandWorse     int
	sumLoss, worstLoss                        float64
	withReady, withoutReady, plainReady       float64
	withDemand, withoutDemand                 float64
	plainDemand                               float64
	worstDemandExcess                         float64 // largest with - without, kW
	withCost, withoutCost, plainCost          float64
	withMonthly, withoutMonthly, plainMonthly float64
}

func (c capCell) String() string {
	n := float64(c.seeds)
	return fmt.Sprintf("ready %.1f%% with, %.1f%% without the target (plain %.1f%%), %d of %d seeds worse, %.0f points lost in all, worst %.1f; demand %.0f kW with, %.0f without (%.1f%% less; plain %.0f), %d seeds higher, worst %+.2f kW; energy R$ %.0f with, %.0f without (%+.2f%%; plain %.0f); monthly bill R$ %.0f with, %.0f without (plain %.0f)",
		c.withReady/n, c.withoutReady/n, c.plainReady/n, c.worse, c.seeds, c.sumLoss, c.worstLoss,
		c.withDemand/n, c.withoutDemand/n, 100*(1-c.withDemand/c.withoutDemand), c.plainDemand/n, c.demandWorse, c.worstDemandExcess,
		c.withCost/n, c.withoutCost/n, 100*(c.withCost/c.withoutCost-1), c.plainCost/n,
		c.withMonthly/n, c.withoutMonthly/n, c.plainMonthly/n)
}

// runCapCell compares the planner with the cap against the same cost policy without a
// target (so the deferral, when it acts, is in both) over seeds 1..seeds; the plain
// planner is measured too, for the report. announced hands the schedule to the planner
// (ForController); otherwise it is unannounced and deferral is off.
func runCapCell(t *testing.T, g capGeo, profile FaultProfile, seeds int, announced bool, strict bool) capCell {
	return runCapCellCfg(t, propertyConfig(), g, profile, seeds, announced, strict, false)
}

// runCapCellCfg is runCapCell with the planner configuration given; withPlain also runs
// the plain planner (no cost policy) for the report columns.
func runCapCellCfg(t *testing.T, base planner.Config, g capGeo, profile FaultProfile, seeds int, announced, strict, withPlain bool) capCell {
	if t != nil {
		t.Helper()
	}
	var cell capCell
	cell.seeds = seeds
	noTarget := base
	noTarget.Cost = &planner.CostPolicy{DeferMarginMin: 45}
	capped := base
	capped.Cost = &planner.CostPolicy{DeferMarginMin: 45, DemandTargetKW: g.target}
	for seed := int64(1); seed <= int64(seeds); seed++ {
		sc := Generate(g.params(profile), seed)
		var plain Metrics
		if withPlain {
			plain = runWith(sc, base, false)
		}
		without := runWith(sc, noTarget, !announced)
		with := runWith(sc, capped, !announced)
		if with.PlanViolations != 0 {
			cell.violations++
			if t != nil {
				t.Errorf("seed %d: %d plan violations", seed, with.PlanViolations)
			}
		}
		if loss := without.ReadyPct - with.ReadyPct; loss > 0 {
			cell.worse++
			cell.sumLoss += loss
			cell.worstLoss = math.Max(cell.worstLoss, loss)
			if strict && t != nil {
				t.Errorf("seed %d: ready%% %.1f with the cap < %.1f without", seed, with.ReadyPct, without.ReadyPct)
			}
		}
		if with.Bill == nil || without.Bill == nil || (withPlain && plain.Bill == nil) {
			if t != nil {
				t.Fatalf("seed %d: no Bill (demand price not set?)", seed)
			}
			continue
		}
		if ex := with.Bill.DemandKW - without.Bill.DemandKW; ex > 1e-9 {
			cell.demandWorse++
			cell.worstDemandExcess = math.Max(cell.worstDemandExcess, ex)
			tol := 1e-9
			if !announced {
				tol = demandTie * g.limit
			}
			if ex > tol && strict && t != nil {
				t.Errorf("seed %d: demand %.2f kW with the cap > %.2f without (tolerance %g kW)", seed, with.Bill.DemandKW, without.Bill.DemandKW, tol)
			}
		}
		cell.withReady += with.ReadyPct
		cell.withoutReady += without.ReadyPct
		cell.withDemand += with.Bill.DemandKW
		cell.withoutDemand += without.Bill.DemandKW
		cell.withCost += with.CostBRL
		cell.withoutCost += without.CostBRL
		cell.withMonthly += with.Bill.MonthlyBRL
		cell.withoutMonthly += without.Bill.MonthlyBRL
		if withPlain {
			cell.plainMonthly += plain.Bill.MonthlyBRL
			cell.plainReady += plain.ReadyPct
			cell.plainDemand += plain.Bill.DemandKW
			cell.plainCost += plain.CostBRL
		}
	}
	return cell
}

// capGeometries are the depots of the strict cap property. The first group is the
// project's default swap scenario (50 buses, 25 chargers, so a plug for half the buses)
// at three site limits, targets at 50% and 80% of the limit and both arrival windows (the
// default 20:00-24:00 one and buses arriving inside the peak); the brief's own case
// (limit 2000, targets 500 and 800) is part of it. The second group has a plug for every
// bus, where the cap composes with the deferral.
func capGeometries() []capGeo {
	var geos []capGeo
	for _, limit := range []float64{600, 1200, 2000} {
		for _, f := range []float64{0.5, 0.8} {
			for _, arr := range []int{0, 180} {
				geos = append(geos, capGeo{costGeo{50, 25, 150, limit, arr}, math.Round(limit * f)})
			}
		}
	}
	geos = append(geos, capGeo{costGeo{50, 25, 150, 2000, 0}, 500}, capGeo{costGeo{50, 25, 150, 2000, 0}, 800})
	for _, f := range []float64{0.5, 0.8} {
		geos = append(geos, capGeo{costGeo{30, 30, 150, 1500, 180}, 1500 * f}, capGeo{costGeo{50, 50, 150, 2000, 180}, 2000 * f})
	}
	return geos
}

func thinCap(geos []capGeo) []capGeo {
	if !raceEnabled {
		return geos
	}
	var out []capGeo
	for i := 0; i < len(geos); i += 3 {
		out = append(out, geos[i])
	}
	return out
}

// capPoorGeometries have several buses per charger and a target close to what the
// fleet needs; without the schedule the cap cannot know the queue that is still to come.
func capPoorGeometries() []capGeo {
	return []capGeo{
		{costGeo{27, 4, 139, 495, 180}, 256}, {costGeo{33, 8, 102, 758, 0}, 405},
		{costGeo{44, 15, 116, 563, 180}, 453}, {costGeo{56, 9, 88, 618, 240}, 520},
		{costGeo{71, 22, 80, 1551, 180}, 1108}, {costGeo{50, 15, 150, 600, 180}, 480},
	}
}

// capFaultGeometries are run under severe and random faults.
func capFaultGeometries() []capGeo {
	return []capGeo{
		{costGeo{50, 25, 150, 1200, 0}, 960}, {costGeo{50, 25, 150, 2000, 180}, 1000},
		{costGeo{30, 30, 150, 1500, 180}, 750}, {costGeo{50, 50, 150, 2000, 180}, 1000},
		{costGeo{5, 5, 150, 750, 180}, 400},
	}
}

// capGrid runs the geometries for the profiles, announced or not, and calls onCell (when
// not nil) with every cell once it has run. strict makes ready% and demand per-seed
// assertions; otherwise the caller checks the cell.
func capGrid(t *testing.T, geos []capGeo, profiles []FaultProfile, announced, strict bool, onCell func(t *testing.T, g capGeo, profile FaultProfile, c capCell)) {
	seeds := costSeeds()
	for _, g := range thinCap(geos) {
		for _, profile := range profiles {
			g, profile := g, profile
			t.Run(fmt.Sprintf("%s/%s", g, profile), func(t *testing.T) {
				t.Parallel()
				c := runCapCell(t, g, profile, seeds, announced, strict)
				if testing.Verbose() {
					t.Log(c)
				}
				if onCell != nil {
					onCell(t, g, profile, c)
				}
			})
		}
	}
}

// Strict property with the schedule announced: the project's default swap scenario (a
// plug for half the buses) at three limits, the brief's limit 2000 with targets 500 and
// 800, and depots with a plug for every bus where the cap composes with the deferral.
func TestDemandCapStrictWithTheSchedule(t *testing.T) {
	capGrid(t, capGeometries(), []FaultProfile{ProfileNone, ProfileMild}, true, true, func(t *testing.T, g capGeo, profile FaultProfile, c capCell) {
		// A cap that never acts would pass the strict property trivially: in the cells
		// where the target is feasible it must lower the demand (measured, over seeds
		// 1..8: 20% at 50 buses/25 chargers/2000 kW/arrivals 0..180/target 1600, 50% at
		// 30 buses/30 chargers/1500 kW/target 750).
		if profile != ProfileNone {
			return
		}
		want := map[string]float64{
			"50/25/2000/180/1600": 0.15, "30/30/1500/180/750": 0.35,
		}[fmt.Sprintf("%d/%d/%.0f/%d/%.0f", g.buses, g.chargers, g.limit, g.arrivalTo, g.target)]
		if want > 0 && c.withDemand > (1-want)*c.withoutDemand {
			t.Errorf("demand %.0f kW with the cap, %.0f without: want at least %.0f%% less", c.withDemand/float64(c.seeds), c.withoutDemand/float64(c.seeds), 100*want)
		}
	})
}

// The same geometries without the schedule (the planner sees only the buses that have
// arrived): the cap waits until every plug is taken and gives way while a savable bus
// waits for one.
func TestDemandCapStrictWithoutTheSchedule(t *testing.T) {
	// The first 14 geometries are the 50-bus/25-charger depot; with a plug for every bus
	// the cap simply waits for the last bus and is checked by the announced test.
	capGrid(t, capGeometries()[:14], []FaultProfile{ProfileNone, ProfileMild}, false, true, nil)
}

// capPoorBounds pin the measured exposure without the schedule in depots with many more
// buses than chargers: the cap can slow the turnover of plugs before the first bus has
// waited. Measured over seeds 1..8 on capPoorGeometries, profiles none and mild.
var capPoorBounds = struct {
	sumLoss, worstLoss float64
	worse              int
}{sumLoss: 20, worstLoss: 4, worse: 6}

func TestDemandCapWithoutTheScheduleInPlugPoorDepots(t *testing.T) {
	capGrid(t, capPoorGeometries(), []FaultProfile{ProfileNone, ProfileMild}, false, false, func(t *testing.T, g capGeo, profile FaultProfile, c capCell) {
		if c.sumLoss > capPoorBounds.sumLoss || c.worstLoss > capPoorBounds.worstLoss || c.worse > capPoorBounds.worse {
			t.Errorf("%d seeds worse, %.1f points lost in all, worst %.1f; bounds %d, %.0f, %.0f", c.worse, c.sumLoss, c.worstLoss, capPoorBounds.worse, capPoorBounds.sumLoss, capPoorBounds.worstLoss)
		}
	})
}

// With the schedule announced the same depots are strict: the energy test sees the whole
// night.
func TestDemandCapWithTheScheduleInPlugPoorDepots(t *testing.T) {
	capGrid(t, capPoorGeometries(), []FaultProfile{ProfileNone, ProfileMild}, true, true, nil)
}

// capFaultBounds pin the measured worst cell under severe and random faults (SoC noise,
// consumption faults, sensors freezing, chargers failing, planner panics and garbage),
// over seeds 1..8 on capFaultGeometries, announced or not: 28 points lost in all (50
// buses/25 chargers/2000 kW, random), 22 on a single seed (the same), 3 seeds worse. One
// seed of the 5-bus depot loses 20 (one bus of five). The strict per-seed property does
// not hold there, as for the deferral; the cap gives way while any bus on charge has an
// unreliable SoC reading, which cut the losses by half but cannot see a sensor that
// starts to lie later.
var capFaultBounds = struct {
	sumLoss, worstLoss float64
	worse              int
}{sumLoss: 30, worstLoss: 22, worse: 3}

func TestDemandCapUnderFaults(t *testing.T) {
	check := func(t *testing.T, g capGeo, profile FaultProfile, c capCell) {
		if c.violations != 0 {
			t.Errorf("%d runs with plan violations", c.violations)
		}
		if c.sumLoss > capFaultBounds.sumLoss || c.worstLoss > capFaultBounds.worstLoss || c.worse > capFaultBounds.worse {
			t.Errorf("%d seeds worse, %.1f points lost in all, worst %.1f; bounds %d, %.0f, %.0f", c.worse, c.sumLoss, c.worstLoss, capFaultBounds.worse, capFaultBounds.sumLoss, capFaultBounds.worstLoss)
		}
	}
	for _, announced := range []bool{true, false} {
		capGrid(t, capFaultGeometries(), []FaultProfile{ProfileSevere, ProfileRandom}, announced, false, check)
	}
}

// With a zero target the cost policy is exactly what it was: same metrics as a policy
// without the field, announced or not.
func TestDemandCapZeroTargetChangesNothing(t *testing.T) {
	g := capGeo{costGeo{50, 25, 150, 1200, 0}, 0}
	for seed := int64(1); seed <= 2; seed++ {
		sc := Generate(g.params(ProfileMild), seed)
		withTarget := propertyConfig()
		withTarget.Cost = &planner.CostPolicy{DeferMarginMin: 45, DemandTargetKW: 0}
		for _, blind := range []bool{true, false} {
			a, b := runWith(sc, propertyConfig(), blind), runWith(sc, withTarget, blind)
			a.PlanP99Micros, b.PlanP99Micros = 0, 0
			billA, billB := *a.Bill, *b.Bill
			a.Bill, b.Bill = nil, nil
			if fmt.Sprint(a) != fmt.Sprint(b) || billA != billB {
				t.Errorf("seed %d blind=%v: metrics differ\n%+v\n%+v", seed, blind, a, b)
			}
		}
	}
}

// TestDemandCapReport prints every cell of capGeometries (both modes, none and mild);
// DEMAND_REPORT=1 go test -run TestDemandCapReport -v ./internal/sim
func TestDemandCapReport(t *testing.T) {
	if os.Getenv("DEMAND_REPORT") != "1" {
		t.Skip("set DEMAND_REPORT=1")
	}
	geos := append(capGeometries(), capPoorGeometries()...)
	if os.Getenv("DEMAND_FAULTS") == "1" {
		geos = capFaultGeometries()
	}
	profiles := []FaultProfile{ProfileNone, ProfileMild}
	if os.Getenv("DEMAND_FAULTS") == "1" {
		profiles = []FaultProfile{ProfileSevere, ProfileRandom}
	}
	for _, g := range geos {
		for _, profile := range profiles {
			for _, ann := range []bool{true, false} {
				c := runCapCellCfg(nil, propertyConfig(), g, profile, 8, ann, false, true)
				fmt.Printf("%s %s announced=%v: %s\n", g, profile, ann, c)
			}
		}
	}
}

// TestDemandCapSweep is the wide random sweep for the cap (random depots of 5..80 buses and
// 3..80 chargers, targets at 30%-90% of the limit, none and mild, announced or not);
// COST_CAP_SWEEP=1, with COST_SWEEP_GEOS, COST_SWEEP_SEEDS and COST_SWEEP_RNG to change the
// sample. Prints the totals and the cells with a worse seed or a higher demand.
func TestDemandCapSweep(t *testing.T) {
	if os.Getenv("COST_CAP_SWEEP") != "1" {
		t.Skip("set COST_CAP_SWEEP=1 to run the wide random sweep of the demand cap")
	}
	nGeo, nSeeds := 120, 8
	fmt.Sscan(os.Getenv("COST_SWEEP_GEOS"), &nGeo)
	fmt.Sscan(os.Getenv("COST_SWEEP_SEEDS"), &nSeeds)
	rngSeed := int64(20261012)
	fmt.Sscan(os.Getenv("COST_SWEEP_RNG"), &rngSeed)
	rng := rand.New(rand.NewSource(rngSeed))
	type job struct {
		g       capGeo
		ann     bool
		profile FaultProfile
	}
	var jobs []job
	for i := 0; i < nGeo; i++ {
		g := costGeo{
			buses: 5 + rng.Intn(76), chargers: 3 + rng.Intn(78), chargerKW: float64(50 + rng.Intn(101)),
			arrivalTo: []int{0, 120, 180, 240}[rng.Intn(4)],
		}
		g.limit = math.Round(float64(g.chargers) * g.chargerKW * (0.1 + 0.9*rng.Float64()))
		target := math.Round(g.limit * (0.3 + 0.6*rng.Float64()))
		for _, ann := range []bool{false, true} {
			jobs = append(jobs, job{capGeo{g, target}, ann, ProfileNone}, job{capGeo{g, target}, ann, ProfileMild})
		}
	}
	type result struct {
		job
		cell capCell
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
			results[i] = result{j, runCapCellCfg(nil, planner.DefaultConfig(), j.g, j.profile, nSeeds, j.ann, false, false)}
		}(i, j)
	}
	wg.Wait()
	for _, ann := range []bool{false, true} {
		cells, worseCells, seedsWorse, demandWorse := 0, 0, 0, 0
		var lines []string
		red, chg, worst := 0.0, 0.0, 0.0
		for _, r := range results {
			if r.ann != ann {
				continue
			}
			c := r.cell
			cells++
			seedsWorse += c.worse
			demandWorse += c.demandWorse
			worst = math.Max(worst, c.worstLoss)
			red += 1 - c.withDemand/c.withoutDemand
			chg += c.withCost/c.withoutCost - 1
			if c.worse > 0 || c.demandWorse > 0 {
				worseCells++
				lines = append(lines, fmt.Sprintf("  %s %s: %s", r.g, r.profile, c))
			}
		}
		sort.Strings(lines)
		t.Logf("announced=%v: %d cells, %d with a worse seed (%d seeds, worst %.1f points), demand higher in %d seeds; mean demand reduction %.1f%%, mean energy change %+.2f%%\n%s",
			ann, cells, worseCells, seedsWorse, worst, demandWorse, 100*red/float64(cells), 100*chg/float64(cells), strings.Join(lines, "\n"))
	}
}
