// Command simrun compares charging controllers on generated depot scenarios.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

// Upper bounds that keep a single invocation from exhausting memory or time.
const (
	maxBuses    = 10000
	maxChargers = 10000
	maxSeeds    = 1000
	maxLimitKW  = 1e7 // above this the verifier's absolute tolerance is near float rounding
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("simrun", flag.ContinueOnError)
	fs.SetOutput(stderr)
	p := sim.DefaultGenParams()
	fs.IntVar(&p.NumBuses, "buses", p.NumBuses, "number of buses")
	fs.IntVar(&p.NumChargers, "chargers", p.NumChargers, "number of chargers")
	fs.Float64Var(&p.LimitKW, "limit", p.LimitKW, "site power limit in kW")
	profile := fs.String("profile", "none", "fault profile: none|mild|severe|random")
	seeds := fs.Int("seeds", 20, "number of random seeds")
	logPath := fs.String("log", "", "write the planner decision log (JSON lines) for seed 1")
	fs.BoolVar(&p.FollowSwaps, "follow-swaps", p.FollowSwaps,
		"operators execute every swap the planner recommends (assumption; false = planner without swaps)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	switch sim.FaultProfile(*profile) {
	case sim.ProfileNone, sim.ProfileMild, sim.ProfileSevere, sim.ProfileRandom:
	default:
		fmt.Fprintf(stderr, "unknown profile %q (use none, mild, severe or random)\n", *profile)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *seeds < 1 || *seeds > maxSeeds {
		fmt.Fprintf(stderr, "-seeds must be between 1 and %d\n", maxSeeds)
		return 2
	}
	if p.NumBuses < 1 || p.NumBuses > maxBuses {
		fmt.Fprintf(stderr, "-buses must be between 1 and %d\n", maxBuses)
		return 2
	}
	if p.NumChargers < 1 || p.NumChargers > maxChargers {
		fmt.Fprintf(stderr, "-chargers must be between 1 and %d\n", maxChargers)
		return 2
	}
	// Written so that NaN and +/-Inf fail the check.
	if !(p.LimitKW > 0 && p.LimitKW <= maxLimitKW) {
		fmt.Fprintf(stderr, "-limit must be a finite value in (0, %g] kW\n", maxLimitKW)
		return 2
	}
	p.Profile = sim.FaultProfile(*profile)
	cfg := planner.DefaultConfig()

	type maker struct {
		name string
		make func(sim.Scenario) sim.Controller
	}
	makers := []maker{
		{"fifo", func(sim.Scenario) sim.Controller { return sim.NewFIFO() }},
		{"edf", func(sim.Scenario) sim.Controller { return sim.NewEDF() }},
		// FIFO plus today's manual routine: operators unplug buses that reached their
		// target when another bus is waiting.
		{"fifo-unplug", func(sim.Scenario) sim.Controller { return sim.NewFIFO() }},
		{"safe", func(sim.Scenario) sim.Controller { return sim.NewSafeOnly() }},
		{"planner", func(sc sim.Scenario) sim.Controller { return sim.NewPlannerController(cfg, sc) }},
	}

	fmt.Fprintf(stdout, "profile: %s, limit: %g kW, follow swaps: %v\n", p.Profile, p.LimitKW, p.FollowSwaps)
	w := tabwriter.NewWriter(stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "controller\tready%\tshortfall kWh\tpeak kW\tplan violations\tovershoot min\tcost R$\tplan changes\tp99 µs")
	for _, mk := range makers {
		var results []sim.Metrics
		for seed := int64(1); seed <= int64(*seeds); seed++ {
			sc := sim.Generate(p, seed)
			sc.UnplugFull = mk.name == "fifo-unplug"
			results = append(results, sim.Run(sc, mk.make(sc), nil))
		}
		a := sim.Aggregate(results)
		fmt.Fprintf(w, "%s\t%.1f\t%.1f\t%.0f\t%d\t%d\t%.0f\t%d\t%d\n",
			mk.name, a.ReadyPct, a.ShortfallKWh, a.PeakKW, a.PlanViolations, a.OvershootMin, a.CostBRL, a.PlanChanges, a.PlanP99Micros)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if *logPath != "" {
		if err := writeDecisionLog(*logPath, p, cfg); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "decision log written to %s\n", *logPath)
	}
	return 0
}

// writeDecisionLog replays seed 1 with the planner and writes its decision log.
// Errors from the log itself are checked because sim.Run ignores Record errors.
func writeDecisionLog(path string, p sim.GenParams, cfg planner.Config) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	sc := sim.Generate(p, 1)
	log := sim.NewDecisionLog(f)
	sim.Run(sc, sim.NewPlannerController(cfg, sc), log)
	if err := log.Err(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
