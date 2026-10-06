// Command simrun compares charging controllers on generated depot scenarios.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
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
	fs.IntVar(&p.ReadingAgeMin, "reading-age", p.ReadingAgeMin,
		"minutes of age the SoC sensor reports for a fresh reading (models a gateway with delayed readings)")
	cfg := planner.DefaultConfig()
	fs.IntVar(&cfg.SwapBackCooldownMin, "swap-back-cooldown", cfg.SwapBackCooldownMin,
		"anti-thrash: minutes before a bus that gave way may be swapped back (0 and -swap-back-min-need 0: rule off)")
	fs.Float64Var(&cfg.SwapBackMinNeedKWh, "swap-back-min-need", cfg.SwapBackMinNeedKWh,
		"anti-thrash: kWh a bus that gave way must need, by the mean of its readings while it waits, to be swapped back")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	p.Profile = sim.FaultProfile(*profile)
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if err := sim.ValidateParams(p, cfg, *seeds); err != nil {
		fmt.Fprintf(stderr, "-%s\n", err)
		return 2
	}

	results, err := sim.Compare(context.Background(), p, cfg, *seeds, 1)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprintf(stdout, "profile: %s, limit: %g kW, follow swaps: %v, swap back: after %d min, need > %g kWh",
		p.Profile, p.LimitKW, p.FollowSwaps, cfg.SwapBackCooldownMin, cfg.SwapBackMinNeedKWh)
	if p.ReadingAgeMin != 0 {
		fmt.Fprintf(stdout, ", reading age: %d min", p.ReadingAgeMin)
	}
	fmt.Fprintln(stdout)
	w := tabwriter.NewWriter(stdout, 0, 8, 2, ' ', 0)
	// energy kWh: grid energy delivered per run (mean). moves/run: swaps executed
	// (planner, when operators follow them) or buses unplugged (fifo-unplug).
	fmt.Fprintln(w, "controller\tready%\tshortfall kWh\tpeak kW\tplan violations\tovershoot min\tenergy kWh\tcost R$\tplan changes\tmoves/run\tp99 µs")
	for _, r := range results {
		a := r.Aggregate
		fmt.Fprintf(w, "%s\t%.1f\t%.1f\t%.0f\t%d\t%d\t%.0f\t%.0f\t%d\t%.1f\t%d\n",
			r.Name, a.ReadyPct, a.ShortfallKWh, a.PeakKW, a.PlanViolations, a.OvershootMin, a.EnergyKWh, a.CostBRL, a.PlanChanges, a.OperatorMoves, a.PlanP99Micros)
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
	log := sim.NewDecisionLogWithConfig(f, cfg) // header: replayable with sim.ReplayFile
	sim.Run(sc, sim.NewPlannerController(cfg, sc), log)
	if err := log.Err(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
