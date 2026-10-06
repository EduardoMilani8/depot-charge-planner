package lab

import (
	"math"
	"regexp"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

type chargerDTO struct {
	ID    string  `json:"id"`
	MaxKW float64 `json:"max_kw"`
	MinKW float64 `json:"min_kw"`
}

type faultDTO struct {
	Kind   string  `json:"kind"`
	Target string  `json:"target"`
	From   int     `json:"from"`
	To     int     `json:"to"`
	Value  float64 `json:"value"`
}

type scenarioDTO struct {
	HorizonMin    int          `json:"horizon_min"`
	StartClockMin int          `json:"start_clock_min"`
	BaseLimitKW   float64      `json:"base_limit_kw"`
	Chargers      []chargerDTO `json:"chargers"`
	Faults        []faultDTO   `json:"faults"`
}

type chargerSeriesDTO struct {
	ID          string `json:"id"`
	Status      []int  `json:"status"`
	CommandedKW Series `json:"commanded_kw"`
	PhysicalKW  Series `json:"physical_kw"`
}

type busSeriesDTO struct {
	ID          string `json:"id"`
	State       []int  `json:"state"`
	TrueSoCKWh  Series `json:"true_soc_kwh"`
	ObservedKWh Series `json:"observed_kwh"`
	Charger     []int  `json:"charger"` // index into scenario.chargers, -1 = none
}

type seriesDTO struct {
	LimitKW     Series             `json:"limit_kw"`
	CommandedKW Series             `json:"commanded_kw"`
	PhysicalKW  Series             `json:"physical_kw"`
	Layer       []string           `json:"layer"`
	Chargers    []chargerSeriesDTO `json:"chargers"`
	Buses       []busSeriesDTO     `json:"buses"`
}

type busStatusDTO struct {
	Bus          string  `json:"b"`
	WillReach    bool    `json:"ok"`
	Assessed     bool    `json:"as"`
	ShortfallKWh float64 `json:"sf,omitempty"` // absent = 0
	Reason       int     `json:"r"`            // index into reasons, -1 = none
}

type swapDTO struct {
	Charger string `json:"charger"`
	Out     string `json:"out"`
	In      string `json:"in"`
	Reason  string `json:"reason"`
}

type decisionDTO struct {
	Minute int            `json:"minute"`
	Layer  string         `json:"layer"`
	Buses  []busStatusDTO `json:"buses"`
	Swaps  []swapDTO      `json:"swaps"`
	Notes  []string       `json:"notes"` // only the first occurrence of each text (see buildRun)
}

type outcomeDTO struct {
	ID                string  `json:"id"`
	Arrival           int     `json:"arrival"`
	Departure         int     `json:"departure"`
	CapacityKWh       float64 `json:"capacity_kwh"`
	InitialSoCKWh     float64 `json:"initial_soc_kwh"`
	ForecastTargetKWh float64 `json:"forecast_target_kwh"`
	TrueTargetKWh     float64 `json:"true_target_kwh"`
	FinalSoCKWh       float64 `json:"final_soc_kwh"`
	Departed          bool    `json:"departed"`
	Ready             bool    `json:"ready"`
	ShortfallKWh      float64 `json:"shortfall_kwh"`
}

type runResponse struct {
	Params     Params        `json:"params"`
	Seed       int64         `json:"seed"`
	Controller string        `json:"controller"`
	Metrics    sim.Metrics   `json:"metrics"`
	Scenario   scenarioDTO   `json:"scenario"`
	Series     seriesDTO     `json:"series"`
	Reasons    []string      `json:"reasons"`
	Decisions  []decisionDTO `json:"decisions"`
	Outcomes   []outcomeDTO  `json:"outcomes"`
}

// numberRE matches standalone numbers (digits inside IDs such as "B004" survive).
var numberRE = regexp.MustCompile(`\b[0-9]+(?:[.,][0-9]+)?\b`)

func sortChargerDTOs(cs []chargerDTO) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].ID < cs[j].ID })
}

// finite keeps NaN/Inf (which JSON cannot carry) out of the response.
func finite(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// decisionToDTO converts one decision; reasonIdx interns reason texts.
func decisionToDTO(d sim.Decision, reasonIdx func(string) int) decisionDTO {
	// d.Setpoints is not copied: series.chargers[].commanded_kw already carries what was
	// commanded every minute, and repeating it here made the response too large.
	out := decisionDTO{Minute: d.Minute, Layer: d.Layer, Notes: d.Notes,
		Buses: []busStatusDTO{}, Swaps: []swapDTO{}}
	if out.Notes == nil {
		out.Notes = []string{}
	}
	for _, b := range d.Buses {
		out.Buses = append(out.Buses, busStatusDTO{
			Bus: b.BusID, WillReach: b.WillReachTarget, Assessed: b.Assessed,
			ShortfallKWh: math.Round(finite(b.ShortfallKWh)*10) / 10, Reason: reasonIdx(b.Reason),
		})
	}
	for _, s := range d.Swaps {
		out.Swaps = append(out.Swaps, swapDTO{Charger: s.ChargerID, Out: s.OutBusID, In: s.InBusID, Reason: s.Reason})
	}
	return out
}

// buildRun converts a traced run into the /api/run response.
func buildRun(p Params, seed int64, controller string, sc sim.Scenario, m sim.Metrics, tr *sim.Trace) runResponse {
	resp := runResponse{Params: p, Seed: seed, Controller: controller, Metrics: m}
	resp.Scenario = scenarioDTO{HorizonMin: sc.Horizon, StartClockMin: sc.StartClockMin, BaseLimitKW: sc.BaseLimitKW,
		Chargers: []chargerDTO{}, Faults: []faultDTO{}}
	chargerIdx := map[string]int{}
	for i, c := range tr.Chargers {
		chargerIdx[c.ID] = i
	}
	for _, c := range sc.Chargers {
		resp.Scenario.Chargers = append(resp.Scenario.Chargers, chargerDTO{ID: c.ID, MaxKW: c.MaxKW, MinKW: c.MinKW})
	}
	sortChargerDTOs(resp.Scenario.Chargers)
	for _, f := range sc.Faults {
		to := f.To
		if to > sc.Horizon {
			to = sc.Horizon
		}
		resp.Scenario.Faults = append(resp.Scenario.Faults, faultDTO{Kind: string(f.Kind), Target: f.Target, From: f.From, To: to, Value: finite(f.Value)})
	}
	resp.Series = seriesDTO{
		LimitKW: tr.Limit, CommandedKW: tr.Commanded, PhysicalKW: tr.Physical, Layer: tr.Layer,
		Chargers: []chargerSeriesDTO{}, Buses: []busSeriesDTO{},
	}
	for _, c := range tr.Chargers {
		resp.Series.Chargers = append(resp.Series.Chargers, chargerSeriesDTO{ID: c.ID, Status: c.Status, CommandedKW: c.CommandedKW, PhysicalKW: c.PhysicalKW})
	}
	for _, b := range tr.Buses {
		idx := make([]int, len(b.ChargerID))
		for i, id := range b.ChargerID {
			if j, ok := chargerIdx[id]; ok && id != "" {
				idx[i] = j
			} else {
				idx[i] = -1
			}
		}
		resp.Series.Buses = append(resp.Series.Buses, busSeriesDTO{ID: b.ID, State: b.State, TrueSoCKWh: b.TrueSoC, ObservedKWh: b.Observed, Charger: idx})
	}

	resp.Reasons = []string{}
	interned := map[string]int{}
	reasonIdx := func(s string) int {
		if s == "" {
			return -1
		}
		if i, ok := interned[s]; ok {
			return i
		}
		interned[s] = len(resp.Reasons)
		resp.Reasons = append(resp.Reasons, s)
		return interned[s]
	}
	resp.Decisions = []decisionDTO{}
	seenNote := map[string]bool{}
	for _, d := range tr.Decisions {
		dto := decisionToDTO(d, reasonIdx)
		// A note such as "rodízio não recomendado: ônibus B006 cedeu o carregador há 3 min"
		// repeats every minute with only its numbers changed: keep the first occurrence of
		// each text (numbers masked) and drop the repeats, so the response stays small.
		kept := []string{}
		for _, n := range dto.Notes {
			if k := numberRE.ReplaceAllString(n, "#"); !seenNote[k] {
				seenNote[k] = true
				kept = append(kept, n)
			}
		}
		dto.Notes = kept
		resp.Decisions = append(resp.Decisions, dto)
	}
	resp.Outcomes = []outcomeDTO{}
	for _, o := range tr.Outcomes {
		resp.Outcomes = append(resp.Outcomes, outcomeDTO{
			ID: o.ID, Arrival: o.Arrival, Departure: o.Departure, CapacityKWh: o.CapacityKWh,
			InitialSoCKWh: math.Round(o.InitialSoCKWh*10) / 10, ForecastTargetKWh: math.Round(o.ForecastTargetKWh*10) / 10,
			TrueTargetKWh: math.Round(o.TrueTargetKWh*10) / 10, FinalSoCKWh: math.Round(o.FinalSoCKWh*10) / 10,
			Departed: o.Departed, Ready: o.Ready, ShortfallKWh: math.Round(o.ShortfallKWh*10) / 10,
		})
	}
	return resp
}
