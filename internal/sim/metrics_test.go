package sim

import (
	"testing"
	"time"
)

func TestP99Micros(t *testing.T) {
	var d []time.Duration
	for i := 1; i <= 100; i++ {
		d = append(d, time.Duration(i)*time.Microsecond)
	}
	if got := p99Micros(d); got != 99 {
		t.Errorf("p99 = %d, want 99", got)
	}
	if p99Micros(nil) != 0 {
		t.Error("empty must be 0")
	}
}

func TestAggregate(t *testing.T) {
	a := Aggregate([]Metrics{
		{Buses: 10, Ready: 10, ReadyPct: 100, PeakKW: 100, PlanViolations: 1, PlanChanges: 10, PlanP99Micros: 5},
		{Buses: 10, Ready: 5, ReadyPct: 50, PeakKW: 200, PlanViolations: 2, PlanChanges: 20, PlanP99Micros: 9},
	})
	if a.ReadyPct != 75 || a.PeakKW != 150 || a.PlanViolations != 3 || a.PlanChanges != 15 || a.PlanP99Micros != 9 || a.Ready != 15 {
		t.Errorf("unexpected aggregate: %+v", a)
	}
	if z := Aggregate(nil); z.Buses != 0 || z.ReadyPct != 0 {
		t.Error("empty aggregate must be zero")
	}
}
