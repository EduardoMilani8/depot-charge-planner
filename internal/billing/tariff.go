// Package billing prices grid energy and demand with a simplified Group A-style
// bill (Brazilian medium voltage): energy by time of use plus the largest
// 15-minute average demand, in the Azul or Verde mode, with optional contracted
// demand. All prices are parameters; nothing here is an official tariff and the
// result is an estimate, not an invoice.
package billing

import (
	"fmt"
	"math"
)

const (
	// BlockMin is the demand integration block, aligned to the clock.
	BlockMin = 15
	// OverrunTolerance is the fraction above the contract that is still
	// billed as the contract; above it the excess is charged as overrun.
	OverrunTolerance = 0.05
	// OverrunFactor multiplies the demand price of the kW above the contract.
	// Both constants are assumptions to be checked with the utility. In
	// particular, billed demand within (C, 1.05*C] is modelled as C itself
	// (the tolerance band is not charged), to be confirmed with the distributor.
	OverrunFactor = 2.0
	// DefaultNightsPerMonth is used when Tariff.NightsPerMonth is 0.
	DefaultNightsPerMonth = 30
)

// Tariff holds prices and the contract. The zero demand prices and no
// contract reproduce the old energy-only tariff exactly.
type Tariff struct {
	PeakFromMin, PeakToMin int // clock minutes of the day; From > To crosses midnight; From == To is never peak

	PeakPrice, OffPeakPrice             float64 // R$/kWh
	DemandPeakPrice, DemandOffPeakPrice float64 // R$/kW; Verde uses DemandOffPeakPrice only (DemandPeakPrice is ignored)
	DemandVerde                         bool    // Verde mode: one demand charge, on the largest block of the whole night
	// ContractedKW is the contracted demand, 0 = no contract (billed demand is
	// the measured block maximum). A real Azul contract has separate peak and
	// off-peak values; this single value is applied to both windows. In Verde
	// it applies to the single (off-peak-priced) demand.
	ContractedKW float64
	// NightsPerMonth scales one night to a month; <= 0 means DefaultNightsPerMonth.
	NightsPerMonth int
}

// InPeak reports whether the clock minute (any int, reduced modulo 1440) is
// inside the peak window [PeakFromMin, PeakToMin).
func (t Tariff) InPeak(clockMin int) bool {
	m := ((clockMin % 1440) + 1440) % 1440
	switch {
	case t.PeakFromMin < t.PeakToMin:
		return m >= t.PeakFromMin && m < t.PeakToMin
	case t.PeakFromMin > t.PeakToMin:
		return m >= t.PeakFromMin || m < t.PeakToMin
	}
	return false
}

// PriceAt is the energy price in R$/kWh at the clock minute.
func (t Tariff) PriceAt(clockMin int) float64 {
	if t.InPeak(clockMin) {
		return t.PeakPrice
	}
	return t.OffPeakPrice
}

// HasDemand reports whether demand is billed at all. In Verde only the
// off-peak demand price counts (the peak demand price is ignored).
func (t Tariff) HasDemand() bool {
	if t.DemandVerde {
		return t.DemandOffPeakPrice > 0 || t.ContractedKW > 0
	}
	return t.DemandPeakPrice > 0 || t.DemandOffPeakPrice > 0 || t.ContractedKW > 0
}

// Nights is the number of nights used to scale one night to a month.
func (t Tariff) Nights() int {
	if t.NightsPerMonth <= 0 {
		return DefaultNightsPerMonth
	}
	return t.NightsPerMonth
}

func okPrice(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

// Validate rejects values that would make a bill meaningless.
func (t Tariff) Validate() error {
	for _, f := range []struct {
		name string
		v    float64
	}{
		{"preço de energia na ponta", t.PeakPrice},
		{"preço de energia fora da ponta", t.OffPeakPrice},
		{"preço de demanda na ponta", t.DemandPeakPrice},
		{"preço de demanda fora da ponta", t.DemandOffPeakPrice},
		{"demanda contratada", t.ContractedKW},
	} {
		if !okPrice(f.v) {
			return fmt.Errorf("tarifa: %s deve ser um número finito e não negativo", f.name)
		}
	}
	if t.PeakFromMin < 0 || t.PeakFromMin > 1440 || t.PeakToMin < 0 || t.PeakToMin > 1440 {
		return fmt.Errorf("tarifa: a janela de ponta deve estar entre 0 e 1440 minutos")
	}
	if t.NightsPerMonth < 0 || t.NightsPerMonth > 366 {
		return fmt.Errorf("tarifa: noites por mês deve estar entre 0 e 366")
	}
	return nil
}
