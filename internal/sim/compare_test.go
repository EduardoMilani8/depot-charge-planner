package sim

import (
	"context"
	"reflect"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func smallParams() GenParams {
	p := DefaultGenParams()
	p.NumBuses, p.NumChargers, p.LimitKW = 10, 5, 500
	p.Profile = ProfileMild
	return p
}

func zeroP99(ms ...*Metrics) {
	for _, m := range ms {
		m.PlanP99Micros = 0
	}
}

func TestCompareOrderAndShape(t *testing.T) {
	res, err := Compare(context.Background(), smallParams(), planner.DefaultConfig(), 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != len(ControllerNames) {
		t.Fatalf("%d controllers, want %d", len(res), len(ControllerNames))
	}
	for i, r := range res {
		if r.Name != ControllerNames[i] {
			t.Errorf("controller %d is %q, want %q", i, r.Name, ControllerNames[i])
		}
		if len(r.Seeds) != 3 {
			t.Errorf("%s: %d seeds, want 3", r.Name, len(r.Seeds))
		}
		for j, s := range r.Seeds {
			if s.Seed != int64(j+1) {
				t.Errorf("%s: seed %d at index %d", r.Name, s.Seed, j)
			}
		}
	}
}

// The worker count must not change any result except the wall-clock p99.
func TestCompareIsIndependentOfWorkers(t *testing.T) {
	cfg := planner.DefaultConfig()
	a, err := Compare(context.Background(), smallParams(), cfg, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Compare(context.Background(), smallParams(), cfg, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		zeroP99(&a[i].Aggregate, &b[i].Aggregate)
		for j := range a[i].Seeds {
			zeroP99(&a[i].Seeds[j].Metrics, &b[i].Seeds[j].Metrics)
		}
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("results differ between 1 and 4 workers:\n%+v\n%+v", a, b)
	}
}

// Compare must give exactly what a plain loop of Run gives (what simrun printed before).
func TestCompareMatchesPlainRun(t *testing.T) {
	cfg := planner.DefaultConfig()
	p := smallParams()
	res, err := Compare(context.Background(), p, cfg, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		var ms []Metrics
		for seed := int64(1); seed <= 3; seed++ {
			sc, ctrl, err := ForController(r.Name, cfg, Generate(p, seed))
			if err != nil {
				t.Fatal(err)
			}
			ms = append(ms, Run(sc, ctrl, nil))
		}
		want := Aggregate(ms)
		got := r.Aggregate
		zeroP99(&want, &got)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s: aggregate differs\nwant %+v\ngot  %+v", r.Name, want, got)
		}
	}
}

func TestCompareHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Compare(ctx, smallParams(), planner.DefaultConfig(), 50, 2); err == nil {
		t.Fatal("a cancelled context must return an error")
	}
}

func TestForControllerUnknownName(t *testing.T) {
	if _, _, err := ForController("nope", planner.DefaultConfig(), Scenario{}); err == nil {
		t.Fatal("unknown controller must be an error")
	}
	sc, _, err := ForController("fifo-unplug", planner.DefaultConfig(), Scenario{})
	if err != nil || !sc.UnplugFull {
		t.Fatalf("fifo-unplug must set UnplugFull: %v %v", sc.UnplugFull, err)
	}
}
