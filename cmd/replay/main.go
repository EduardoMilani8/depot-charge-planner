// Command replay replays the nights of a real depot (CSV spreadsheets) through
// the simulator and compares what happened with the planner and the baselines.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/realdata"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

const (
	dash       = "—"
	estimated  = " (estimado)"
	maxWorkers = 1024
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run returns 0 on success, 1 for an execution error and 2 for a usage or data
// error (the message goes to stderr; it never panics on bad spreadsheets).
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := planner.DefaultConfig()
	dir := fs.String("dir", "", "pasta com as planilhas CSV da garagem (obrigatório): garagem, carregadores, onibus, sessoes, potencia")
	socNoise := fs.Float64("soc-noise", 0, "ruído (desvio padrão, kWh) nas leituras de SoC dos ônibus, para testar a sensibilidade (0 = leitura perfeita)")
	fs.IntVar(&cfg.SwapBackCooldownMin, "swap-back-cooldown", cfg.SwapBackCooldownMin,
		"anti-vaivém: minutos antes de um ônibus que cedeu o carregador poder voltar (0 e -swap-back-min-need 0: regra desligada)")
	fs.Float64Var(&cfg.SwapBackMinNeedKWh, "swap-back-min-need", cfg.SwapBackMinNeedKWh,
		"anti-vaivém: kWh que um ônibus que cedeu o carregador precisa ter de necessidade, pela média das leituras enquanto espera, para voltar")
	// Energy cost (see docs/superpowers/specs/2026-10-11-custo-de-energia-design.md).
	costAware := fs.Bool("cost-aware", false,
		"planner: adia a carga para depois da ponta quando os ônibus têm folga e limita a demanda em -demand-target (o adiamento só age com -announce-arrivals; as outras linhas não mudam)")
	deferMargin := fs.Int("defer-margin", planner.DefaultDeferMarginMin,
		fmt.Sprintf("com -cost-aware: minutos de folga que um ônibus adiado ainda deve ter quando a ponta termina (0 a %d; 0 = %d)", sim.MaxDeferMarginMin, planner.DefaultDeferMarginMin))
	demandTarget := fs.Float64("demand-target", 0,
		"com -cost-aware: limita a demanda da rede perto destes kW quando a prontidão permite (0 = desligado; acima do limite da garagem não tem efeito). Um teto pode aumentar a fatura mensal")
	announce := fs.Bool("announce-arrivals", false,
		"com -cost-aware: o planner conhece a agenda da noite (os ônibus que ainda não chegaram). Padrão: não conhece, como numa noite reproduzida sem escala anunciada; sem isso o adiamento não age")
	asJSON := fs.Bool("json", false, "escreve o relatório completo como JSON (em vez das tabelas)")
	workers := fs.Int("workers", runtime.NumCPU(), "execuções simultâneas (1 ou mais; o resultado não depende disso)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Uso: replay -dir PASTA [-soc-noise KWH] [-swap-back-cooldown N] [-swap-back-min-need KWH] [-cost-aware [-defer-margin MIN] [-demand-target KW] [-announce-arrivals]] [-workers N] [-json]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "argumento inesperado %q\n", fs.Arg(0))
		return 2
	}
	if *costAware {
		cfg.Cost = &planner.CostPolicy{DeferMarginMin: float64(*deferMargin), DemandTargetKW: *demandTarget}
	} else {
		// Silently ignoring these would let a run look cost-aware when it is not.
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		for _, name := range []string{"defer-margin", "demand-target", "announce-arrivals"} {
			if set[name] {
				fmt.Fprintf(stderr, "-%s só vale com -cost-aware (ele configura o planner com custo)\n", name)
				return 2
			}
		}
	}
	if msg := validateFlags(*dir, *socNoise, cfg, *workers); msg != "" {
		fmt.Fprintln(stderr, msg)
		return 2
	}
	if *costAware && !*announce {
		fmt.Fprintln(stderr, "nota: -cost-aware sem -announce-arrivals: o planner não conhece as chegadas futuras e não adia carga para depois da ponta (só a meta de demanda, se houver, age); use -announce-arrivals se a agenda da noite é conhecida")
	}

	d, err := realdata.LoadDir(*dir)
	if err != nil {
		var fe *realdata.FieldError
		if errors.As(err, &fe) {
			if fe.File == "" { // e.g. the folder itself: say which one was asked for
				fmt.Fprintf(stderr, "erro: %s (-dir %q)\n", fe.Error(), *dir)
			} else {
				fmt.Fprintf(stderr, "erro: %s\n", fe.Error())
			}
			return 2
		}
		fmt.Fprintf(stderr, "erro: %v\n", err)
		return 1
	}

	rep, err := realdata.Replay(context.Background(), d, cfg, realdata.NightOptions{SoCNoiseKWh: *socNoise, AnnounceArrivals: *announce}, *workers)
	if err != nil {
		fmt.Fprintf(stderr, "erro: %v\n", err)
		return 1
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(stderr, "erro: %v\n", err)
			return 1
		}
		return 0
	}
	ew := &errWriter{w: stdout}
	printReport(ew, rep, cfg)
	if ew.err != nil {
		fmt.Fprintf(stderr, "erro: %v\n", ew.err)
		return 1
	}
	return 0
}

// validateFlags returns a usage message ("" when everything is fine). Values the
// library would silently ignore or misread (NaN, Inf, negatives) are refused here.
func validateFlags(dir string, socNoise float64, cfg planner.Config, workers int) string {
	switch {
	case strings.TrimSpace(dir) == "":
		return "-dir é obrigatório: informe a pasta com as planilhas (garagem.csv, carregadores.csv, onibus.csv, ...)"
	case !(socNoise >= 0 && socNoise <= sim.MaxLimitKW):
		return fmt.Sprintf("-soc-noise deve ser um número finito entre 0 e %g kWh", float64(sim.MaxLimitKW))
	case cfg.SwapBackCooldownMin < 0:
		return "-swap-back-cooldown deve ser 0 ou mais (minutos)"
	case !(cfg.SwapBackMinNeedKWh >= 0 && cfg.SwapBackMinNeedKWh <= sim.MaxLimitKW):
		return fmt.Sprintf("-swap-back-min-need deve ser um número finito entre 0 e %g kWh", float64(sim.MaxLimitKW))
	case workers < 1 || workers > maxWorkers:
		return fmt.Sprintf("-workers deve estar entre 1 e %d", maxWorkers)
	}
	if c := cfg.Cost; c != nil {
		switch {
		case !(c.DeferMarginMin >= 0 && c.DeferMarginMin <= sim.MaxDeferMarginMin):
			return fmt.Sprintf("-defer-margin deve estar entre 0 e %d minutos (0 = %d)", sim.MaxDeferMarginMin, planner.DefaultDeferMarginMin)
		case !(c.DemandTargetKW >= 0 && c.DemandTargetKW <= sim.MaxLimitKW):
			return fmt.Sprintf("-demand-target deve ser um número finito entre 0 e %g kW (0 = desligado)", float64(sim.MaxLimitKW))
		}
	}
	return ""
}

// errWriter remembers the first write error so the printing code stays linear.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	e.err = err
	return n, err
}

func printReport(w io.Writer, rep *realdata.Report, cfg planner.Config) {
	for _, wn := range rep.Warnings {
		fmt.Fprintf(w, "aviso: %s\n", warningText(wn))
	}
	if len(rep.Warnings) > 0 {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Premissas:")
	for _, a := range rep.Assumptions {
		fmt.Fprintf(w, "- %s\n", a)
	}
	fmt.Fprintf(w, "Planejador: um ônibus que cedeu o carregador só volta após %d min e com necessidade > %g kWh.\n",
		cfg.SwapBackCooldownMin, cfg.SwapBackMinNeedKWh)

	if len(rep.Nights) == 0 {
		fmt.Fprintln(w, "\nNenhuma noite pôde ser reproduzida (veja os avisos acima).")
		return
	}
	for _, n := range rep.Nights {
		fmt.Fprintf(w, "\nNoite %s (%d ônibus)\n", n.Key, n.Buses)
		printTable(w, n.Real, n.Controllers, rep.CostComparable, rep.DemandBilled)
		printMatched(w, n.Real, n.Matched)
		printRealNote(w, n.Real)
		printNotReady(w, n)
	}
	fmt.Fprintf(w, "\nAgregado (%s; %s)\n", nightsWord(len(rep.Nights)), aggregateNote)
	printTable(w, rep.RealAggregate, rep.Aggregate, rep.CostComparable, rep.DemandBilled)
	printMatched(w, rep.RealAggregate, rep.MatchedAggregate)
	printRealNote(w, rep.RealAggregate)
	if !rep.CostComparable {
		fmt.Fprintln(w, noCostNote)
	}
	if rep.DemandBilled {
		printBillNotes(w, rep)
	}
}

// aggregateNote says what the aggregate rows are: most columns are means per night, but the
// counts of violations and overshoot minutes are sums over the nights and plan changes is
// the mean cut to a whole number (that is how sim.Aggregate works).
const aggregateNote = "média por noite, exceto plan violations e overshoot min, que são somas das noites, e plan changes, média arredondada para baixo"

const noCostNote = "Nota: o custo simulado não é comparável com o custo real (garagem.csv não tem a tarifa de energia completa): \"cost R$\" aparece como " + dash + " nas linhas simuladas."

// printTable prints the simrun columns (without p99): first the measured row. With demand
// prices (demand) two columns follow "cost R$": demand kW (the largest 15-minute block) and
// monthly R$; without them the table is exactly what it was before the demand columns existed.
func printTable(w io.Writer, real realdata.Real, rows []realdata.ControllerResult, costComparable, demand bool) {
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	// energy kWh: grid energy delivered (mean per run). moves/run: swaps executed
	// (planner, when operators follow them) or buses unplugged (fifo-unplug).
	head := "controller\tready%\tshortfall kWh\tpeak kW\tplan violations\tovershoot min\tenergy kWh\tcost R$"
	if demand {
		head += "\tdemand kW\tmonthly R$"
	}
	fmt.Fprintln(tw, head+"\tplan changes\tmoves/run")
	ready, shortfall := dash, dash
	if real.WithOutcome > 0 {
		ready, shortfall = fmt.Sprintf("%.1f", real.ReadyPct), fmt.Sprintf("%.1f", real.ShortfallKWh)
	}
	cells := []string{"real", ready, shortfall,
		optional(real.PeakKW, real.PeakEstimated, real), dash, dash,
		optional(real.EnergyKWh, false, real), optional(real.CostBRL, real.CostEstimated, real)}
	if demand {
		cells = append(cells, sealed(real.DemandKW, real.DemandEstimated, real), sealed(real.MonthlyBRL, real.DemandEstimated, real))
	}
	cells = append(cells, dash, dash)
	fmt.Fprintln(tw, strings.Join(cells, "\t"))
	for _, r := range rows {
		a := r.Aggregate
		cost := dash
		if costComparable {
			cost = fmt.Sprintf("%.0f", a.CostBRL)
		}
		fmt.Fprintf(tw, "%s\t%.1f\t%.1f\t%.0f\t%d\t%d\t%.0f\t%s",
			r.Name, a.ReadyPct, a.ShortfallKWh, a.PeakKW, a.PlanViolations, a.OvershootMin, a.EnergyKWh, cost)
		if demand {
			if b := a.Bill; b != nil {
				fmt.Fprintf(tw, "\t%.0f\t%.0f", b.DemandKW, b.MonthlyBRL)
			} else {
				fmt.Fprintf(tw, "\t%s\t%s", dash, dash)
			}
		}
		fmt.Fprintf(tw, "\t%d\t%.1f\n", a.PlanChanges, a.OperatorMoves)
	}
	tw.Flush()
}

// sealed is optional for the demand columns of the real row: it always says where the figure
// comes from, "(medido)" for the blocks of potencia.csv and "(estimado)" for the ones built
// from the sessions, each spread uniformly over its minutes.
func sealed(v *float64, est bool, r realdata.Real) string {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return dash
	}
	s := fmt.Sprintf("%.0f (medido)", *v)
	if est {
		s = fmt.Sprintf("%.0f%s", *v, estimated)
	}
	if r.Partial {
		s += " (" + r.PartialNote + ")"
	}
	return s
}

// printBillNotes explains the monthly column and compares the planner with the two
// baselines in estimated reais per month, from the aggregate rows (the mean night). Same
// sentence as simrun's.
func printBillNotes(w io.Writer, rep *realdata.Report) {
	fmt.Fprintf(w, "monthly R$ = estimativa: energia da noite x %d noites + demanda (proxy: a demanda de uma noite vale o mês); demand kW é o maior bloco de 15 minutos; na linha real, (medido) vem de potencia.csv e (estimado) das sessões, cada uma espalhada de modo uniforme\n", rep.NightsPerMonth)
	monthly := map[string]float64{}
	for _, r := range rep.Aggregate {
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
	fmt.Fprintln(w, "; compare com o ready%: um fifo que deixa ônibus sem carga gasta menos, então não é uma comparação equivalente")
}

// optional formats a measured value with no decimals: "—" when unknown, with the
// "(estimado)" seal when it was derived from the sessions instead of measured and
// the "(parcial: X de Y ônibus)" seal when the figures do not cover the whole night.
func optional(v *float64, est bool, r realdata.Real) string {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return dash
	}
	s := fmt.Sprintf("%.0f", *v)
	if est {
		s += estimated
	}
	if r.Partial {
		s += " (" + r.PartialNote + ")"
	}
	return s
}

// printMatched prints the fair comparison under a table: ready% (and shortfall) of the
// real outcome, the planner and the planner without swaps over the same buses, the ones
// that have soc_saida_real_pct (the simulated rows above cover every bus).
func printMatched(w io.Writer, real realdata.Real, m realdata.Matched) {
	if m.Buses == 0 {
		return
	}
	fmt.Fprintf(w, "Comparação justa (mesmos %d ônibus do real): real %.1f%% | planner %.1f%% | sem rodízio %.1f%% (shortfall kWh: real %.1f | planner %.1f | sem rodízio %.1f)\n",
		m.Buses, real.ReadyPct, m.PlannerReadyPct, m.NoSwapReadyPct, real.ShortfallKWh, m.PlannerShortfallKWh, m.NoSwapShortfallKWh)
}

// printRealNote explains the "real" row when it covers only part of the buses.
func printRealNote(w io.Writer, r realdata.Real) {
	if r.LateDepartures > 0 {
		fmt.Fprintf(w, "Nota: %s.\n", realdata.LateDeparturesText(r.LateDepartures))
	}
	if r.Partial {
		fmt.Fprintf(w, "Nota: energia, pico e custo reais cobrem só parte da noite (%s) e não são comparáveis com as linhas simuladas, que cobrem todos os ônibus (veja os avisos).\n", r.PartialNote)
	}
	switch {
	case r.Buses > 0 && r.WithOutcome == 0:
		fmt.Fprintf(w, "Nota: nenhum dos %d ônibus tem soc_saida_real_pct: ready%% e shortfall reais são desconhecidos.\n", r.Buses)
	case r.WithOutcome < r.Buses:
		fmt.Fprintf(w, "Nota: ready%% e shortfall reais contam só os %d de %d ônibus com soc_saida_real_pct; os demais não entram como não prontos.\n", r.WithOutcome, r.Buses)
	}
}

// printNotReady lists the buses that were not ready in reality and what the
// planner (following the swaps / without them) ends with for each.
func printNotReady(w io.Writer, n realdata.NightReport) {
	var late []realdata.BusRow
	for _, b := range n.PerBus {
		if b.RealReady != nil && !*b.RealReady {
			late = append(late, b)
		}
	}
	if len(late) == 0 {
		if n.Real.WithOutcome == 0 {
			// no outcome is not "everybody was ready": the spreadsheets just do not say
			fmt.Fprintln(w, "Resultado real desconhecido: sem soc_saida_real_pct não dá para dizer quais ônibus ficaram sem a carga exigida.")
		} else {
			fmt.Fprintf(w, "Nenhum dos %d ônibus com resultado real ficou sem a carga exigida.\n", n.Real.WithOutcome)
		}
		return
	}
	fmt.Fprintln(w, "Ônibus não prontos na realidade (alvo; real; planner; planner sem rodízio):")
	for _, b := range late {
		real := dash
		if b.RealFinalKWh != nil {
			real = fmt.Sprintf("%.1f kWh", *b.RealFinalKWh)
		}
		fmt.Fprintf(w, "  %s: alvo %.1f kWh; real %s (não pronto); planner %.1f kWh (%s); sem rodízio %.1f kWh (%s)\n",
			b.ID, b.TargetKWh, real, b.PlannerFinalKWh, readyWord(b.PlannerReady), b.NoSwapFinalKWh, readyWord(b.NoSwapReady))
	}
}

func readyWord(ok bool) string {
	if ok {
		return "pronto"
	}
	return "não pronto"
}

// warningText: "arquivo, linha N: mensagem" (the parts that exist).
func warningText(wn realdata.Warning) string {
	var where []string
	if wn.File != "" {
		where = append(where, wn.File)
	}
	if wn.Line > 0 {
		where = append(where, fmt.Sprintf("linha %d", wn.Line))
	}
	if len(where) == 0 {
		return wn.Message
	}
	return strings.Join(where, ", ") + ": " + wn.Message
}

func nightsWord(n int) string {
	if n == 1 {
		return "1 noite"
	}
	return fmt.Sprintf("%d noites", n)
}
