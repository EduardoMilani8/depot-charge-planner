package sim

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// cancelAfter cancels its context from inside Plan, on call number n.
type cancelAfter struct {
	inner  Controller
	n      int
	calls  int
	cancel context.CancelFunc
}

func (c *cancelAfter) Plan(in planner.Input) planner.Plan {
	c.calls++
	if c.calls == c.n {
		c.cancel()
	}
	return c.inner.Plan(in)
}

func TestRunContextStopsWhenCancelled(t *testing.T) {
	sc := baseScenario()
	ctx, cancel := context.WithCancel(context.Background())
	ctrl := &cancelAfter{inner: stubController{kw: 100}, n: 10, cancel: cancel}
	tr := NewTrace()
	m, err := RunContext(ctx, sc, ctrl, nil, tr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want context.Canceled", err)
	}
	if ctrl.calls != 10 {
		t.Errorf("the controller was called %d times, want 10 (the run must stop at the next minute)", ctrl.calls)
	}
	if ctrl.calls >= sc.Horizon+1 {
		t.Errorf("ran to the horizon (%d calls)", ctrl.calls)
	}
	if !reflect.DeepEqual(m, Metrics{}) {
		t.Errorf("a cancelled run returned metrics: %+v", m)
	}
}

func TestRunContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctrl := &cancelAfter{inner: stubController{kw: 100}, n: -1}
	if _, err := RunContext(ctx, baseScenario(), ctrl, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if ctrl.calls != 0 {
		t.Errorf("planned %d times on a dead context", ctrl.calls)
	}
}

func TestRunContextMatchesRun(t *testing.T) {
	p := smallParams()
	p.Profile = ProfileSevere
	for _, name := range ControllerNames {
		sc, ctrl, _ := ForController(name, planner.DefaultConfig(), Generate(p, 3))
		want := Run(sc, ctrl, nil)
		sc2, ctrl2, _ := ForController(name, planner.DefaultConfig(), Generate(p, 3))
		got, err := RunContext(context.Background(), sc2, ctrl2, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		want.PlanP99Micros, got.PlanP99Micros = 0, 0
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s: RunContext differs from Run\nwant %+v\ngot  %+v", name, want, got)
		}
	}
}

// A deadline stops Compare long before the work would finish, and leaves no goroutine behind.
func TestCompareDeadlineStopsInFlightRuns(t *testing.T) {
	before := runtime.NumGoroutine()
	p := DefaultGenParams()
	p.NumBuses, p.NumChargers = 400, 200
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := Compare(ctx, p, planner.DefaultConfig(), 40, 4)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) || res != nil {
		t.Fatalf("err %v, results %v", err, res)
	}
	if elapsed > 3*time.Second {
		t.Errorf("Compare took %v to notice a 150 ms deadline", elapsed)
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("%d goroutines after Compare, %d before", n, before)
	}
}
