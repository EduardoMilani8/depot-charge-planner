package sim

import (
	"fmt"
	"math/rand"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

type FaultProfile string

const (
	ProfileNone   FaultProfile = "none"
	ProfileMild   FaultProfile = "mild"
	ProfileSevere FaultProfile = "severe"
)

type GenParams struct {
	NumBuses, NumChargers int
	LimitKW               float64
	ChargerMaxKW          float64
	ChargerMinKW          float64
	Efficiency            float64
	CapacityKWh           float64
	MaxBatteryKW          float64
	Profile               FaultProfile
}

// DefaultGenParams uses public-data assumptions that still need validation with real operators.
func DefaultGenParams() GenParams {
	return GenParams{
		NumBuses: 50, NumChargers: 25, LimitKW: 2000,
		ChargerMaxKW: 150, ChargerMinKW: 5, Efficiency: 0.94,
		CapacityKWh: 350, MaxBatteryKW: 150, Profile: ProfileNone,
	}
}

// Generate builds a reproducible scenario: minute 0 is 18:00, buses arrive 20:00-24:00
// and leave 04:30-07:00.
func Generate(p GenParams, seed int64) Scenario {
	rng := rand.New(rand.NewSource(seed))
	sc := Scenario{
		Name:          fmt.Sprintf("gen-%s-%d", p.Profile, seed),
		Seed:          seed,
		StartClockMin: 18 * 60,
		Horizon:       800,
		BaseLimitKW:   p.LimitKW,
		Tariff:        Tariff{PeakFromMin: 18 * 60, PeakToMin: 21 * 60, PeakPrice: 2.70, OffPeakPrice: 0.90},
		FollowSwaps:   true,
	}
	for i := 0; i < p.NumChargers; i++ {
		sc.Chargers = append(sc.Chargers, model.Charger{
			ID: fmt.Sprintf("C%03d", i+1), MaxKW: p.ChargerMaxKW, MinKW: p.ChargerMinKW,
			Efficiency: p.Efficiency, Status: model.ChargerOK,
		})
	}
	for i := 0; i < p.NumBuses; i++ {
		sc.Buses = append(sc.Buses, BusSpec{Bus: model.Bus{
			ID:            fmt.Sprintf("B%03d", i+1),
			CapacityKWh:   p.CapacityKWh,
			SoCKWh:        p.CapacityKWh * (0.15 + 0.30*rng.Float64()),
			SoCConfidence: 1,
			TargetKWh:     p.CapacityKWh * (0.75 + 0.20*rng.Float64()),
			ArrivalMin:    120 + rng.Intn(241),
			DepartureMin:  630 + rng.Intn(151),
			MaxBatteryKW:  p.MaxBatteryKW,
		}})
	}
	genFaults(rng, p.Profile, &sc)
	return sc
}

func genFaults(rng *rand.Rand, profile FaultProfile, sc *Scenario) {
	if len(sc.Chargers) == 0 || len(sc.Buses) == 0 {
		return
	}
	add := func(f Fault) { sc.Faults = append(sc.Faults, f) }
	charger := func() string { return sc.Chargers[rng.Intn(len(sc.Chargers))].ID }
	bus := func() *BusSpec { return &sc.Buses[rng.Intn(len(sc.Buses))] }

	switch profile {
	case ProfileMild:
		from := 200 + rng.Intn(300)
		add(Fault{Kind: FaultChargerFail, Target: charger(), From: from, To: from + 90})
		add(Fault{Kind: FaultSoCNoise, Target: "*", From: 0, To: forever, Value: 3})
		d := 200 + rng.Intn(300)
		add(Fault{Kind: FaultLimitDrop, From: d, To: d + 60, Value: 0.9})
	case ProfileSevere:
		for i := 0; i < 2; i++ {
			from := 150 + rng.Intn(400)
			add(Fault{Kind: FaultChargerFail, Target: charger(), From: from, To: forever})
		}
		from := 150 + rng.Intn(400)
		add(Fault{Kind: FaultChargerOffline, Target: charger(), From: from, To: from + 120})
		d := 200 + rng.Intn(300)
		add(Fault{Kind: FaultLimitDrop, From: d, To: d + 120, Value: 0.6})
		add(Fault{Kind: FaultSoCNoise, Target: "*", From: 0, To: forever, Value: 10})
		for i := range sc.Buses {
			id := sc.Buses[i].Bus.ID
			switch r := rng.Float64(); {
			case r < 0.10:
				f := 300 + rng.Intn(200)
				add(Fault{Kind: FaultSoCFreeze, Target: id, From: f, To: f + 120})
			case r < 0.15:
				f := 300 + rng.Intn(200)
				add(Fault{Kind: FaultSoCMissing, Target: id, From: f, To: f + 60})
			case r < 0.35:
				add(Fault{Kind: FaultConsumption, Target: id, Value: 30})
			}
		}
		for i := 0; i < 3; i++ {
			add(Fault{Kind: FaultLateArrival, Target: bus().Bus.ID, Value: 60})
		}
		for i := 0; i < 3; i++ {
			b := bus()
			newDep := b.Bus.DepartureMin - 45
			add(Fault{Kind: FaultEarlyDeparture, Target: b.Bus.ID, From: newDep - 75, To: forever, Value: float64(newDep)})
		}
		pf := 200 + rng.Intn(400)
		add(Fault{Kind: FaultPlannerPanic, From: pf, To: pf + 5})
		ps := 200 + rng.Intn(400)
		add(Fault{Kind: FaultPlannerSlow, From: ps, To: ps + 2})
	}
}
