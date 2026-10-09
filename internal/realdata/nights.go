package realdata

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

const (
	// maxHorizonMin is the longest night Nights turns into a scenario; longer
	// ones are omitted with a warning (the simulator ticks once per minute).
	maxHorizonMin = 3000
	originPadMin  = 60 // the scenario starts this long before the first arrival hour
	horizonPadMin = 30 // and ends this long after the last departure

	// energyMismatch: when a night has both sessions and power readings and their
	// energies differ by more than this fraction of the larger one, the night is Partial.
	energyMismatch = 0.20
)

// Real is what the depot measured on one night (the "real" column of the
// comparison). Pointers are nil when the spreadsheets cannot tell (serialized
// as null); no field is ever NaN or Inf.
type Real struct {
	Buses         int      `json:"buses"`        // buses in the night
	WithOutcome   int      `json:"with_outcome"` // buses with soc_saida_real_pct
	Ready         int      `json:"ready"`        // of WithOutcome
	ReadyPct      float64  `json:"ready_pct"`    // 100*Ready/WithOutcome, 0 if WithOutcome==0
	ShortfallKWh  float64  `json:"shortfall_kwh"`
	EnergyKWh     *float64 `json:"energy_kwh"` // nil = unknown (no sessions and no power in the night)
	PeakKW        *float64 `json:"peak_kw"`
	PeakEstimated bool     `json:"peak_estimated"`
	CostBRL       *float64 `json:"cost_brl"` // nil = no tariff or no energy data
	CostEstimated bool     `json:"cost_estimated"`
	// Partial: energy, peak and cost come from sessions or readings that do not cover
	// every bus of the night (or the readings and the sessions disagree by more than
	// energyMismatch). The values are kept, but they are not the whole night.
	Partial bool `json:"partial"`
	// CoveredBuses: of Buses, how many have at least one session in the night (equal to
	// Buses when no gap was found, e.g. no sessoes.csv at all).
	CoveredBuses int    `json:"covered_buses"`
	PartialNote  string `json:"partial_note"` // short seal, "parcial: 2 de 3 ônibus"; "" when not Partial
}

// BusReal is the measured outcome of one bus (nil = soc_saida_real_pct missing).
type BusReal struct {
	ID       string   `json:"id"`
	FinalKWh *float64 `json:"final_kwh"`
	Ready    *bool    `json:"ready"`
}

// Night is one depot night: a simulator scenario plus what really happened.
type Night struct {
	Key      string       `json:"key"`      // "2026-03-04"
	Scenario sim.Scenario `json:"-"`        // Name "real-<Key>", Seed 1, FollowSwaps true
	Real     Real         `json:"real"`     //
	BusReal  []BusReal    `json:"bus_real"` // sorted by bus ID
}

// NightOptions tunes the generated scenarios.
type NightOptions struct {
	// SoCNoiseKWh > 0 (finite) adds a soc_noise fault on every bus ("*", whole night).
	SoCNoiseKWh float64
}

// Nights turns the dataset into one Night per night key, sorted by Key. It does
// not modify d. The warnings are about nights left out (horizon above 3000
// minutes), readings outside any night and a peak window the simulator cannot
// represent; they are separate from d.Warnings.
//
// Which data feeds each Real figure (per night, never mixing the two sources
// for one number):
//   - EnergyKWh: the sessions of the night if any, else the integral of the
//     power series, else nil.
//   - PeakKW: the power series if the night has samples (PeakEstimated=false),
//     else the sessions' average powers (PeakEstimated=true), else nil.
//   - CostBRL: needs a tariff; from the power series (measured) if the night has
//     samples, else from the sessions spread uniformly over their minutes
//     (CostEstimated=true), else nil.
func (d *Dataset) Nights(o NightOptions) ([]Night, []Warning) {
	var ws []Warning

	busesBy := map[string][]Bus{}
	for _, b := range d.Buses {
		k := NightKey(b.Arrival)
		busesBy[k] = append(busesBy[k], b)
	}
	sessBy := map[string][]Session{}
	for _, s := range d.Sessions {
		k := s.Night                 // set by Load: the night of the stay the session belongs to
		if _, ok := busesBy[k]; ok { // Load guarantees it; defensive
			sessBy[k] = append(sessBy[k], s)
		}
	}
	powBy := map[string][]PowerSample{}
	orphans := 0
	for _, p := range d.Power {
		k := NightKey(p.At)
		if _, ok := busesBy[k]; !ok {
			orphans++
			continue
		}
		powBy[k] = append(powBy[k], p)
	}
	if orphans > 0 {
		ws = append(ws, Warning{File: FilePotencia, Message: plural(orphans, "leitura", "leituras") +
			" de potência em noites sem ônibus em onibus.csv: ignorada(s)"})
	}
	g := d.Garage
	if g.HasTariff && g.PeakFromMin > g.PeakToMin {
		ws = append(ws, Warning{File: FileGaragem, Message: "a janela de ponta atravessa a meia-noite; o simulador só aceita janelas dentro do mesmo dia " +
			"e trata esse horário como fora de ponta: o custo real é calculado com a janela correta, mas o custo simulado não é comparável"})
	}

	var faults []sim.Fault
	if o.SoCNoiseKWh > 0 && !math.IsInf(o.SoCNoiseKWh, 0) {
		faults = []sim.Fault{{Kind: sim.FaultSoCNoise, Target: "*", From: 0, To: math.MaxInt32, Value: o.SoCNoiseKWh}}
	} else if o.SoCNoiseKWh != 0 {
		ws = append(ws, Warning{Message: fmt.Sprintf("ruído de SoC %g kWh inválido (use um valor finito maior que 0): ignorado", o.SoCNoiseKWh)})
	}

	chargers := make([]model.Charger, len(d.Chargers))
	for i, c := range d.Chargers {
		chargers[i] = model.Charger{ID: c.ID, MaxKW: c.MaxKW, MinKW: c.MinKW, Efficiency: c.Efficiency, Status: model.ChargerOK}
	}
	defBattery := commonChargerKW(d.Chargers)
	var tariff sim.Tariff
	if g.HasTariff {
		tariff = sim.Tariff{PeakFromMin: g.PeakFromMin, PeakToMin: g.PeakToMin, PeakPrice: g.PeakPrice, OffPeakPrice: g.OffPeakPrice}
	}
	cum := priceTable(g)

	keys := make([]string, 0, len(busesBy))
	for k := range busesBy {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	nights := make([]Night, 0, len(keys))
	for _, k := range keys {
		buses := busesBy[k]
		origin := nightOrigin(buses)

		// scenario
		horizon := 0
		specs := make([]sim.BusSpec, 0, len(buses))
		for _, b := range buses {
			dep := minutesSince(origin, b.Departure)
			last := dep
			if b.RealDeparture != nil {
				last = max(last, minutesSince(origin, *b.RealDeparture))
			}
			horizon = max(horizon, last+horizonPadMin)
			target := b.CapacityKWh * b.RequiredPct / 100
			maxBat := b.MaxBatteryKW
			if maxBat == 0 {
				maxBat = defBattery
			}
			specs = append(specs, sim.BusSpec{
				Bus: model.Bus{
					ID:            b.ID,
					CapacityKWh:   b.CapacityKWh,
					SoCKWh:        b.CapacityKWh * b.SoCArrivalPct / 100,
					SoCConfidence: 1,
					TargetKWh:     target,
					ArrivalMin:    minutesSince(origin, b.Arrival),
					DepartureMin:  dep,
					MaxBatteryKW:  maxBat,
				},
				TrueTargetKWh: target,
			})
		}
		// The simulator breaks ties by the order of Buses: fix it so the order of
		// the rows in onibus.csv never changes a result.
		sort.SliceStable(specs, func(i, j int) bool {
			a, b := specs[i].Bus, specs[j].Bus
			if a.ArrivalMin != b.ArrivalMin {
				return a.ArrivalMin < b.ArrivalMin
			}
			return a.ID < b.ID
		})
		if horizon > maxHorizonMin {
			line := buses[0].Line
			for _, b := range buses {
				line = min(line, b.Line)
			}
			ws = append(ws, Warning{File: FileOnibus, Line: line, Message: fmt.Sprintf(
				"noite %s omitida: o horizonte de %d min passa do máximo de %d min (confira as datas de saída)", k, horizon, maxHorizonMin)})
			continue
		}
		sc := sim.Scenario{
			Name:          "real-" + k,
			Seed:          1,
			StartClockMin: origin.Hour()*60 + origin.Minute(),
			Horizon:       horizon,
			BaseLimitKW:   g.LimitKW,
			Chargers:      append([]model.Charger(nil), chargers...),
			Buses:         specs,
			Faults:        append([]sim.Fault(nil), faults...),
			Tariff:        tariff,
			FollowSwaps:   true,
		}

		real, busReal, notes := measured(buses, sessBy[k], powBy[k], g, cum, d.HasSessions)
		for _, msg := range notes {
			ws = append(ws, Warning{File: FileSessoes, Message: "noite " + k + ": " + msg})
		}
		if !real.finite() {
			ws = append(ws, Warning{File: FileOnibus, Line: buses[0].Line, Message: fmt.Sprintf("noite %s omitida: o resultado real não é um número finito (confira as unidades)", k)})
			continue
		}
		nights = append(nights, Night{Key: k, Scenario: sc, Real: real, BusReal: busReal})
	}
	return nights, ws
}

// nightOrigin is the earliest arrival rounded down to the hour, minus one hour.
func nightOrigin(buses []Bus) time.Time {
	first := buses[0].Arrival
	for _, b := range buses {
		if b.Arrival.Before(first) {
			first = b.Arrival
		}
	}
	h := time.Date(first.Year(), first.Month(), first.Day(), first.Hour(), 0, 0, 0, first.Location())
	return h.Add(-originPadMin * time.Minute)
}

func minutesSince(origin, t time.Time) int { return int(t.Sub(origin) / time.Minute) }

// commonChargerKW is the most common potencia_max_kw among the chargers (ties:
// the larger), the default potencia_max_bateria_kw.
func commonChargerKW(cs []Charger) float64 {
	count := map[float64]int{}
	best, bestN := 0.0, 0
	for _, c := range cs {
		count[c.MaxKW]++
	}
	for kw, n := range count {
		if n > bestN || (n == bestN && kw > best) {
			best, bestN = kw, n
		}
	}
	return best
}

// priceAt is the tariff price at a minute of the day. Unlike sim.Tariff.PriceAt
// it also handles a peak window that crosses midnight.
func priceAt(g Garage, clockMin int) float64 {
	if !g.HasTariff {
		return 0
	}
	m := ((clockMin % 1440) + 1440) % 1440
	peak := m >= g.PeakFromMin && m < g.PeakToMin
	if g.PeakFromMin > g.PeakToMin {
		peak = m >= g.PeakFromMin || m < g.PeakToMin
	}
	if peak {
		return g.PeakPrice
	}
	return g.OffPeakPrice
}

// priceTable[m] = sum of the price of minutes 0..m-1 of a day.
func priceTable(g Garage) [1441]float64 {
	var cum [1441]float64
	for m := 0; m < 1440; m++ {
		cum[m+1] = cum[m] + priceAt(g, m)
	}
	return cum
}

// priceSpan is the sum of the per-minute price over n minutes starting at the
// given minute of the day.
func priceSpan(cum *[1441]float64, startClock, n int) float64 {
	total := float64(n/1440) * cum[1440]
	rem := n % 1440
	if startClock+rem <= 1440 {
		return total + cum[startClock+rem] - cum[startClock]
	}
	return total + cum[1440] - cum[startClock] + cum[startClock+rem-1440]
}

func clockOf(t time.Time) int { return t.Hour()*60 + t.Minute() }

// measured computes the Real figures of one night and the per-bus outcomes. The
// strings are warnings about how much of the night the figures cover (Partial).
// hasSessions says whether sessoes.csv exists at all: a night without sessions in a
// file that has them for other nights is a gap, one in a dataset without the file is not.
func measured(buses []Bus, sess []Session, pow []PowerSample, g Garage, cum [1441]float64, hasSessions bool) (Real, []BusReal, []string) {
	r := Real{Buses: len(buses), CoveredBuses: len(buses)}
	busReal := make([]BusReal, 0, len(buses))
	for _, b := range buses {
		br := BusReal{ID: b.ID}
		if b.RealSoCPct != nil {
			final := b.CapacityKWh * *b.RealSoCPct / 100
			target := math.Min(b.CapacityKWh*b.RequiredPct/100, b.CapacityKWh)
			ready := final >= target-1e-6 // same rule as sim.World.depart
			br.FinalKWh, br.Ready = &final, &ready
			r.WithOutcome++
			if ready {
				r.Ready++
			} else {
				r.ShortfallKWh += target - final
			}
		}
		busReal = append(busReal, br)
	}
	sort.SliceStable(busReal, func(i, j int) bool { return busReal[i].ID < busReal[j].ID })
	if r.WithOutcome > 0 {
		r.ReadyPct = 100 * float64(r.Ready) / float64(r.WithOutcome)
	}

	var sampled sampleStats
	if len(pow) > 0 {
		sampled = integrate(pow, g)
	}
	switch {
	case len(sess) > 0:
		e := 0.0
		for _, s := range sess {
			e += s.EnergyKWh
		}
		r.EnergyKWh = &e
	case len(pow) > 0:
		e := sampled.energy
		r.EnergyKWh = &e
	}
	switch {
	case len(pow) > 0:
		p := sampled.peak
		r.PeakKW = &p
	case len(sess) > 0:
		p := sessionPeak(sess)
		r.PeakKW, r.PeakEstimated = &p, true
	}
	if g.HasTariff {
		switch {
		case len(pow) > 0:
			c := sampled.cost
			r.CostBRL = &c
		case len(sess) > 0:
			c := 0.0
			for _, s := range sess {
				n := max(1, minutesSince(s.Start, s.End))
				c += s.EnergyKWh * priceSpan(&cum, clockOf(s.Start), n) / float64(n)
			}
			r.CostBRL, r.CostEstimated = &c, true
		}
	}
	return r, busReal, markPartial(&r, buses, sess, sampled, len(pow) > 0, hasSessions)
}

// markPartial decides whether the energy, peak and cost of a night cover all of it:
//   - sessions exist for the night but some buses have none (CoveredBuses < Buses), or
//     sessoes.csv exists but has nothing for this night while the figures come from readings;
//   - sessions and readings both exist and their energies differ by more than energyMismatch.
//
// It returns the warnings to show; nothing is changed when there is no energy figure at all.
func markPartial(r *Real, buses []Bus, sess []Session, sampled sampleStats, hasPower, hasSessions bool) []string {
	if r.EnergyKWh == nil && r.PeakKW == nil && r.CostBRL == nil {
		return nil
	}
	var notes, parts []string
	if len(sess) > 0 {
		have := map[string]bool{}
		for _, s := range sess {
			have[s.BusID] = true
		}
		r.CoveredBuses = 0
		for _, b := range buses {
			if have[b.ID] {
				r.CoveredBuses++
			}
		}
		if missing := r.Buses - r.CoveredBuses; missing > 0 {
			notes = append(notes, fmt.Sprintf("%d de %d ônibus sem sessão: energia, pico e custo reais cobrem só os demais", missing, r.Buses))
			parts = append(parts, fmt.Sprintf("%d de %d ônibus", r.CoveredBuses, r.Buses))
		}
	} else if hasSessions {
		r.CoveredBuses = 0
		notes = append(notes, "nenhuma sessão em sessoes.csv nesta noite: energia, pico e custo reais vêm só das leituras de potência e podem não cobrir todos os ônibus")
		parts = append(parts, fmt.Sprintf("0 de %d ônibus", r.Buses))
	}
	if len(sess) > 0 && hasPower {
		se := 0.0
		for _, s := range sess {
			se += s.EnergyKWh
		}
		if hi := math.Max(se, sampled.energy); hi > 0 && math.Abs(se-sampled.energy) > energyMismatch*hi {
			notes = append(notes, fmt.Sprintf("a energia das sessões (%.0f kWh) e a das leituras de potência (%.0f kWh) diferem em mais de %d%%: sessões ou leituras podem não cobrir a noite toda",
				se, sampled.energy, int(energyMismatch*100)))
			parts = append(parts, "sessões e leituras de potência divergem")
		}
	}
	if len(notes) > 0 {
		r.Partial = true
		r.PartialNote = "parcial: " + strings.Join(parts, "; ")
	}
	return notes
}

func (r Real) finite() bool {
	ok := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	for _, p := range []*float64{r.EnergyKWh, r.PeakKW, r.CostBRL} {
		if p != nil && !ok(*p) {
			return false
		}
	}
	return ok(r.ReadyPct) && ok(r.ShortfallKWh)
}

type sampleStats struct{ energy, peak, cost float64 }

// integrate walks the night's samples in time order (Dataset.Power is only
// sorted per charger, so it sorts a copy). Each reading holds until the next
// reading of the same charger; the last one holds for 0 minutes. The cost of an
// interval uses the price at its start.
func integrate(pow []PowerSample, g Garage) sampleStats {
	sorted := append([]PowerSample(nil), pow...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	type held struct {
		at time.Time
		kw float64
	}
	cur := map[string]held{}
	var st sampleStats
	total := 0.0
	for i := 0; i < len(sorted); {
		at := sorted[i].At
		j := i
		for ; j < len(sorted) && sorted[j].At.Equal(at); j++ {
			s := sorted[j]
			if h, ok := cur[s.ChargerID]; ok {
				e := h.kw * at.Sub(h.at).Hours()
				st.energy += e
				st.cost += e * priceAt(g, clockOf(h.at))
				total -= h.kw
			}
			total += s.KW
			cur[s.ChargerID] = held{at, s.KW}
		}
		st.peak = math.Max(st.peak, total) // after every reading of the instant
		i = j
	}
	return st
}

// sessionPeak is the largest sum of the average powers (kWh / hours) of the
// sessions active together, evaluated where a session starts.
func sessionPeak(sess []Session) float64 {
	type event struct {
		at    time.Time
		delta float64
		start bool
	}
	ev := make([]event, 0, 2*len(sess))
	for _, s := range sess {
		kw := s.EnergyKWh / s.End.Sub(s.Start).Hours()
		ev = append(ev, event{s.Start, kw, true}, event{s.End, -kw, false})
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].at.Before(ev[j].at) })
	peak, total := 0.0, 0.0
	for i := 0; i < len(ev); {
		j, started := i, false
		for ; j < len(ev) && ev[j].at.Equal(ev[i].at); j++ {
			total += ev[j].delta
			started = started || ev[j].start
		}
		if started {
			peak = math.Max(peak, total)
		}
		i = j
	}
	return peak
}
