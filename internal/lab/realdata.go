package lab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/realdata"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

const (
	// maxFileEntries bounds the "files" object: the five spreadsheets plus a few stray names
	// (which are ignored with a warning).
	maxFileEntries = 32
	// maxEchoedName is how many characters of a file name may come back in a warning.
	maxEchoedName = 48
	// noSwapController is the planner run as if the operators followed no swap.
	noSwapController = "planner (sem rodízio)"
)

// The server keeps nothing between requests: every real-data route receives the text of the
// spreadsheets again ("files": name -> content) and parses it in memory. Nothing here
// touches the file system, and no request body is logged.

// loadFiles parses the spreadsheets of a request. Only the five fixed names are read; any
// other name is ignored by realdata.Load with a warning (its content is never copied).
func loadFiles(files map[string]string) (*realdata.Dataset, *apiError) {
	if len(files) > maxFileEntries {
		return nil, &apiError{status: http.StatusBadRequest, Field: "files",
			Message: fmt.Sprintf("Arquivos demais: envie no máximo as %d planilhas do kit.", len(realdata.FileNames))}
	}
	in := make(map[string][]byte, len(files))
	for name, content := range files {
		if knownFile(name) {
			in[name] = []byte(content)
			continue
		}
		if r := []rune(name); len(r) > maxEchoedName {
			name = string(r[:maxEchoedName]) + "…"
		}
		in[name] = nil
	}
	d, err := realdata.Load(in)
	if err != nil {
		return nil, loadError(err)
	}
	return d, nil
}

func knownFile(name string) bool {
	for _, n := range realdata.FileNames {
		if n == name {
			return true
		}
	}
	return false
}

// loadError maps a realdata.Load error: a spreadsheet error carries file, line and column.
func loadError(err error) *apiError {
	var fe *realdata.FieldError
	if errors.As(err, &fe) {
		return &apiError{status: http.StatusBadRequest, Message: fe.Error(),
			Field: fe.File, File: fe.File, Line: fe.Line, Column: fe.Column}
	}
	return &apiError{status: http.StatusBadRequest, Message: "Não foi possível ler as planilhas: confira os arquivos enviados."}
}

// replayError maps a realdata.Replay error: the deadline is 504, a client that left is 408
// and anything else is an internal error whose details are not sent.
func replayError(err error) *apiError {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &apiError{status: http.StatusGatewayTimeout, Message: "Tempo esgotado: use menos noites ou menos ônibus."}
	case errors.Is(err, context.Canceled):
		return &apiError{status: http.StatusRequestTimeout, Message: "Requisição cancelada."}
	}
	return &apiError{status: http.StatusInternalServerError, Message: "Erro interno ao comparar as noites."}
}

// swapBackError validates the planner's anti-ping-pong parameters (the only planner
// settings the real-data routes take from the client).
func swapBackError(cooldownMin int, minNeedKWh float64) *apiError {
	p := defaultParams()
	p.SwapBackCooldownMin, p.SwapBackMinNeedKWh = cooldownMin, minNeedKWh
	return p.validate()
}

// replayWorkError refuses a dataset with too many buses for one screen: every bus is
// simulated by six controllers, so the limit is labMaxWork buses (labMaxWork x 6 runs).
func replayWorkError(buses int) *apiError {
	if buses <= labMaxWork {
		return nil
	}
	return &apiError{status: http.StatusBadRequest, Field: "files",
		Message: fmt.Sprintf("Trabalho demais para uma tela: as planilhas têm %d ônibus no total e o limite é %d. Envie menos noites.", buses, labMaxWork)}
}

// ---- POST /api/import ----

type importRequest struct {
	Files map[string]string `json:"files"`
}

type importNightDTO struct {
	Key          string  `json:"key"`
	Buses        int     `json:"buses"`
	WithOutcome  int     `json:"with_outcome"` // buses that have soc_saida_real_pct
	Ready        int     `json:"ready"`        // of those, how many left ready
	RealReadyPct float64 `json:"real_ready_pct"`
	HasOutcome   bool    `json:"has_outcome"` // false: real_ready_pct is not a measurement
}

type importResponse struct {
	Nights      []importNightDTO   `json:"nights"`
	Warnings    []realdata.Warning `json:"warnings"`
	HasSessions bool               `json:"has_sessions"`
	HasPower    bool               `json:"has_power"`
	HasTariff   bool               `json:"has_tariff"`
}

func (s *Server) importFiles(r *http.Request) (any, *apiError) {
	var req importRequest
	if e := decodeBody(r, &req); e != nil {
		return nil, e
	}
	release, ok := s.acquire()
	if !ok {
		return nil, errBusy
	}
	defer release()
	d, e := loadFiles(req.Files)
	if e != nil {
		return nil, e
	}
	nights, warns := d.Nights(realdata.NightOptions{})
	out := importResponse{
		Nights:      make([]importNightDTO, 0, len(nights)),
		Warnings:    append(append([]realdata.Warning{}, d.Warnings...), warns...),
		HasSessions: d.HasSessions, HasPower: d.HasPower, HasTariff: d.Garage.HasTariff,
	}
	for _, n := range nights {
		out.Nights = append(out.Nights, importNightDTO{
			Key: n.Key, Buses: n.Real.Buses, WithOutcome: n.Real.WithOutcome, Ready: n.Real.Ready,
			RealReadyPct: n.Real.ReadyPct, HasOutcome: n.Real.WithOutcome > 0,
		})
	}
	return out, nil
}

// ---- POST /api/replay ----

type replayRequest struct {
	Files               map[string]string `json:"files"`
	SoCNoiseKWh         float64           `json:"soc_noise_kwh"`
	SwapBackCooldownMin int               `json:"swap_back_cooldown_min"`
	SwapBackMinNeedKWh  float64           `json:"swap_back_min_need_kwh"`
}

func (s *Server) replay(r *http.Request) (any, *apiError) {
	cfg := planner.DefaultConfig()
	req := replayRequest{SwapBackCooldownMin: cfg.SwapBackCooldownMin, SwapBackMinNeedKWh: cfg.SwapBackMinNeedKWh}
	if e := decodeBody(r, &req); e != nil {
		return nil, e
	}
	if !(req.SoCNoiseKWh >= 0 && req.SoCNoiseKWh <= sim.MaxLimitKW) {
		return nil, &apiError{status: http.StatusBadRequest, Field: "soc_noise_kwh",
			Message: fmt.Sprintf("Ruído de SoC: informe de 0 a %.0f kWh.", float64(sim.MaxLimitKW))}
	}
	if e := swapBackError(req.SwapBackCooldownMin, req.SwapBackMinNeedKWh); e != nil {
		return nil, e
	}
	cfg.SwapBackCooldownMin, cfg.SwapBackMinNeedKWh = req.SwapBackCooldownMin, req.SwapBackMinNeedKWh
	release, ok := s.acquire()
	if !ok {
		return nil, errBusy
	}
	defer release()
	d, e := loadFiles(req.Files)
	if e != nil {
		return nil, e
	}
	// Counted on the buses of the file, which is an upper bound of the buses simulated
	// (nights left out with a warning are counted too).
	if e := replayWorkError(len(d.Buses)); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	rep, err := realdata.Replay(ctx, d, cfg, realdata.NightOptions{SoCNoiseKWh: req.SoCNoiseKWh}, runtime.NumCPU())
	if err != nil {
		return nil, replayError(err)
	}
	return withoutP99(rep), nil
}

// withoutP99 returns a copy of the report with plan_p99_micros zeroed in every Metrics:
// it is wall-clock time and would make the same request give different bytes. The report
// from realdata.Replay is left as measured.
func withoutP99(rep *realdata.Report) *realdata.Report {
	out := *rep
	out.Nights = make([]realdata.NightReport, len(rep.Nights))
	for i, n := range rep.Nights {
		n.Controllers = withoutP99Results(n.Controllers)
		out.Nights[i] = n
	}
	out.Aggregate = withoutP99Results(rep.Aggregate)
	return &out
}

func withoutP99Results(in []realdata.ControllerResult) []realdata.ControllerResult {
	out := make([]realdata.ControllerResult, len(in))
	for i, c := range in {
		c.Aggregate.PlanP99Micros = 0
		c.Seeds = append([]realdata.SeedResult(nil), c.Seeds...)
		for k := range c.Seeds {
			c.Seeds[k].Metrics.PlanP99Micros = 0
		}
		out[i] = c
	}
	return out
}

// ---- POST /api/run with "files" ----

// runNight is the detailed run of one imported night: the same response as a generated
// scenario, plus "source". The form's generator parameters (buses, chargers, limit,
// profile, seed...) are ignored; the swap-back parameters are used.
func (s *Server) runNight(r *http.Request, req runRequest) (any, *apiError) {
	if req.Night == "" {
		return nil, &apiError{status: http.StatusBadRequest, Field: "night",
			Message: "Noite: informe a noite (AAAA-MM-DD) que quer abrir."}
	}
	if req.Controller != noSwapController && !knownController(req.Controller) {
		return nil, &apiError{status: http.StatusBadRequest, Field: "controller", Message: "Controlador desconhecido."}
	}
	if e := swapBackError(req.SwapBackCooldownMin, req.SwapBackMinNeedKWh); e != nil {
		return nil, e
	}
	release, ok := s.acquire()
	if !ok {
		return nil, errBusy
	}
	defer release()
	d, e := loadFiles(req.Files)
	if e != nil {
		return nil, e
	}
	nights, _ := d.Nights(realdata.NightOptions{})
	var sc *sim.Scenario
	for i := range nights {
		if nights[i].Key == req.Night {
			sc = &nights[i].Scenario
			break
		}
	}
	if sc == nil {
		return nil, &apiError{status: http.StatusBadRequest, Field: "night",
			Message: "Noite não encontrada nas planilhas enviadas (use uma das noites devolvidas na importação)."}
	}
	if len(sc.Buses) > labMaxRunBuses {
		return nil, &apiError{status: http.StatusBadRequest, Field: "buses",
			Message: fmt.Sprintf("Para abrir uma execução detalhada use no máximo %d ônibus.", labMaxRunBuses)}
	}
	if len(sc.Chargers) > labMaxRunChargers {
		return nil, &apiError{status: http.StatusBadRequest, Field: "chargers",
			Message: fmt.Sprintf("Para abrir uma execução detalhada use no máximo %d carregadores.", labMaxRunChargers)}
	}

	name := req.Controller
	if name == noSwapController {
		name = "planner"
		sc.FollowSwaps = false
	}
	cfg := planner.DefaultConfig()
	cfg.SwapBackCooldownMin, cfg.SwapBackMinNeedKWh = req.SwapBackCooldownMin, req.SwapBackMinNeedKWh
	p := Params{
		Buses: len(sc.Buses), Chargers: len(sc.Chargers), LimitKW: sc.BaseLimitKW, Profile: string(sim.ProfileNone),
		Seeds: 1, FollowSwaps: sc.FollowSwaps, ReadingAgeMin: 0,
		SwapBackCooldownMin: cfg.SwapBackCooldownMin, SwapBackMinNeedKWh: cfg.SwapBackMinNeedKWh,
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	run, ctrl, err := sim.ForController(name, cfg, *sc)
	if err != nil {
		return nil, &apiError{status: http.StatusBadRequest, Field: "controller", Message: "Controlador desconhecido."}
	}
	tr := sim.NewTrace()
	m, err := sim.RunContext(ctx, run, ctrl, nil, tr)
	if err != nil {
		return nil, ctxError(err, "Tempo esgotado: use uma noite com menos ônibus ou carregadores.")
	}
	m.PlanP99Micros = 0 // wall-clock time: zeroed so the same request gives the same bytes
	resp := buildRun(p, 1, req.Controller, run, m, tr)
	resp.Source = "Dados reais, noite " + req.Night
	return resp, nil
}

func knownController(name string) bool {
	for _, n := range sim.ControllerNames {
		if n == name {
			return true
		}
	}
	return false
}
