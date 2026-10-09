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
	asJSON := fs.Bool("json", false, "escreve o relatório completo como JSON (em vez das tabelas)")
	workers := fs.Int("workers", runtime.NumCPU(), "execuções simultâneas (1 ou mais; o resultado não depende disso)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Uso: replay -dir PASTA [-soc-noise KWH] [-swap-back-cooldown N] [-swap-back-min-need KWH] [-workers N] [-json]")
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
	if msg := validateFlags(*dir, *socNoise, cfg, *workers); msg != "" {
		fmt.Fprintln(stderr, msg)
		return 2
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

	rep, err := realdata.Replay(context.Background(), d, cfg, realdata.NightOptions{SoCNoiseKWh: *socNoise}, *workers)
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
		printTable(w, n.Real, n.Controllers, rep.CostComparable)
		printRealNote(w, n.Real)
		printNotReady(w, n)
	}
	fmt.Fprintf(w, "\nAgregado (%s; média por noite)\n", nightsWord(len(rep.Nights)))
	printTable(w, rep.RealAggregate, rep.Aggregate, rep.CostComparable)
	printRealNote(w, rep.RealAggregate)
	if !rep.CostComparable {
		fmt.Fprintln(w, noCostNote)
	}
}

const noCostNote = "Nota: o custo simulado não é comparável com o custo real (sem tarifa, ou janela de ponta que atravessa a meia-noite): \"cost R$\" aparece como " + dash + " nas linhas simuladas."

// printTable prints the simrun columns (without p99): first the measured row.
func printTable(w io.Writer, real realdata.Real, rows []realdata.ControllerResult, costComparable bool) {
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	// energy kWh: grid energy delivered (mean per run). moves/run: swaps executed
	// (planner, when operators follow them) or buses unplugged (fifo-unplug).
	fmt.Fprintln(tw, "controller\tready%\tshortfall kWh\tpeak kW\tplan violations\tovershoot min\tenergy kWh\tcost R$\tplan changes\tmoves/run")
	ready, shortfall := dash, dash
	if real.WithOutcome > 0 {
		ready, shortfall = fmt.Sprintf("%.1f", real.ReadyPct), fmt.Sprintf("%.1f", real.ShortfallKWh)
	}
	fmt.Fprintf(tw, "real\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		ready, shortfall,
		optional(real.PeakKW, real.PeakEstimated, real), dash, dash,
		optional(real.EnergyKWh, false, real), optional(real.CostBRL, real.CostEstimated, real), dash, dash)
	for _, r := range rows {
		a := r.Aggregate
		cost := dash
		if costComparable {
			cost = fmt.Sprintf("%.0f", a.CostBRL)
		}
		fmt.Fprintf(tw, "%s\t%.1f\t%.1f\t%.0f\t%d\t%d\t%.0f\t%s\t%d\t%.1f\n",
			r.Name, a.ReadyPct, a.ShortfallKWh, a.PeakKW, a.PlanViolations, a.OvershootMin, a.EnergyKWh, cost, a.PlanChanges, a.OperatorMoves)
	}
	tw.Flush()
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

// printRealNote explains the "real" row when it covers only part of the buses.
func printRealNote(w io.Writer, r realdata.Real) {
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
