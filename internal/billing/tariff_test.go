package billing

import (
	"math"
	"strings"
	"testing"
)

func TestInPeakWindow(t *testing.T) {
	day := Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9}
	night := Tariff{PeakFromMin: 1320, PeakToMin: 120, PeakPrice: 2.7, OffPeakPrice: 0.9}
	never := Tariff{PeakFromMin: 600, PeakToMin: 600, PeakPrice: 2.7, OffPeakPrice: 0.9}
	cases := []struct {
		name string
		t    Tariff
		min  int
		want bool
	}{
		{"day 1079", day, 1079, false},
		{"day 1080", day, 1080, true},
		{"day 1259", day, 1259, true},
		{"day 1260", day, 1260, false},
		{"day 1500 (01:00)", day, 1500, false},
		{"day -60 (23:00)", day, -60, false},
		{"day 1440+1080", day, 1440 + 1080, true},
		{"day -360 (18:00 yesterday)", day, -360, true},
		{"cross 1380", night, 1380, true},
		{"cross 1320", night, 1320, true},
		{"cross 1319", night, 1319, false},
		{"cross 60", night, 60, true},
		{"cross 0", night, 0, true},
		{"cross 119", night, 119, true},
		{"cross 120", night, 120, false},
		{"cross 720", night, 720, false},
		{"cross 1440+60", night, 1440 + 60, true},
		{"cross -30 (23:30)", night, -30, true},
		{"never 600", never, 600, false},
		{"never 0", never, 0, false},
		{"never 1000", never, 1000, false},
	}
	for _, c := range cases {
		if got := c.t.InPeak(c.min); got != c.want {
			t.Errorf("%s: InPeak = %v, want %v", c.name, got, c.want)
		}
		want := c.t.OffPeakPrice
		if c.want {
			want = c.t.PeakPrice
		}
		if got := c.t.PriceAt(c.min); got != want {
			t.Errorf("%s: PriceAt = %v, want %v", c.name, got, want)
		}
	}
}

// oldPriceAt is the pre-billing sim.Tariff.PriceAt, copied verbatim.
func oldPriceAt(from, to int, peak, off float64, clockMin int) float64 {
	m := ((clockMin % 1440) + 1440) % 1440
	if m >= from && m < to {
		return peak
	}
	return off
}

func TestCompatWithOldSimTariff(t *testing.T) {
	tf := Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9}
	if tf.HasDemand() {
		t.Error("HasDemand = true for a tariff without demand prices or contract")
	}
	for m := -1440; m < 3*1440; m++ {
		if got, want := tf.PriceAt(m), oldPriceAt(1080, 1260, 2.7, 0.9, m); got != want {
			t.Fatalf("PriceAt(%d) = %v, old = %v", m, got, want)
		}
	}
}

func TestHasDemand(t *testing.T) {
	cases := []struct {
		name string
		t    Tariff
		want bool
	}{
		{"none", Tariff{}, false},
		{"peak price", Tariff{DemandPeakPrice: 50}, true},
		{"off-peak price", Tariff{DemandOffPeakPrice: 20}, true},
		{"contract only", Tariff{ContractedKW: 80}, true},
	}
	for _, c := range cases {
		if got := c.t.HasDemand(); got != c.want {
			t.Errorf("%s: HasDemand = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNights(t *testing.T) {
	if got := (Tariff{}).Nights(); got != 30 {
		t.Errorf("default Nights = %d, want 30", got)
	}
	if got := (Tariff{NightsPerMonth: 22}).Nights(); got != 22 {
		t.Errorf("Nights = %d, want 22", got)
	}
}

func TestValidate(t *testing.T) {
	ok := Tariff{PeakFromMin: 1080, PeakToMin: 1260, PeakPrice: 2.7, OffPeakPrice: 0.9,
		DemandPeakPrice: 50, DemandOffPeakPrice: 20, ContractedKW: 80, NightsPerMonth: 366}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid tariff rejected: %v", err)
	}
	if err := (Tariff{}).Validate(); err != nil {
		t.Fatalf("zero tariff rejected: %v", err)
	}
	if err := (Tariff{PeakFromMin: 0, PeakToMin: 1440}).Validate(); err != nil {
		t.Fatalf("window 0..1440 rejected: %v", err)
	}
	nan, inf := math.NaN(), math.Inf(1)
	bad := map[string]Tariff{
		"peak price negative":  {PeakPrice: -1},
		"off-peak price NaN":   {OffPeakPrice: nan},
		"peak price Inf":       {PeakPrice: inf},
		"demand peak negative": {DemandPeakPrice: -0.01},
		"demand off-peak NaN":  {DemandOffPeakPrice: nan},
		"demand off-peak -Inf": {DemandOffPeakPrice: math.Inf(-1)},
		"contract negative":    {ContractedKW: -5},
		"contract Inf":         {ContractedKW: inf},
		"from negative":        {PeakFromMin: -1},
		"from 1441":            {PeakFromMin: 1441},
		"to negative":          {PeakToMin: -1},
		"to 1441":              {PeakToMin: 1441},
		"nights 367":           {NightsPerMonth: 367},
		"nights negative":      {NightsPerMonth: -1},
	}
	for name, tf := range bad {
		err := tf.Validate()
		if err == nil {
			t.Errorf("%s: Validate = nil, want error", name)
			continue
		}
		if strings.TrimSpace(err.Error()) == "" {
			t.Errorf("%s: empty error message", name)
		}
	}
}

func TestValidateNamesTheField(t *testing.T) {
	err := Tariff{PeakPrice: 1, DemandPeakPrice: -1}.Validate()
	if err == nil || !strings.Contains(err.Error(), "demanda na ponta") {
		t.Errorf("error = %v, want one naming the demand peak price", err)
	}
}
