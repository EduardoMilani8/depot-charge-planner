package lab

import (
	"errors"
	"fmt"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

// Work limits of the lab (on top of sim.Max*): a request must stay interactive.
const (
	labMaxWork        = 20000 // buses x seeds in /api/compare
	labMaxRunBuses    = 500   // buses in /api/run (the response carries every series)
	labMaxRunChargers = 500   // chargers in /api/run
)

// Params is the scenario form. Missing JSON fields keep the default.
type Params struct {
	Buses               int     `json:"buses"`
	Chargers            int     `json:"chargers"`
	LimitKW             float64 `json:"limit_kw"`
	Profile             string  `json:"profile"`
	Seeds               int     `json:"seeds"`
	FollowSwaps         bool    `json:"follow_swaps"`
	ReadingAgeMin       int     `json:"reading_age_min"`
	SwapBackCooldownMin int     `json:"swap_back_cooldown_min"`
	SwapBackMinNeedKWh  float64 `json:"swap_back_min_need_kwh"`
}

func defaultParams() Params {
	g, c := sim.DefaultGenParams(), planner.DefaultConfig()
	return Params{
		Buses: g.NumBuses, Chargers: g.NumChargers, LimitKW: g.LimitKW, Profile: string(g.Profile),
		Seeds: 20, FollowSwaps: g.FollowSwaps, ReadingAgeMin: g.ReadingAgeMin,
		SwapBackCooldownMin: c.SwapBackCooldownMin, SwapBackMinNeedKWh: c.SwapBackMinNeedKWh,
	}
}

func (p Params) toSim() (sim.GenParams, planner.Config) {
	g := sim.DefaultGenParams()
	g.NumBuses, g.NumChargers, g.LimitKW = p.Buses, p.Chargers, p.LimitKW
	g.Profile = sim.FaultProfile(p.Profile)
	g.FollowSwaps, g.ReadingAgeMin = p.FollowSwaps, p.ReadingAgeMin
	cfg := planner.DefaultConfig()
	cfg.SwapBackCooldownMin, cfg.SwapBackMinNeedKWh = p.SwapBackCooldownMin, p.SwapBackMinNeedKWh
	return g, cfg
}

// apiError is the JSON error body.
type apiError struct {
	status  int
	Message string `json:"error"`
	Field   string `json:"field,omitempty"`
}

// jsonField maps sim.FieldError names (simrun flag names) to the API's JSON keys.
var jsonField = map[string]string{
	"profile": "profile", "seeds": "seeds", "buses": "buses", "chargers": "chargers",
	"limit": "limit_kw", "reading-age": "reading_age_min",
	"swap-back-cooldown": "swap_back_cooldown_min", "swap-back-min-need": "swap_back_min_need_kwh",
}

func ptMessage(field string) string {
	switch field {
	case "profile":
		return "Perfil de falhas inválido: use none, mild, severe ou random."
	case "seeds":
		return fmt.Sprintf("Sementes: informe um valor entre 1 e %d.", sim.MaxSeeds)
	case "buses":
		return fmt.Sprintf("Ônibus: informe um valor entre 1 e %d.", sim.MaxBuses)
	case "chargers":
		return fmt.Sprintf("Carregadores: informe um valor entre 1 e %d.", sim.MaxChargers)
	case "limit":
		return fmt.Sprintf("Limite da garagem: informe um valor maior que 0 e até %.0f kW.", float64(sim.MaxLimitKW))
	case "reading-age":
		return fmt.Sprintf("Idade da leitura: informe de 0 a %d minutos.", sim.MaxReadingAgeMin)
	case "swap-back-cooldown":
		return "Espera para voltar a um carregador: informe 0 ou mais minutos."
	case "swap-back-min-need":
		return fmt.Sprintf("Necessidade mínima para voltar: informe de 0 a %.0f kWh.", float64(sim.MaxLimitKW))
	}
	return "Parâmetro inválido."
}

// validate checks the generator and planner parameters (not the lab's work limits).
func (p Params) validate() *apiError {
	g, cfg := p.toSim()
	err := sim.ValidateParams(g, cfg, p.Seeds)
	if err == nil {
		return nil
	}
	var fe *sim.FieldError
	if !errors.As(err, &fe) {
		return &apiError{status: 400, Message: "Parâmetros inválidos."}
	}
	return &apiError{status: 400, Message: ptMessage(fe.Field), Field: jsonField[fe.Field]}
}
