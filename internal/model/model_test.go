package model

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestTaperFactor(t *testing.T) {
	cases := []struct{ soc, want float64 }{
		{0, 1}, {50, 1}, {80, 1}, {90, 0.6}, {100, 0}, {120, 0},
	}
	for _, c := range cases {
		if got := TaperFactor(c.soc, 100); !near(got, c.want) {
			t.Errorf("TaperFactor(%v,100) = %v, want %v", c.soc, got, c.want)
		}
	}
	if TaperFactor(10, 0) != 0 {
		t.Error("zero capacity must give zero factor")
	}
}

func TestEffectiveEnergy(t *testing.T) {
	cases := []struct{ soc, target, want float64 }{
		{0, 80, 80},
		{80, 100, 40},
		{50, 100, 70},
		{60, 50, 0},
		{90, 100, 20},
	}
	for _, c := range cases {
		if got := EffectiveEnergy(c.soc, c.target, 100); !near(got, c.want) {
			t.Errorf("EffectiveEnergy(%v,%v,100) = %v, want %v", c.soc, c.target, got, c.want)
		}
	}
}

func TestChargerHealthy(t *testing.T) {
	if !(Charger{Status: ChargerOK}).Healthy() {
		t.Error("OK charger should be healthy")
	}
	if (Charger{Status: ChargerOffline}).Healthy() || (Charger{Status: ChargerFaulted}).Healthy() {
		t.Error("offline/faulted chargers must not be healthy")
	}
}
