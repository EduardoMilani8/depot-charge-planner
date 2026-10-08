package lab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"

	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

type presetDTO struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Params      Params `json:"params"`
}

type limitsDTO struct {
	MaxBuses          int     `json:"max_buses"`
	MaxChargers       int     `json:"max_chargers"`
	MaxSeeds          int     `json:"max_seeds"`
	MaxLimitKW        float64 `json:"max_limit_kw"`
	MaxReadingAgeMin  int     `json:"max_reading_age_min"`
	LabMaxWork        int     `json:"lab_max_work"`
	LabMaxRunBuses    int     `json:"lab_max_run_buses"`
	LabMaxRunChargers int     `json:"lab_max_run_chargers"`
}

type defaultsResponse struct {
	Params      Params      `json:"params"`
	Limits      limitsDTO   `json:"limits"`
	Presets     []presetDTO `json:"presets"`
	Controllers []string    `json:"controllers"`
	Profiles    []string    `json:"profiles"`
}

func presets() []presetDTO {
	d := defaultParams()
	tight, severe, aged := d, d, d
	tight.LimitKW, tight.Profile = 600, "mild"
	severe.Profile = "severe"
	aged.LimitKW, aged.Profile, aged.ReadingAgeMin = 1200, "severe", 1
	return []presetDTO{
		{"default", "Padrão", "50 ônibus, 25 carregadores, 2000 kW, sem falhas.", d},
		{"tight", "Rede apertada (600 kW)", "Pouca potência para muitos ônibus, com falhas leves.", tight},
		{"severe", "Falhas severas", "Carregadores caindo, leituras ruins e queda de rede.", severe},
		{"aged", "Leituras atrasadas", "Gateway que informa leituras com 1 minuto de idade, 1200 kW e falhas severas.", aged},
	}
}

func (s *Server) defaults(r *http.Request) (any, *apiError) {
	return defaultsResponse{
		Params: defaultParams(),
		Limits: limitsDTO{
			MaxBuses: sim.MaxBuses, MaxChargers: sim.MaxChargers, MaxSeeds: sim.MaxSeeds,
			MaxLimitKW: sim.MaxLimitKW, MaxReadingAgeMin: sim.MaxReadingAgeMin,
			LabMaxWork: labMaxWork, LabMaxRunBuses: labMaxRunBuses, LabMaxRunChargers: labMaxRunChargers,
		},
		Presets:     presets(),
		Controllers: sim.ControllerNames,
		Profiles:    []string{"none", "mild", "severe", "random"},
	}, nil
}

// ctxError maps a simulation that ended early: 504 on the deadline, 408 if the client left.
func ctxError(err error, timeoutMsg string) *apiError {
	if errors.Is(err, context.DeadlineExceeded) {
		return &apiError{status: http.StatusGatewayTimeout, Message: timeoutMsg}
	}
	return &apiError{status: http.StatusRequestTimeout, Message: "Requisição cancelada."}
}

type seedDTO struct {
	Seed    int64       `json:"seed"`
	Metrics sim.Metrics `json:"metrics"`
}

type controllerDTO struct {
	Name      string      `json:"name"`
	Aggregate sim.Metrics `json:"aggregate"`
	Seeds     []seedDTO   `json:"seeds"`
}

type compareResponse struct {
	Params      Params          `json:"params"`
	Controllers []controllerDTO `json:"controllers"`
	P99Note     string          `json:"p99_note"`
}

func (s *Server) compare(r *http.Request) (any, *apiError) {
	p := defaultParams()
	if e := decodeBody(r, &p); e != nil {
		return nil, e
	}
	if e := p.validate(); e != nil {
		return nil, e
	}
	if p.Buses*p.Seeds > labMaxWork {
		return nil, &apiError{status: http.StatusBadRequest, Field: "seeds",
			Message: "Trabalho demais para uma tela: ônibus × sementes deve ser no máximo 20000. Reduza as sementes ou os ônibus."}
	}
	release, ok := s.acquire()
	if !ok {
		return nil, errBusy
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	g, cfg := p.toSim()
	res, err := sim.Compare(ctx, g, cfg, p.Seeds, runtime.NumCPU())
	if err != nil {
		return nil, ctxError(err, "Tempo esgotado: reduza as sementes ou o número de ônibus.")
	}
	out := compareResponse{
		Params:  p,
		P99Note: "O p99 do planejador é tempo real medido com várias simulações em paralelo: use como ordem de grandeza (para um valor preciso use go run ./cmd/simrun).",
	}
	for _, c := range res {
		cd := controllerDTO{Name: c.Name, Aggregate: c.Aggregate}
		for _, sr := range c.Seeds {
			cd.Seeds = append(cd.Seeds, seedDTO{Seed: sr.Seed, Metrics: sr.Metrics})
		}
		out.Controllers = append(out.Controllers, cd)
	}
	return out, nil
}

type runRequest struct {
	Params
	Seed       int64  `json:"seed"`
	Controller string `json:"controller"`
	// Files and Night open one night of imported real data (see runNight): the form's
	// generator parameters are then ignored, and Controller may also be "planner (sem rodízio)".
	Files map[string]string `json:"files,omitempty"`
	Night string            `json:"night,omitempty"`
}

func (s *Server) run(r *http.Request) (any, *apiError) {
	req := runRequest{Params: defaultParams(), Seed: 1, Controller: "planner"}
	if e := decodeBody(r, &req); e != nil {
		return nil, e
	}
	if req.Files != nil {
		return s.runNight(r, req)
	}
	if req.Night != "" {
		return nil, &apiError{status: http.StatusBadRequest, Field: "night", Message: "A noite só vale junto com as planilhas (files)."}
	}
	p := req.Params
	if e := p.validate(); e != nil {
		return nil, e
	}
	if req.Seed < 1 || req.Seed > sim.MaxSeeds {
		return nil, &apiError{status: http.StatusBadRequest, Field: "seed",
			Message: fmt.Sprintf("Semente: informe um valor entre 1 e %d.", sim.MaxSeeds)}
	}
	known := false
	for _, n := range sim.ControllerNames {
		known = known || n == req.Controller
	}
	if !known {
		return nil, &apiError{status: http.StatusBadRequest, Field: "controller", Message: "Controlador desconhecido."}
	}
	if p.Buses > labMaxRunBuses {
		return nil, &apiError{status: http.StatusBadRequest, Field: "buses",
			Message: fmt.Sprintf("Para abrir uma execução detalhada use no máximo %d ônibus.", labMaxRunBuses)}
	}
	if p.Chargers > labMaxRunChargers {
		return nil, &apiError{status: http.StatusBadRequest, Field: "chargers",
			Message: fmt.Sprintf("Para abrir uma execução detalhada use no máximo %d carregadores.", labMaxRunChargers)}
	}
	release, ok := s.acquire()
	if !ok {
		return nil, errBusy
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	g, cfg := p.toSim()
	sc, ctrl, _ := sim.ForController(req.Controller, cfg, sim.Generate(g, req.Seed))
	tr := sim.NewTrace()
	m, err := sim.RunContext(ctx, sc, ctrl, nil, tr)
	if err != nil {
		return nil, ctxError(err, "Tempo esgotado: reduza o número de ônibus ou de carregadores.")
	}
	// plan_p99_micros is wall-clock time and is zeroed on purpose, so the same request
	// always gives the same bytes.
	m.PlanP99Micros = 0
	return buildRun(p, req.Seed, req.Controller, sc, m, tr), nil
}
