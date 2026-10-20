// Command simrun compares charging controllers on generated depot scenarios.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/EduardoMilani8/depot-charge-planner/internal/billing"
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
	// Energy cost (see docs/superpowers/specs/2026-10-11-custo-de-energia-design.md).
	costAware := fs.Bool("cost-aware", false,
		"planner: defer charging past the peak window when buses have slack, and cap the demand at -demand-target (needs a plug per bus and announced arrivals; the other controllers are unchanged)")
	deferMargin := fs.Int("defer-margin", defaultDeferMarginMin,
		"with -cost-aware: minutes of slack a deferred bus must still have when the peak ends (0..240; 0 = 45)")
	demandTarget := fs.Float64("demand-target", 0,
		"with -cost-aware: cap the grid demand near this many kW when readiness allows (0 = off; above -limit it has no effect). A cap can raise the monthly bill")
	demandMode := fs.String("demand-mode", "azul",
		"demand billing mode: azul (peak and off-peak demand prices) or verde (one price, -demand-offpeak-price, on the whole night; -demand-peak-price is ignored)")
	fs.Float64Var(&p.Demand.PeakPrice, "demand-peak-price", 0, "demand price in the peak window, R$/kW (azul only; 0 = none). Illustrative: use the utility's tariff")
	fs.Float64Var(&p.Demand.OffPeakPrice, "demand-offpeak-price", 0, "demand price outside the peak window, R$/kW (verde: the only price; 0 = none)")
	fs.Float64Var(&p.Demand.ContractedKW, "demand-contracted", 0, "contracted demand in kW (0 = no contract: the largest 15-minute block is billed)")
	fs.IntVar(&p.ArrivalFromMin, "arrival-from", 0,
		"first minute buses may arrive, counted from the scenario start at 18:00 (so 180 = 21:00; with -arrival-to; both 0 = the default 120..360, i.e. 20:00-24:00)")
	fs.IntVar(&p.ArrivalToMin, "arrival-to", 0,
		fmt.Sprintf("last minute buses may arrive, from 18:00 (up to %d; must be after -arrival-from; the peak window is minutes 0..180)", sim.MaxArrivalMin))
	fs.IntVar(&p.Demand.NightsPerMonth, "nights-per-month", billing.DefaultNightsPerMonth,
		"nights the monthly estimate multiplies the night's energy by (1..31; with demand prices)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	p.Profile = sim.FaultProfile(*profile)
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	switch *demandMode {
	case "azul":
	case "verde":
		p.Demand.Verde = true
	default:
		fmt.Fprintf(stderr, "-demand-mode unknown %q (use azul or verde)\n", *demandMode)
		return 2
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if *costAware {
		cfg.Cost = &planner.CostPolicy{DeferMarginMin: float64(*deferMargin), DemandTargetKW: *demandTarget}
	} else {
		// Silently ignoring these would let a run look cost-aware when it is not.
		for _, name := range []string{"demand-target", "defer-margin"} {
			if set[name] {
				fmt.Fprintf(stderr, "-%s needs -cost-aware (it only configures the cost-aware planner)\n", name)
				return 2
			}
		}
	}
	if err := sim.ValidateParams(p, cfg, *seeds); err != nil {
		fmt.Fprintf(stderr, "-%s\n", err)
		return 2
	}
	if p.Demand.Verde && p.Demand.PeakPrice > 0 {
		fmt.Fprintln(stderr, "note: -demand-peak-price is ignored in verde mode (one demand charge, billed at -demand-offpeak-price)")
	}
	if *costAware && *demandTarget > p.LimitKW {
		fmt.Fprintf(stderr, "note: -demand-target %g kW is above -limit %g kW, so it has no effect\n", *demandTarget, p.LimitKW)
	}
	billed := sim.Generate(p, 1).Tariff.HasDemand()

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
	if p.ArrivalFromMin != 0 || p.ArrivalToMin != 0 {
		fmt.Fprintf(stdout, ", arrivals: minutes %d-%d", p.ArrivalFromMin, p.ArrivalToMin)
	}
	if cfg.Cost != nil {
		fmt.Fprintf(stdout, ", cost-aware: defer margin %d min", *deferMargin)
		if *demandTarget > 0 {
			fmt.Fprintf(stdout, ", demand target %g kW", *demandTarget)
		}
	}
	if billed {
		fmt.Fprintf(stdout, ", demand: %s %s, contract %s, %d nights/month", *demandMode, demandPrices(p.Demand), contract(p.Demand.ContractedKW), nights(p.Demand))
	}
	fmt.Fprintln(stdout)
	w := tabwriter.NewWriter(stdout, 0, 8, 2, ' ', 0)
	// energy kWh: grid energy delivered per run (mean). moves/run: swaps executed
	// (planner, when operators follow them) or buses unplugged (fifo-unplug).
	// With demand prices three bill columns follow "cost R$" (energy of the night); p99
	// stays last.
	head := "controller\tready%\tshortfall kWh\tpeak kW\tplan violations\tovershoot min\tenergy kWh\tcost R$"
	if billed {
		head += "\tdemand kW\tdemand R$/mês\tmonthly R$"
	}
	fmt.Fprintln(w, head+"\tplan changes\tmoves/run\tp99 µs")
	for _, r := range results {
		a := r.Aggregate
		fmt.Fprintf(w, "%s\t%.1f\t%.1f\t%.0f\t%d\t%d\t%.0f\t%.0f",
			r.Name, a.ReadyPct, a.ShortfallKWh, a.PeakKW, a.PlanViolations, a.OvershootMin, a.EnergyKWh, a.CostBRL)
		if billed {
			if b := a.Bill; b != nil {
				fmt.Fprintf(w, "\t%.0f\t%.0f\t%.0f", b.DemandKW, b.DemandBRL, b.MonthlyBRL)
			} else {
				fmt.Fprint(w, "\t-\t-\t-")
			}
		}
		fmt.Fprintf(w, "\t%d\t%.1f\t%d\n", a.PlanChanges, a.OperatorMoves, a.PlanP99Micros)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if billed {
		printBillNotes(stdout, results, nights(p.Demand))
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
	// The scenario and controller exactly as the table ran them (a cost policy announces the
	// arrivals), and a header with the configuration the planner really used, so the log
	// replays with sim.ReplayFile.
	sc, ctrl, err := sim.ForController("planner", cfg, sim.Generate(p, 1))
	if err != nil {
		f.Close()
		return err
	}
	log := sim.NewDecisionLogForScenario(f, cfg, sc)
	sim.Run(sc, ctrl, log)
	if err := log.Err(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// defaultDeferMarginMin is the planner's own default for -defer-margin.
const defaultDeferMarginMin = 45

func nights(d sim.DemandParams) int {
	if d.NightsPerMonth <= 0 {
		return billing.DefaultNightsPerMonth
	}
	return d.NightsPerMonth
}

func contract(kw float64) string {
	if kw <= 0 {
		return "none"
	}
	return fmt.Sprintf("%g kW", kw)
}

func demandPrices(d sim.DemandParams) string {
	if d.Verde {
		return fmt.Sprintf("R$ %g/kW (one charge)", d.OffPeakPrice)
	}
	return fmt.Sprintf("R$ %g/kW peak, R$ %g/kW off-peak", d.PeakPrice, d.OffPeakPrice)
}

// printBillNotes explains the monthly column and compares the planner with the two
// baselines in estimated reais per month (the same MonthlyBRL the table shows).
func printBillNotes(w io.Writer, results []sim.ControllerResult, nightsPerMonth int) {
	fmt.Fprintf(w, "monthly R$ = estimativa: energia da noite x %d noites + demanda (proxy: a demanda de uma noite vale o mês); demand R$/mês é a demanda cobrada\n", nightsPerMonth)
	monthly := map[string]float64{}
	for _, r := range results {
		if r.Aggregate.Bill != nil {
			monthly[r.Name] = r.Aggregate.Bill.MonthlyBRL
		}
	}
	pl, ok := monthly["planner"]
	if !ok {
		return
	}
	fmt.Fprint(w, "economia mensal estimada do planner (R$/mês, negativa = custa mais):")
	sep := " "
	for _, base := range []string{"fifo", "fifo-unplug"} {
		bm, ok := monthly[base]
		if !ok {
			continue
		}
		if bm > 0 {
			fmt.Fprintf(w, "%scontra %s R$ %.0f (%.1f%%)", sep, base, bm-pl, 100*(bm-pl)/bm)
		} else {
			fmt.Fprintf(w, "%scontra %s R$ %.0f", sep, base, bm-pl)
		}
		sep = ", "
	}
	fmt.Fprintln(w)
}
