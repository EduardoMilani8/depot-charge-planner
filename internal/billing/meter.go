package billing

// maxKW is the largest single reading AddKW accepts. Anything above it is a
// sensor glitch, and ignoring it keeps every Bill field finite.
const maxKW = 1e9

// Bill is the priced result of a Meter. Demand fields are kW, money is R$.
type Bill struct {
	EnergyKWh, EnergyBRL float64

	DemandKW        float64 // largest 15-minute block (any window)
	DemandPeakKW    float64 // largest block starting inside the peak window
	DemandOffPeakKW float64 // largest block starting outside it

	BilledPeakKW, BilledOffPeakKW float64 // what the tariff charges, after contract rules
	OverrunKW                     float64 // kW above the contract, summed over the windows charged as overrun

	DemandBRL  float64
	OverrunBRL float64 // already included in DemandBRL

	MonthlyBRL float64 // EnergyBRL*Nights + DemandBRL; an estimate (one night stands for the month)
}

// Meter accumulates grid energy per clock minute and prices it.
type Meter struct {
	t      Tariff
	kwh    float64
	brl    float64
	blocks map[int]float64 // kWh per absolute block index floor(clockMin/BlockMin)
}

func NewMeter(t Tariff) *Meter {
	return &Meter{t: t, blocks: make(map[int]float64)}
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// AddKW adds a constant grid power over the clock minutes [from, to). Minutes
// are absolute (may exceed 1440 or be negative). Non-positive, NaN, Inf or
// absurd (> 1e9 kW) power and empty ranges are ignored, so a Bill never holds
// NaN or Inf. Energy is accumulated minute by minute, in call order, so sums
// match a per-minute simulator loop bit for bit. The cost is O(minutes in the
// range), so callers should feed ranges of a night, not of years.
func (m *Meter) AddKW(fromClockMin, toClockMin int, kw float64) {
	if !(kw > 0) || kw > maxKW || toClockMin <= fromClockMin {
		return
	}
	e := kw / 60
	cur := floorDiv(fromClockMin, BlockMin)
	acc := m.blocks[cur]
	for min := fromClockMin; min < toClockMin; min++ {
		if b := floorDiv(min, BlockMin); b != cur {
			m.blocks[cur] = acc
			cur, acc = b, m.blocks[b]
		}
		m.kwh += e
		m.brl += e * m.t.PriceAt(min)
		acc += e
	}
	m.blocks[cur] = acc
}

// window bills one demand window: measured kW at price R$/kW.
func (m *Meter) window(measured, price float64) (billed, overKW, overBRL float64) {
	c := m.t.ContractedKW
	switch {
	case c <= 0:
		return measured, 0, 0
	case measured > c*(1+OverrunTolerance):
		return c, measured - c, (measured - c) * OverrunFactor * price
	}
	return c, 0, 0
}

func (m *Meter) Bill() Bill {
	b := Bill{EnergyKWh: m.kwh, EnergyBRL: m.brl}
	for idx, kwh := range m.blocks {
		kw := kwh / (BlockMin / 60.0)
		if kw > b.DemandKW {
			b.DemandKW = kw
		}
		if m.t.InPeak(idx * BlockMin) {
			if kw > b.DemandPeakKW {
				b.DemandPeakKW = kw
			}
		} else if kw > b.DemandOffPeakKW {
			b.DemandOffPeakKW = kw
		}
	}
	if m.t.DemandVerde {
		billed, over, overBRL := m.window(b.DemandKW, m.t.DemandOffPeakPrice)
		b.BilledOffPeakKW, b.OverrunKW, b.OverrunBRL = billed, over, overBRL
		b.DemandBRL = billed*m.t.DemandOffPeakPrice + overBRL
	} else {
		pk, pkOver, pkBRL := m.window(b.DemandPeakKW, m.t.DemandPeakPrice)
		op, opOver, opBRL := m.window(b.DemandOffPeakKW, m.t.DemandOffPeakPrice)
		b.BilledPeakKW, b.BilledOffPeakKW = pk, op
		b.OverrunKW, b.OverrunBRL = pkOver+opOver, pkBRL+opBRL
		b.DemandBRL = pk*m.t.DemandPeakPrice + op*m.t.DemandOffPeakPrice + b.OverrunBRL
	}
	b.MonthlyBRL = b.EnergyBRL*float64(m.t.Nights()) + b.DemandBRL
	return b
}
