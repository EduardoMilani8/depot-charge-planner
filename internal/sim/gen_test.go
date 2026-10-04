package sim

import (
	"reflect"
	"testing"
)

func TestGenerateIsDeterministic(t *testing.T) {
	p := DefaultGenParams()
	p.Profile = ProfileSevere
	a, b := Generate(p, 7), Generate(p, 7)
	if !reflect.DeepEqual(a, b) {
		t.Error("same seed must generate the same scenario")
	}
	if reflect.DeepEqual(a, Generate(p, 8)) {
		t.Error("different seeds should differ")
	}
}

func TestGenerateShape(t *testing.T) {
	p := DefaultGenParams()
	sc := Generate(p, 1)
	if len(sc.Buses) != p.NumBuses || len(sc.Chargers) != p.NumChargers {
		t.Fatalf("sizes: %d buses, %d chargers", len(sc.Buses), len(sc.Chargers))
	}
	ids := map[string]bool{}
	for _, b := range sc.Buses {
		if ids[b.Bus.ID] {
			t.Errorf("duplicate bus ID %s", b.Bus.ID)
		}
		ids[b.Bus.ID] = true
		if b.Bus.ArrivalMin >= b.Bus.DepartureMin || b.Bus.DepartureMin >= sc.Horizon {
			t.Errorf("bad times for %s: %d..%d (horizon %d)", b.Bus.ID, b.Bus.ArrivalMin, b.Bus.DepartureMin, sc.Horizon)
		}
		if b.Bus.SoCKWh >= b.Bus.TargetKWh {
			t.Errorf("%s arrives already at its target", b.Bus.ID)
		}
	}
}

func TestProfilesInjectExpectedFaults(t *testing.T) {
	p := DefaultGenParams()
	if got := Generate(p, 1).Faults; len(got) != 0 {
		t.Errorf("none profile must have no faults: %v", got)
	}
	p.Profile = ProfileMild
	if got := Generate(p, 1).Faults; len(got) == 0 {
		t.Error("mild profile must inject faults")
	}
	p.Profile = ProfileSevere
	kinds := map[FaultKind]bool{}
	for _, f := range Generate(p, 1).Faults {
		kinds[f.Kind] = true
	}
	for _, k := range []FaultKind{FaultChargerFail, FaultChargerOffline, FaultLimitDrop, FaultSoCNoise,
		FaultLateArrival, FaultEarlyDeparture, FaultPlannerPanic, FaultPlannerSlow} {
		if !kinds[k] {
			t.Errorf("severe profile is missing %s", k)
		}
	}
}

func TestGenerateDegenerateSizesDoNotPanic(t *testing.T) {
	for _, p := range []GenParams{{Profile: ProfileSevere}, {NumBuses: 3, Profile: ProfileSevere}, {NumChargers: 3, Profile: ProfileSevere},
		{Profile: ProfileRandom}, {NumBuses: 3, NumChargers: 1, Profile: ProfileRandom}} {
		_ = Generate(p, 1)
	}
}

func TestGenerateFollowSwapsComesFromParams(t *testing.T) {
	p := DefaultGenParams()
	if !p.FollowSwaps || !Generate(p, 1).FollowSwaps {
		t.Error("default scenarios assume operators follow the planner's swaps")
	}
	p.FollowSwaps = false
	if Generate(p, 1).FollowSwaps {
		t.Error("FollowSwaps=false must reach the scenario")
	}
}

// The random (fuzz) profile draws a seeded combination of every fault kind with random
// intensity: deterministic per seed, and over a few seeds every kind shows up.
func TestRandomProfileCombinesEveryFaultKind(t *testing.T) {
	p := DefaultGenParams()
	p.Profile = ProfileRandom
	if !reflect.DeepEqual(Generate(p, 3), Generate(p, 3)) {
		t.Fatal("same seed must generate the same random faults")
	}
	kinds := map[FaultKind]bool{}
	distinct := map[int]bool{}
	for seed := int64(1); seed <= 20; seed++ {
		fs := Generate(p, seed).Faults
		distinct[len(fs)] = true
		for _, f := range fs {
			kinds[f.Kind] = true
		}
	}
	for _, k := range []FaultKind{FaultChargerFail, FaultChargerOffline, FaultLimitDrop, FaultSoCNoise,
		FaultSoCBias, FaultSoCFreeze, FaultSoCMissing, FaultConsumption, FaultLateArrival,
		FaultEarlyDeparture, FaultPlannerPanic, FaultPlannerSlow, FaultPlannerGarbage} {
		if !kinds[k] {
			t.Errorf("random profile never injected %s in 20 seeds", k)
		}
	}
	if len(distinct) < 5 {
		t.Errorf("random profile should vary the number of faults across seeds: %v", distinct)
	}
}
