package sim

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/gateway"
	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

const (
	stepHours       = 1.0 / 60.0
	swapDurationMin = 5
)

var _ gateway.Gateway = (*World)(nil)

type busState struct {
	spec      BusSpec
	soc       float64 // true SoC
	target    float64 // true energy required at departure
	arrival   int
	departure int
	chargerID string
	present   bool
	departed  bool
	busyUntil int
	parked    bool    // unplugged by operators after charging (UnplugFull): never re-plugged
	shown     float64 // last SoC reading given to the controller (what operators also see)
	shownOK   bool    // false until a usable reading was shown (or while it is missing)
	lastGood  float64 // last observed value, used by the freeze fault
	lastGoodT int
}

// World holds the truth. The planner only ever sees observe() and Chargers().
type World struct {
	sc        Scenario
	rng       *rand.Rand
	t         int
	buses     []*busState // sorted by ID
	chargers  []model.Charger
	cmd       map[string]float64 // commands issued this step
	applied   map[string]float64 // power in effect (commands of the previous step)
	physKW    map[string]float64 // grid power each charger actually delivered this step
	physTotal float64            // sum of physKW (what the overshoot metric compares to the limit)
	freed     map[string]bool    // chargers operators freed this tick (UnplugFull)
	moves     int                // manual moves: swaps executed and buses unplugged
	ready     int
	shortfall float64
}

func newWorld(sc Scenario) *World {
	w := &World{
		sc:      sc,
		rng:     rand.New(rand.NewSource(sc.Seed)),
		cmd:     map[string]float64{},
		applied: map[string]float64{},
		physKW:  map[string]float64{},
	}
	w.chargers = append([]model.Charger(nil), sc.Chargers...)
	sort.Slice(w.chargers, func(i, j int) bool { return w.chargers[i].ID < w.chargers[j].ID })
	specs := append([]BusSpec(nil), sc.Buses...)
	sort.Slice(specs, func(i, j int) bool { return specs[i].Bus.ID < specs[j].Bus.ID })
	for _, s := range specs {
		bs := &busState{spec: s, soc: s.Bus.SoCKWh, target: s.Bus.TargetKWh,
			arrival: s.Bus.ArrivalMin, departure: s.Bus.DepartureMin}
		if s.TrueTargetKWh > 0 {
			bs.target = s.TrueTargetKWh
		}
		for _, f := range sc.Faults {
			if f.Target != s.Bus.ID {
				continue
			}
			switch f.Kind {
			case FaultLateArrival:
				bs.arrival += int(f.Value)
			case FaultConsumption:
				bs.target += f.Value
			}
		}
		bs.target = math.Min(bs.target, s.Bus.CapacityKWh)
		w.buses = append(w.buses, bs)
	}
	return w
}

func (w *World) charger(id string) *model.Charger {
	for i := range w.chargers {
		if w.chargers[i].ID == id {
			return &w.chargers[i]
		}
	}
	return nil
}

func (w *World) statusOf(id string) model.ChargerStatus {
	if c := w.charger(id); c != nil {
		return c.Status
	}
	return model.ChargerFaulted
}

func (w *World) find(id string) *busState {
	for _, bs := range w.buses {
		if bs.spec.Bus.ID == id {
			return bs
		}
	}
	return nil
}

// Chargers implements gateway.Gateway: the observed state (offline chargers are
// visible as offline, with the power they keep drawing).
func (w *World) Chargers() []model.Charger {
	out := make([]model.Charger, len(w.chargers))
	for i, c := range w.chargers {
		c.LastCommandedKW = w.applied[c.ID]
		out[i] = c
	}
	return out
}

// SetPower implements gateway.Gateway.
func (w *World) SetPower(id string, kw float64) error {
	c := w.charger(id)
	if c == nil {
		return fmt.Errorf("carregador desconhecido: %s", id)
	}
	if c.Status != model.ChargerOK {
		return fmt.Errorf("carregador %s indisponível", id)
	}
	if math.IsNaN(kw) || math.IsInf(kw, 0) || kw < 0 {
		return fmt.Errorf("potência inválida: %v", kw)
	}
	w.cmd[id] = kw
	return nil
}

func (w *World) limitAt(t int) float64 {
	f := 1.0
	for _, x := range w.sc.Faults {
		if x.Kind == FaultLimitDrop && x.Active(t) && x.Value < f {
			f = x.Value
		}
	}
	return w.sc.BaseLimitKW * f
}

// beginTick applies faults, departures, arrivals and operator behaviour.
func (w *World) beginTick(t int) {
	w.t = t
	for i := range w.chargers {
		w.chargers[i].Status = model.ChargerOK
		for _, f := range w.sc.Faults {
			if f.Target != w.chargers[i].ID || !f.Active(t) {
				continue
			}
			switch f.Kind {
			case FaultChargerFail:
				w.chargers[i].Status = model.ChargerFaulted
			case FaultChargerOffline:
				if w.chargers[i].Status == model.ChargerOK {
					w.chargers[i].Status = model.ChargerOffline
				}
			}
		}
	}
	for _, f := range w.sc.Faults {
		if f.Kind == FaultEarlyDeparture && f.From == t {
			if bs := w.find(f.Target); bs != nil {
				bs.departure = int(f.Value)
			}
		}
	}
	for _, bs := range w.buses {
		if bs.present && !bs.departed && t >= bs.departure {
			w.depart(bs)
		}
	}
	for _, bs := range w.buses {
		if !bs.present && !bs.departed && t >= bs.arrival {
			bs.present = true
			bs.lastGood, bs.lastGoodT = bs.soc, t
		}
	}
	if w.sc.UnplugFull {
		w.unplugFull()
	}
	w.connectWaiting()
}

// waitingBus reports whether a bus is present and needs a (working) charger.
func (w *World) waitingBus(bs *busState) bool {
	if !bs.present || bs.departed || bs.parked {
		return false
	}
	return bs.chargerID == "" || w.statusOf(bs.chargerID) == model.ChargerFaulted
}

// unplugFull frees one charger per waiting bus by unplugging connected buses whose
// SoC reading already reached their forecast target.
func (w *World) unplugFull() {
	waiting := 0
	for _, bs := range w.buses {
		if w.waitingBus(bs) {
			waiting++
		}
	}
	w.freed = map[string]bool{}
	for _, bs := range w.buses { // sorted by ID: deterministic
		if waiting == 0 {
			return
		}
		// Operators judge by the same (possibly faulty) SoC reading the controllers get,
		// from the previous minute: they have no access to the simulator's truth.
		if !bs.present || bs.departed || bs.chargerID == "" || w.t < bs.busyUntil ||
			w.statusOf(bs.chargerID) != model.ChargerOK || !bs.shownOK || bs.shown < bs.spec.Bus.TargetKWh-1e-6 {
			continue
		}
		w.freed[bs.chargerID] = true
		bs.chargerID = ""
		bs.parked = true
		waiting--
		w.moves++
	}
}

func (w *World) depart(bs *busState) {
	bs.departed = true
	bs.chargerID = ""
	if bs.soc >= bs.target-1e-6 {
		w.ready++
	} else {
		w.shortfall += bs.target - bs.soc
	}
}

// connectWaiting plugs waiting buses (and buses on faulted chargers) into free healthy chargers.
func (w *World) connectWaiting() {
	occupied := map[string]bool{}
	for _, bs := range w.buses {
		if bs.present && !bs.departed && bs.chargerID != "" {
			occupied[bs.chargerID] = true
		}
	}
	var waiting []*busState
	for _, bs := range w.buses {
		if w.waitingBus(bs) {
			waiting = append(waiting, bs)
		}
	}
	sort.SliceStable(waiting, func(i, j int) bool { return waiting[i].arrival < waiting[j].arrival })
	for _, bs := range waiting {
		for i := range w.chargers {
			c := &w.chargers[i]
			if c.Status == model.ChargerOK && !occupied[c.ID] {
				occupied[c.ID] = true
				bs.chargerID = c.ID
				if w.freed[c.ID] { // operators are moving buses: same time as a swap
					bs.busyUntil = w.t + swapDurationMin
				}
				break
			}
		}
	}
}

// sense returns what the SoC sensor reports for a bus.
func (w *World) sense(bs *busState) (soc float64, ageMin int, conf float64) {
	soc = bs.soc
	frozen, missing := false, false
	for _, f := range w.sc.Faults {
		if !f.Active(w.t) || (f.Target != "*" && f.Target != bs.spec.Bus.ID) {
			continue
		}
		switch f.Kind {
		case FaultSoCNoise:
			soc += w.rng.NormFloat64() * f.Value
		case FaultSoCBias:
			soc += f.Value
		case FaultSoCFreeze:
			frozen = true
		case FaultSoCMissing:
			missing = true
		}
	}
	switch {
	case missing:
		return 0, 10000, 0
	case frozen:
		return bs.lastGood, w.t - bs.lastGoodT + w.sc.ReadingAgeMin, 1
	}
	bs.lastGood, bs.lastGoodT = soc, w.t
	return soc, w.sc.ReadingAgeMin, 1
}

// observe builds the planner input from sensors and schedules (never from the truth).
func (w *World) observe() planner.Input {
	in := planner.Input{
		Now:      w.t,
		Site:     model.Site{LimitKW: w.limitAt(w.t), StepMin: 1},
		Chargers: w.Chargers(),
	}
	for _, bs := range w.buses {
		if !bs.present || bs.departed {
			continue
		}
		b := bs.spec.Bus
		b.ChargerID = bs.chargerID
		b.ArrivalMin = bs.arrival
		b.DepartureMin = bs.departure
		b.SoCKWh, b.SoCAgeMin, b.SoCConfidence = w.sense(bs)
		bs.shown, bs.shownOK = b.SoCKWh, b.SoCConfidence > 0
		in.Buses = append(in.Buses, b)
	}
	return in
}

// commandedTotal is the power the commands imply: this step's commands for
// controllable chargers plus what offline chargers keep drawing.
func (w *World) commandedTotal() float64 {
	total := 0.0
	for _, c := range w.chargers {
		if c.Status == model.ChargerOffline {
			total += w.applied[c.ID]
		} else {
			total += w.cmd[c.ID]
		}
	}
	return total
}

// commandedBy is the power one charger draws under this step's commands (offline
// chargers keep drawing their last power).
func (w *World) commandedBy(c model.Charger) float64 {
	if c.Status == model.ChargerOffline {
		return w.applied[c.ID]
	}
	return w.cmd[c.ID]
}

// applySwaps executes recommended swaps (when operators follow them).
func (w *World) applySwaps(swaps []planner.Swap) {
	for _, s := range swaps {
		out, in := w.find(s.OutBusID), w.find(s.InBusID)
		if out == nil || in == nil || !out.present || out.departed || !in.present || in.departed {
			continue
		}
		if out.chargerID != s.ChargerID || w.statusOf(s.ChargerID) != model.ChargerOK {
			continue
		}
		prev := in.chargerID
		in.chargerID = s.ChargerID
		out.chargerID = ""
		if prev != "" && w.statusOf(prev) == model.ChargerOK {
			out.chargerID = prev
		}
		in.busyUntil = w.t + swapDurationMin
		out.busyUntil = w.t + swapDurationMin
		w.moves++
	}
}

// advance runs the physics for one step using the power in effect (previous commands).
func (w *World) advance(m *Metrics) {
	w.physKW = map[string]float64{}
	total := 0.0
	for _, bs := range w.buses {
		if !bs.present || bs.departed || bs.chargerID == "" || w.t < bs.busyUntil {
			continue
		}
		c := w.charger(bs.chargerID)
		if c == nil || c.Status == model.ChargerFaulted {
			continue
		}
		gridKW := math.Min(w.applied[c.ID], c.MaxKW)
		if gridKW < c.MinKW {
			gridKW = 0
		}
		b := bs.spec.Bus
		battKW := math.Min(gridKW*c.Efficiency, b.MaxBatteryKW*model.TaperFactor(bs.soc, b.CapacityKWh))
		e := math.Min(battKW*stepHours, b.CapacityKWh-bs.soc)
		if e <= 0 {
			continue
		}
		bs.soc += e
		grid := e / c.Efficiency
		total += grid / stepHours
		w.physKW[c.ID] = grid / stepHours
		m.EnergyKWh += grid
		m.CostBRL += grid * w.sc.Tariff.PriceAt(w.sc.StartClockMin+w.t)
	}
	w.physTotal = total
	if total > m.PeakKW {
		m.PeakKW = total
	}
	if total > w.limitAt(w.t)+1e-6 {
		m.OvershootMin++
	}
}

// endTick makes this step's commands the power in effect for the next step.
func (w *World) endTick() {
	for _, c := range w.chargers {
		switch c.Status {
		case model.ChargerOK:
			w.applied[c.ID] = w.cmd[c.ID]
		case model.ChargerFaulted:
			w.applied[c.ID] = 0
		} // offline chargers keep drawing their last power
	}
}

func (w *World) finish(m *Metrics) {
	for _, bs := range w.buses {
		if bs.present && !bs.departed {
			w.depart(bs)
		}
	}
	m.Ready = w.ready
	m.ShortfallKWh = w.shortfall
	m.OperatorMoves = float64(w.moves)
	if m.Buses > 0 {
		m.ReadyPct = 100 * float64(m.Ready) / float64(m.Buses)
	}
}
