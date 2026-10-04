package sim

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// This file mirrors the planner and model types for the decision log, so that
// non-finite floats and layer names survive JSON. TestDecisionLogRoundTripsEveryField
// fails if a field is added to those types and not here.

// logFloat is a float64 that JSON can always carry: NaN and ±Inf become the strings
// "NaN", "+Inf" and "-Inf"; finite values are plain numbers (shortest exact form).
type logFloat float64

func (f logFloat) MarshalJSON() ([]byte, error) {
	x := float64(f)
	switch {
	case math.IsNaN(x):
		return []byte(`"NaN"`), nil
	case math.IsInf(x, 1):
		return []byte(`"+Inf"`), nil
	case math.IsInf(x, -1):
		return []byte(`"-Inf"`), nil
	}
	return strconv.AppendFloat(nil, x, 'g', -1, 64), nil
}

func (f *logFloat) UnmarshalJSON(b []byte) error {
	var s string
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		switch s {
		case "NaN":
			*f = logFloat(math.NaN())
		case "+Inf", "Inf":
			*f = logFloat(math.Inf(1))
		case "-Inf":
			*f = logFloat(math.Inf(-1))
		default:
			return fmt.Errorf("número inválido %q", s)
		}
		return nil
	}
	var x float64
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	*f = logFloat(x)
	return nil
}

// logLayer writes planner.Layer by name and reads either the name or the number.
type logLayer planner.Layer

func (l logLayer) MarshalJSON() ([]byte, error) {
	switch planner.Layer(l) {
	case planner.LayerNormal, planner.LayerLastValid, planner.LayerSafe:
		return json.Marshal(planner.Layer(l).String())
	}
	return json.Marshal(int(l))
}

func (l *logLayer) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		for _, x := range []planner.Layer{planner.LayerNormal, planner.LayerLastValid, planner.LayerSafe} {
			if x.String() == s {
				*l = logLayer(x)
				return nil
			}
		}
		return fmt.Errorf("camada desconhecida %q", s)
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*l = logLayer(n)
	return nil
}

type logHeader struct {
	Format  string    `json:"format"`
	Version int       `json:"version"`
	Config  logConfig `json:"config"`
}

type logRecord struct {
	Minute int      `json:"minute"`
	Input  logInput `json:"input"`
	Plan   logPlan  `json:"plan"`
}

type logConfig struct {
	MarginKWh             logFloat
	StaleAfterMin         int
	MinConfidence         logFloat
	UnreliablePenaltyFrac logFloat
	MaxUnreliableFrac     logFloat
	SurplusLaxityMin      logFloat
	SwapUrgentLaxityMin   logFloat
	SwapMoveMin           int
	SwapBackCooldownMin   int      // absent in older logs: 0 (anti-thrash off)
	SwapBackMinNeedKWh    logFloat // absent in older logs: 0
	LastPlanTTLMin        int
	Timeout               time.Duration
}

func toLogConfig(c planner.Config) logConfig {
	return logConfig{
		MarginKWh: logFloat(c.MarginKWh), StaleAfterMin: c.StaleAfterMin, MinConfidence: logFloat(c.MinConfidence),
		UnreliablePenaltyFrac: logFloat(c.UnreliablePenaltyFrac), MaxUnreliableFrac: logFloat(c.MaxUnreliableFrac),
		SurplusLaxityMin: logFloat(c.SurplusLaxityMin), SwapUrgentLaxityMin: logFloat(c.SwapUrgentLaxityMin),
		SwapMoveMin: c.SwapMoveMin, SwapBackCooldownMin: c.SwapBackCooldownMin, SwapBackMinNeedKWh: logFloat(c.SwapBackMinNeedKWh),
		LastPlanTTLMin: c.LastPlanTTLMin, Timeout: c.Timeout,
	}
}

func (c logConfig) toConfig() planner.Config {
	return planner.Config{
		MarginKWh: float64(c.MarginKWh), StaleAfterMin: c.StaleAfterMin, MinConfidence: float64(c.MinConfidence),
		UnreliablePenaltyFrac: float64(c.UnreliablePenaltyFrac), MaxUnreliableFrac: float64(c.MaxUnreliableFrac),
		SurplusLaxityMin: float64(c.SurplusLaxityMin), SwapUrgentLaxityMin: float64(c.SwapUrgentLaxityMin),
		SwapMoveMin: c.SwapMoveMin, SwapBackCooldownMin: c.SwapBackCooldownMin, SwapBackMinNeedKWh: float64(c.SwapBackMinNeedKWh),
		LastPlanTTLMin: c.LastPlanTTLMin, Timeout: c.Timeout,
	}
}

type logSite struct {
	LimitKW logFloat
	StepMin int
}

type logCharger struct {
	ID              string
	MaxKW           logFloat
	MinKW           logFloat
	Efficiency      logFloat
	Status          model.ChargerStatus
	LastCommandedKW logFloat
}

type logBus struct {
	ID            string
	CapacityKWh   logFloat
	SoCKWh        logFloat
	SoCAgeMin     int
	SoCConfidence logFloat
	TargetKWh     logFloat
	ArrivalMin    int
	DepartureMin  int
	MaxBatteryKW  logFloat
	ChargerID     string
}

type logInput struct {
	Now      int
	Site     logSite
	Chargers []logCharger
	Buses    []logBus
}

func toLogInput(in planner.Input) logInput {
	out := logInput{Now: in.Now, Site: logSite{LimitKW: logFloat(in.Site.LimitKW), StepMin: in.Site.StepMin}}
	if in.Chargers != nil {
		out.Chargers = make([]logCharger, len(in.Chargers))
	}
	for i, c := range in.Chargers {
		out.Chargers[i] = logCharger{ID: c.ID, MaxKW: logFloat(c.MaxKW), MinKW: logFloat(c.MinKW),
			Efficiency: logFloat(c.Efficiency), Status: c.Status, LastCommandedKW: logFloat(c.LastCommandedKW)}
	}
	if in.Buses != nil {
		out.Buses = make([]logBus, len(in.Buses))
	}
	for i, b := range in.Buses {
		out.Buses[i] = logBus{ID: b.ID, CapacityKWh: logFloat(b.CapacityKWh), SoCKWh: logFloat(b.SoCKWh),
			SoCAgeMin: b.SoCAgeMin, SoCConfidence: logFloat(b.SoCConfidence), TargetKWh: logFloat(b.TargetKWh),
			ArrivalMin: b.ArrivalMin, DepartureMin: b.DepartureMin, MaxBatteryKW: logFloat(b.MaxBatteryKW), ChargerID: b.ChargerID}
	}
	return out
}

func (l logInput) toInput() planner.Input {
	in := planner.Input{Now: l.Now, Site: model.Site{LimitKW: float64(l.Site.LimitKW), StepMin: l.Site.StepMin}}
	if l.Chargers != nil {
		in.Chargers = make([]model.Charger, len(l.Chargers))
	}
	for i, c := range l.Chargers {
		in.Chargers[i] = model.Charger{ID: c.ID, MaxKW: float64(c.MaxKW), MinKW: float64(c.MinKW),
			Efficiency: float64(c.Efficiency), Status: c.Status, LastCommandedKW: float64(c.LastCommandedKW)}
	}
	if l.Buses != nil {
		in.Buses = make([]model.Bus, len(l.Buses))
	}
	for i, b := range l.Buses {
		in.Buses[i] = model.Bus{ID: b.ID, CapacityKWh: float64(b.CapacityKWh), SoCKWh: float64(b.SoCKWh),
			SoCAgeMin: b.SoCAgeMin, SoCConfidence: float64(b.SoCConfidence), TargetKWh: float64(b.TargetKWh),
			ArrivalMin: b.ArrivalMin, DepartureMin: b.DepartureMin, MaxBatteryKW: float64(b.MaxBatteryKW), ChargerID: b.ChargerID}
	}
	return in
}

type logSetpoint struct {
	ChargerID string
	KW        logFloat
}

type logBusStatus struct {
	BusID           string
	Assessed        bool
	WillReachTarget bool
	ShortfallKWh    logFloat
	LaxityMin       logFloat
	Reason          string
}

type logPlan struct {
	Layer     logLayer
	Setpoints []logSetpoint
	Buses     []logBusStatus
	Swaps     []planner.Swap
	Notes     []string
}

func toLogPlan(p planner.Plan) logPlan {
	out := logPlan{Layer: logLayer(p.Layer), Swaps: p.Swaps, Notes: p.Notes}
	if p.Setpoints != nil {
		out.Setpoints = make([]logSetpoint, len(p.Setpoints))
	}
	for i, s := range p.Setpoints {
		out.Setpoints[i] = logSetpoint{ChargerID: s.ChargerID, KW: logFloat(s.KW)}
	}
	if p.Buses != nil {
		out.Buses = make([]logBusStatus, len(p.Buses))
	}
	for i, b := range p.Buses {
		out.Buses[i] = logBusStatus{BusID: b.BusID, Assessed: b.Assessed, WillReachTarget: b.WillReachTarget,
			ShortfallKWh: logFloat(b.ShortfallKWh), LaxityMin: logFloat(b.LaxityMin), Reason: b.Reason}
	}
	return out
}

func (l logPlan) toPlan() planner.Plan {
	p := planner.Plan{Layer: planner.Layer(l.Layer), Swaps: l.Swaps, Notes: l.Notes}
	if l.Setpoints != nil {
		p.Setpoints = make([]planner.Setpoint, len(l.Setpoints))
	}
	for i, s := range l.Setpoints {
		p.Setpoints[i] = planner.Setpoint{ChargerID: s.ChargerID, KW: float64(s.KW)}
	}
	if l.Buses != nil {
		p.Buses = make([]planner.BusStatus, len(l.Buses))
	}
	for i, b := range l.Buses {
		p.Buses[i] = planner.BusStatus{BusID: b.BusID, Assessed: b.Assessed, WillReachTarget: b.WillReachTarget,
			ShortfallKWh: float64(b.ShortfallKWh), LaxityMin: float64(b.LaxityMin), Reason: b.Reason}
	}
	return p
}
