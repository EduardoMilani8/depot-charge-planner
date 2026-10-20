package lab

import (
	"errors"
	"fmt"

	"github.com/EduardoMilani8/depot-charge-planner/internal/billing"
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

	// Energy cost (all optional and omitted when unset, so the default Params serialise
	// byte for byte as before; the zero value of each is its default).
	//
	// CostAware turns on the planner's cost policy: defer charging past the peak window
	// and cap the demand at DemandTargetKW. DeferMarginMin 0 means the planner's default
	// (45); both need CostAware. The other controllers never change.
	CostAware      bool    `json:"cost_aware,omitempty"`
	DeferMarginMin int     `json:"defer_margin_min,omitempty"`
	DemandTargetKW float64 `json:"demand_target_kw,omitempty"`
	// Demand prices in R$/kW and the contract in kW (0 = none). With DemandVerde there is
	// one demand charge, at DemandOffPeakPrice: DemandPeakPrice is ignored.
	DemandPeakPrice    float64 `json:"demand_peak_price,omitempty"`
	DemandOffPeakPrice float64 `json:"demand_offpeak_price,omitempty"`
	DemandContractedKW float64 `json:"demand_contracted_kw,omitempty"`
	DemandVerde        bool    `json:"demand_verde,omitempty"`
	// ArrivalFromMin..ArrivalToMin: arrival window in minutes since the scenario start
	// (18:00, so 180 = 21:00); both 0 = the default 120..360 (20:00-24:00).
	ArrivalFromMin int `json:"arrival_from_min,omitempty"`
	ArrivalToMin   int `json:"arrival_to_min,omitempty"`
	// NightsPerMonth scales the night to a month in the bill estimate; 0 = 30.
	NightsPerMonth int `json:"nights_per_month,omitempty"`
}

// verdeNote is shown with the limits and in the responses that carry a Verde bill with a
// peak demand price.
const verdeNote = "No modo Verde há uma única cobrança de demanda, ao preço fora da ponta, sobre o maior bloco da noite: o preço de demanda na ponta é ignorado."

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
	g.ArrivalFromMin, g.ArrivalToMin = p.ArrivalFromMin, p.ArrivalToMin
	g.Demand = sim.DemandParams{PeakPrice: p.DemandPeakPrice, OffPeakPrice: p.DemandOffPeakPrice,
		ContractedKW: p.DemandContractedKW, Verde: p.DemandVerde, NightsPerMonth: p.NightsPerMonth}
	cfg := planner.DefaultConfig()
	cfg.SwapBackCooldownMin, cfg.SwapBackMinNeedKWh = p.SwapBackCooldownMin, p.SwapBackMinNeedKWh
	if p.CostAware {
		// The clock and peak window come from the scenario (sim.ScenarioConfig).
		cfg.Cost = &planner.CostPolicy{DeferMarginMin: float64(p.DeferMarginMin), DemandTargetKW: p.DemandTargetKW}
	}
	return g, cfg
}

// apiError is the JSON error body.
type apiError struct {
	status  int
	Message string `json:"error"`
	Field   string `json:"field,omitempty"`
	// File, Line and Column locate a spreadsheet error (imported real data).
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column string `json:"column,omitempty"`
}

// jsonField maps sim.FieldError names (simrun flag names) to the API's JSON keys.
var jsonField = map[string]string{
	"profile": "profile", "seeds": "seeds", "buses": "buses", "chargers": "chargers",
	"limit": "limit_kw", "reading-age": "reading_age_min",
	"swap-back-cooldown": "swap_back_cooldown_min", "swap-back-min-need": "swap_back_min_need_kwh",
	"defer-margin": "defer_margin_min", "demand-target": "demand_target_kw",
	"demand-peak-price": "demand_peak_price", "demand-offpeak-price": "demand_offpeak_price",
	"demand-contracted": "demand_contracted_kw", "arrival-from": "arrival_from_min", "arrival-to": "arrival_to_min",
	"nights-per-month": "nights_per_month",
	// Lab-only checks (not sim.FieldError names).
	"defer-margin-needs-cost-aware": "defer_margin_min", "demand-target-needs-cost-aware": "demand_target_kw",
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
	case "defer-margin":
		return fmt.Sprintf("Folga do adiamento: informe de 0 a %d minutos (0 usa o padrão, 45).", sim.MaxDeferMarginMin)
	case "demand-target":
		return fmt.Sprintf("Meta de demanda: informe de 0 a %.0f kW (0 desliga).", float64(sim.MaxLimitKW))
	case "defer-margin-needs-cost-aware":
		return "Folga do adiamento: só vale com o planejador consciente de custo ligado (cost_aware)."
	case "demand-target-needs-cost-aware":
		return "Meta de demanda: só vale com o planejador consciente de custo ligado (cost_aware)."
	case "demand-peak-price":
		return fmt.Sprintf("Preço de demanda na ponta: informe de 0 a %.0f R$/kW.", float64(sim.MaxDemandPrice))
	case "demand-offpeak-price":
		return fmt.Sprintf("Preço de demanda fora da ponta: informe de 0 a %.0f R$/kW.", float64(sim.MaxDemandPrice))
	case "demand-contracted":
		return fmt.Sprintf("Demanda contratada: informe de 0 a %.0f kW (0 = sem contrato).", float64(sim.MaxLimitKW))
	case "arrival-from":
		return fmt.Sprintf("Início das chegadas: informe de 0 a %d minutos desde as 18:00 (180 = 21:00).", sim.MaxArrivalMin-1)
	case "arrival-to":
		return fmt.Sprintf("Fim das chegadas: informe um valor maior que o início e até %d minutos desde as 18:00 (a janela deve terminar antes da primeira saída).", sim.MaxArrivalMin)
	case "nights-per-month":
		return fmt.Sprintf("Noites por mês: informe de 1 a %d (padrão %d).", sim.MaxNightsPerMonth, billing.DefaultNightsPerMonth)
	}
	return "Parâmetro inválido."
}

// validate checks the generator and planner parameters (not the lab's work limits).
func (p Params) validate() *apiError {
	// These two only configure the cost-aware planner; accepting them silently would let a
	// form look cost-aware when it is not.
	if !p.CostAware {
		if p.DeferMarginMin != 0 {
			return &apiError{status: 400, Message: ptMessage("defer-margin-needs-cost-aware"), Field: jsonField["defer-margin-needs-cost-aware"]}
		}
		if p.DemandTargetKW != 0 {
			return &apiError{status: 400, Message: ptMessage("demand-target-needs-cost-aware"), Field: jsonField["demand-target-needs-cost-aware"]}
		}
	}
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

// warnings lists what the response should say about valid parameters that do less than
// they seem to. A UI must not derive them from anything else.
func (p Params) warnings() []string {
	var w []string
	if p.DemandVerde && p.DemandPeakPrice > 0 {
		w = append(w, verdeNote)
	}
	if p.CostAware && p.DemandTargetKW > p.LimitKW {
		w = append(w, fmt.Sprintf("A meta de demanda (%.0f kW) é maior que o limite da garagem (%.0f kW): não tem efeito.", p.DemandTargetKW, p.LimitKW))
	}
	if p.CostAware && p.Chargers < p.Buses {
		w = append(w, "Com menos carregadores que ônibus o planejador não adia a carga para depois da ponta (cada ônibus precisa de uma tomada); só a meta de demanda atua.")
	}
	return w
}
