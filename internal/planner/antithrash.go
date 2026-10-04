package planner

import (
	"fmt"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

// swapMemory is the Planner's record of the buses it told to give way, used to stop
// swap thrash under noisy SoC readings. A full bus gives its charger away; while it
// waits, noise makes single readings look short of target, so a later snapshot would
// swap it back (and then out again): measured, over 1000 moves per night at 2000 kW
// with the noise of the mild/severe profiles. PlanNormal sees one snapshot and cannot
// tell that apart from a bus that really needs charge; the Planner sees the sequence.
//
// A bus that waits unplugged does not charge or drive, so its true SoC is constant
// and the mean of its readings since it was unplugged is a much less noisy estimate
// than the latest reading. A swap that brings such a bus back is kept only if that
// mean says it needs more than cfg.SwapBackMinNeedKWh (and cfg.SwapBackCooldownMin
// minutes have passed). The memory only depends on the inputs the Planner was given,
// so Plan stays deterministic for the same sequence of inputs.
type swapMemory struct {
	gaveWay map[string]*gaveWay // bus ID -> state since it was last told to give way
}

// gaveWay is one bus told to give way.
type gaveWay struct {
	at    model.Minute
	from  string  // the charger it was told to give away
	left  bool    // seen off that charger since (operators may take a while)
	sum   float64 // sum of the counted readings since it left
	count int
	// lastTS is the timestamp (minute taken: Now - SoCAgeMin) of the last counted
	// reading; it starts at the minute the bus was told to give way.
	lastTS model.Minute
}

// antiThrash reports whether the swap-back rule is on: either knob non-zero and both
// valid. An invalid knob (negative cooldown, or a negative, NaN or infinite need)
// turns the rule off, so a bad config degrades to the old behaviour (swaps back on a
// single reading) and never into a silent permanent ban.
func (c Config) antiThrash() bool {
	if c.SwapBackCooldownMin < 0 || !finite(c.SwapBackMinNeedKWh) || c.SwapBackMinNeedKWh < 0 {
		return false
	}
	return c.SwapBackCooldownMin > 0 || c.SwapBackMinNeedKWh > 0
}

// observe updates the memory with the current input. A bus is forgotten when it is no
// longer present or, after leaving the charger it gave away, is on a healthy charger
// again (it charges, so its SoC is no longer constant). While it waits, a reading is
// added to its mean only if it is reliable (isReliable: valid, confident, at most
// StaleAfterMin old) AND its timestamp (Now - SoCAgeMin) is newer than the last
// counted one (initially the minute it was told to give way). So the same reading is
// counted once, whether Plan runs twice in a minute, the gateway reports every reading
// a few minutes old, or the reading is frozen (same timestamp, growing age). Until the
// bus is seen off its charger, nothing is added.
func (m *swapMemory) observe(cfg Config, in Input) {
	if len(m.gaveWay) == 0 {
		return
	}
	chargers := chargerMap(in)
	present := map[string]model.Bus{}
	for _, b := range presentBuses(in) {
		present[b.ID] = b
	}
	for id, g := range m.gaveWay {
		b, ok := present[id]
		if !ok || g.at > in.Now { // gone, or the clock went back: start over
			delete(m.gaveWay, id)
			continue
		}
		if !g.left {
			if b.ChargerID == g.from {
				continue
			}
			g.left = true
		}
		if c, plugged := chargers[b.ChargerID]; plugged && c.Healthy() {
			delete(m.gaveWay, id)
			continue
		}
		if ts := in.Now - max(b.SoCAgeMin, 0); isReliable(cfg, b) && ts > g.lastTS {
			g.sum += b.SoCKWh
			g.count++
			g.lastTS = ts
		}
	}
}

// filter drops the swaps that would bring back a bus told to give way that is still
// waiting, unless at least cfg.SwapBackCooldownMin minutes passed and its need by the
// mean of its readings since it left is above cfg.SwapBackMinNeedKWh (a bus without
// any fresh reliable reading since then is not brought back). It returns the kept
// swaps and one note per dropped swap.
//
// It runs after PlanNormal's greedy search (recommendSwaps), which does not know which
// buses are blocked. So a blocked bus still takes part in the search: if it is the
// most urgent, it can take the only full donor, its swap is then dropped here, and no
// other waiting bus gets that donor in this cycle, nor in later cycles while the same
// blocked bus keeps winning the search. A kept swap judged after a dropped one also
// keeps its reason, whose predicted ready counts assumed the dropped swap. Re-running
// the search without the blocked buses was measured in review (not committed) and
// was worse: ready% 72.0 -> 66.6 at 1200 kW severe and 88.5 -> 83.0 at 2000 kW severe.
func (m *swapMemory) filter(cfg Config, in Input, swaps []Swap) (kept []Swap, notes []string) {
	if len(swaps) == 0 || len(m.gaveWay) == 0 || !cfg.antiThrash() {
		return swaps, nil
	}
	buses := map[string]model.Bus{}
	for _, b := range in.Buses {
		buses[b.ID] = b
	}
	chargers := chargerMap(in)
	for _, s := range swaps {
		g, ok := m.gaveWay[s.InBusID]
		if !ok || !g.left {
			kept = append(kept, s)
			continue
		}
		need, known := g.need(cfg, in, buses[s.InBusID], chargers[s.ChargerID])
		if in.Now-g.at >= cfg.SwapBackCooldownMin && known && need > cfg.SwapBackMinNeedKWh {
			kept = append(kept, s)
			continue
		}
		notes = append(notes, fmt.Sprintf("rodízio não recomendado: ônibus %s cedeu o carregador há %d min; pela média de %d leituras desde então faltam %.1f kWh (o mínimo para voltar é mais de %.1f kWh, após %d min): a leitura isolada pode ser ruído",
			s.InBusID, in.Now-g.at, g.count, need, cfg.SwapBackMinNeedKWh, cfg.SwapBackCooldownMin))
	}
	return kept, notes
}

// need is the kWh the bus is short of its margin-adjusted target by the mean of its
// readings; known is false before any fresh reliable reading.
func (g *gaveWay) need(cfg Config, in Input, b model.Bus, c model.Charger) (float64, bool) {
	if g.count == 0 {
		return 0, false
	}
	b.SoCKWh = g.sum / float64(g.count)
	b.SoCAgeMin, b.SoCConfidence = 0, 1
	return newCandidate(cfg, in, b, c).need, true
}

// remember records the buses told to give way by a plan the Planner returned.
func (m *swapMemory) remember(cfg Config, now model.Minute, swaps []Swap) {
	if !cfg.antiThrash() {
		return
	}
	for _, s := range swaps {
		if m.gaveWay == nil {
			m.gaveWay = map[string]*gaveWay{}
		}
		m.gaveWay[s.OutBusID] = &gaveWay{at: now, from: s.ChargerID, lastTS: now}
	}
}
