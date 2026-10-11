package billing

import (
	"math"
	"testing"
)

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.IsNaN(got) || math.IsInf(got, 0) || math.Abs(got-want) > 1e-9*math.Max(1, math.Abs(want)) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func azul() Tariff {
	return Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9,
		DemandPeakPrice: 50, DemandOffPeakPrice: 20}
}

// 100 kW from 20:00 to 22:00 (peak until 21:00).
func profile(t Tariff) Bill {
	m := NewMeter(t)
	m.AddKW(1200, 1320, 100)
	return m.Bill()
}

func TestCase1Azul(t *testing.T) {
	b := profile(azul())
	near(t, "EnergyKWh", b.EnergyKWh, 200)
	near(t, "EnergyBRL", b.EnergyBRL, 360)
	near(t, "DemandKW", b.DemandKW, 100)
	near(t, "DemandPeakKW", b.DemandPeakKW, 100)
	near(t, "DemandOffPeakKW", b.DemandOffPeakKW, 100)
	near(t, "BilledPeakKW", b.BilledPeakKW, 100)
	near(t, "BilledOffPeakKW", b.BilledOffPeakKW, 100)
	near(t, "OverrunKW", b.OverrunKW, 0)
	near(t, "OverrunBRL", b.OverrunBRL, 0)
	near(t, "DemandBRL", b.DemandBRL, 7000)
	near(t, "MonthlyBRL", b.MonthlyBRL, 17800)
}

func TestCase2Verde(t *testing.T) {
	tf := Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9,
		DemandVerde: true, DemandOffPeakPrice: 20}
	b := profile(tf)
	near(t, "DemandKW", b.DemandKW, 100)
	near(t, "BilledOffPeakKW", b.BilledOffPeakKW, 100)
	near(t, "BilledPeakKW", b.BilledPeakKW, 0)
	near(t, "DemandBRL", b.DemandBRL, 2000)
	near(t, "MonthlyBRL", b.MonthlyBRL, 360*30+2000)
	// the peak demand price is ignored in Verde
	tf.DemandPeakPrice = 999
	near(t, "DemandBRL with peak price set", profile(tf).DemandBRL, 2000)
}

func TestCase3PartialBlock(t *testing.T) {
	m := NewMeter(azul())
	m.AddKW(1200, 1205, 400)
	b := m.Bill()
	want := 400.0 * 5 / 60 / 0.25 // 133.333... kW
	near(t, "DemandKW", b.DemandKW, want)
	near(t, "DemandPeakKW", b.DemandPeakKW, want)
	near(t, "DemandOffPeakKW", b.DemandOffPeakKW, 0)
	near(t, "EnergyKWh", b.EnergyKWh, 400.0*5/60)
}

func TestCase4Contract(t *testing.T) {
	tf := azul()
	tf.ContractedKW = 80 // measured 100 > 84
	b := profile(tf)
	near(t, "BilledPeakKW", b.BilledPeakKW, 80)
	near(t, "BilledOffPeakKW", b.BilledOffPeakKW, 80)
	near(t, "OverrunKW", b.OverrunKW, 40)
	near(t, "OverrunBRL", b.OverrunBRL, 2800)
	near(t, "DemandBRL", b.DemandBRL, 8400)
	near(t, "MonthlyBRL", b.MonthlyBRL, 360*30+8400)

	tf.ContractedKW = 100
	b = profile(tf)
	near(t, "BilledPeakKW@100", b.BilledPeakKW, 100)
	near(t, "OverrunKW@100", b.OverrunKW, 0)
	near(t, "DemandBRL@100", b.DemandBRL, 7000)

	tf.ContractedKW = 120
	b = profile(tf)
	near(t, "BilledPeakKW@120", b.BilledPeakKW, 120)
	near(t, "BilledOffPeakKW@120", b.BilledOffPeakKW, 120)
	near(t, "OverrunKW@120", b.OverrunKW, 0)
	near(t, "DemandBRL@120", b.DemandBRL, 8400)
}

func TestContractTolerance(t *testing.T) {
	tf := azul()
	tf.ContractedKW = 100 // 105 kW is exactly at the tolerance edge
	m := NewMeter(tf)
	m.AddKW(1200, 1320, 105)
	b := m.Bill()
	near(t, "Billed at 105%", b.BilledPeakKW, 100)
	near(t, "Overrun at 105%", b.OverrunKW, 0)
	near(t, "DemandBRL at 105%", b.DemandBRL, 100*50+100*20)

	m = NewMeter(tf)
	m.AddKW(1200, 1320, 106)
	b = m.Bill()
	near(t, "Billed at 106%", b.BilledPeakKW, 100)
	near(t, "Overrun at 106%", b.OverrunKW, 12)
	near(t, "OverrunBRL at 106%", b.OverrunBRL, 6*2*50+6*2*20)
	near(t, "DemandBRL at 106%", b.DemandBRL, 100*50+100*20+6*2*50+6*2*20)
}

func TestContractWithoutUse(t *testing.T) {
	tf := azul()
	tf.ContractedKW = 80
	b := NewMeter(tf).Bill()
	near(t, "DemandBRL idle", b.DemandBRL, 80*50+80*20)
	near(t, "EnergyBRL idle", b.EnergyBRL, 0)

	tf.DemandVerde = true // Verde: single window, off-peak price only
	b = NewMeter(tf).Bill()
	near(t, "Verde idle", b.DemandBRL, 80*20)
}

func TestVerdeContractOverrun(t *testing.T) {
	tf := Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9,
		DemandVerde: true, DemandOffPeakPrice: 20, ContractedKW: 80}
	b := profile(tf)
	near(t, "BilledOffPeakKW", b.BilledOffPeakKW, 80)
	near(t, "OverrunKW", b.OverrunKW, 20)
	near(t, "OverrunBRL", b.OverrunBRL, 20*2*20)
	near(t, "DemandBRL", b.DemandBRL, 80*20+20*2*20)
}

func TestAzulPeaksDiffer(t *testing.T) {
	m := NewMeter(azul())
	m.AddKW(1080, 1095, 60)  // peak block, 60 kW
	m.AddKW(1320, 1335, 100) // off-peak block, 100 kW
	b := m.Bill()
	near(t, "DemandKW", b.DemandKW, 100)
	near(t, "DemandPeakKW", b.DemandPeakKW, 60)
	near(t, "DemandOffPeakKW", b.DemandOffPeakKW, 100)
	near(t, "DemandBRL", b.DemandBRL, 60*50+100*20)
}

func TestBlockBelongsToPeakByFirstMinute(t *testing.T) {
	// block 1245..1259 starts in peak; block 1260.. starts off-peak.
	m := NewMeter(azul())
	m.AddKW(1259, 1261, 60) // 1 min in block 1245 (peak), 1 min in block 1260 (off)
	b := m.Bill()
	near(t, "DemandPeakKW", b.DemandPeakKW, 60.0/60/0.25)
	near(t, "DemandOffPeakKW", b.DemandOffPeakKW, 60.0/60/0.25)
	// a call straddling a block edge adds to both blocks
	m = NewMeter(azul())
	m.AddKW(1190, 1210, 60) // 10 min in block 1185..1199, 10 min in 1200..1214
	b = m.Bill()
	near(t, "straddle DemandKW", b.DemandKW, 10.0/60*60/0.25)
}

func TestBlocksAccumulateAcrossCalls(t *testing.T) {
	m := NewMeter(azul())
	m.AddKW(1200, 1207, 100)
	m.AddKW(1207, 1215, 100)
	m.AddKW(1200, 1215, 20) // overlapping adds up
	b := m.Bill()
	near(t, "DemandKW", b.DemandKW, 120)
}

func TestCrossMidnightWindowAndAbsoluteMinutes(t *testing.T) {
	tf := Tariff{PeakFromMin: 1320, PeakToMin: 120, PeakPrice: 3, OffPeakPrice: 1,
		DemandPeakPrice: 10, DemandOffPeakPrice: 5}
	m := NewMeter(tf)
	m.AddKW(1440+30, 1440+45, 40) // 00:30..00:45 next day, in peak
	m.AddKW(1440+600, 1440+615, 20)
	b := m.Bill()
	near(t, "DemandPeakKW", b.DemandPeakKW, 40)
	near(t, "DemandOffPeakKW", b.DemandOffPeakKW, 20)
	near(t, "EnergyBRL", b.EnergyBRL, 40.0*15/60*3+20.0*15/60*1)
}

func TestNegativeClockMinutes(t *testing.T) {
	// -15..0 is the block 23:45..24:00 of the previous day: off-peak for 18-21.
	m := NewMeter(azul())
	m.AddKW(-20, -5, 60)
	b := m.Bill()
	// blocks -2 (-30..-16): 10 min; -1 (-15..-1): 10 min -> 60*10/60/0.25 = 40 each
	near(t, "DemandKW", b.DemandKW, 40)
	near(t, "EnergyKWh", b.EnergyKWh, 15)
	near(t, "EnergyBRL", b.EnergyBRL, 15*0.9)
	// -360 is 18:00 of the previous day: in peak
	m = NewMeter(azul())
	m.AddKW(-360, -345, 100)
	b = m.Bill()
	near(t, "DemandPeakKW", b.DemandPeakKW, 100)
	near(t, "DemandOffPeakKW", b.DemandOffPeakKW, 0)
}

func TestInvalidInputIgnored(t *testing.T) {
	m := NewMeter(azul())
	m.AddKW(1200, 1260, math.NaN())
	m.AddKW(1200, 1260, math.Inf(1))
	m.AddKW(1200, 1260, math.Inf(-1))
	m.AddKW(1200, 1260, -50)
	m.AddKW(1200, 1260, 0)
	m.AddKW(1260, 1200, 100)
	m.AddKW(1200, 1200, 100)
	if b := m.Bill(); b != (Bill{}) {
		t.Errorf("Bill = %+v, want zero", b)
	}
	m.AddKW(1200, 1215, 100)
	near(t, "valid after invalid", m.Bill().DemandKW, 100)
}

func TestBillIsRepeatableAndNonDestructive(t *testing.T) {
	m := NewMeter(azul())
	m.AddKW(1200, 1260, 50)
	a := m.Bill()
	m.AddKW(1260, 1320, 50)
	b := m.Bill()
	if a.EnergyKWh >= b.EnergyKWh {
		t.Errorf("Bill after more energy did not grow: %v then %v", a.EnergyKWh, b.EnergyKWh)
	}
	if c := m.Bill(); c != b {
		t.Errorf("Bill not repeatable: %+v vs %+v", c, b)
	}
}

func TestEnergyAccumulatedMinuteByMinute(t *testing.T) {
	tf := azul()
	m := NewMeter(tf)
	var want float64
	for min := 1000; min < 1400; min++ {
		kw := 10 + float64(min%7)*1.3
		m.AddKW(min, min+1, kw)
		want += kw / 60 * tf.PriceAt(min)
	}
	if got := m.Bill().EnergyBRL; got != want {
		t.Errorf("EnergyBRL = %v, want bit-identical %v", got, want)
	}
}

func TestNoDemandPricesNoDemandCharge(t *testing.T) {
	tf := Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9}
	b := profile(tf)
	near(t, "DemandBRL", b.DemandBRL, 0)
	near(t, "MonthlyBRL", b.MonthlyBRL, 360*30)
	near(t, "DemandKW still measured", b.DemandKW, 100)
}

func TestAddKWAllocs(t *testing.T) {
	m := NewMeter(azul())
	m.AddKW(0, 1440, 10) // warm the map
	n := testing.AllocsPerRun(20, func() { m.AddKW(0, 1440, 10) })
	if n > 0 {
		t.Errorf("AddKW over existing blocks allocated %v times", n)
	}
}
