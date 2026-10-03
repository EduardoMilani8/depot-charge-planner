package sim

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

func TestDecisionLogRoundTripAndReplay(t *testing.T) {
	sc := baseScenario()
	var buf bytes.Buffer
	log := NewDecisionLog(&buf)
	Run(sc, plannerCtrl(sc), log)
	if err := log.Err(); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadDecisionLog(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != sc.Horizon+1 {
		t.Fatalf("got %d records, want %d", len(recs), sc.Horizon+1)
	}
	normal, moving := 0, 0
	for _, rec := range recs {
		if rec.Plan.Layer != planner.LayerNormal {
			continue
		}
		normal++
		for _, s := range rec.Plan.Setpoints {
			if s.KW > 0 {
				moving++
				break
			}
		}
	}
	if normal == 0 {
		t.Fatal("no normal-layer records were logged; the replay check would be vacuous")
	}
	if moving == 0 {
		t.Error("no logged normal record commands any power; the replay check would be vacuous")
	}
	diffs, replayed, skipped := ReplayStats(planner.DefaultConfig(), recs)
	if replayed != normal || replayed+skipped != len(recs) {
		t.Errorf("replayed %d, skipped %d, want replayed %d of %d records", replayed, skipped, normal, len(recs))
	}
	if len(diffs) != 0 {
		t.Errorf("replaying with the same config must reproduce every decision: %+v", diffs[:1])
	}
	if d := Replay(planner.DefaultConfig(), recs); len(d) != 0 {
		t.Errorf("Replay wrapper disagrees with ReplayStats: %+v", d[:1])
	}
}

func TestReplayStatsSkipsNonNormalLayers(t *testing.T) {
	recs := []DecisionRecord{
		{Minute: 0, Plan: planner.Plan{Layer: planner.LayerSafe}},
		{Minute: 1, Plan: planner.Plan{Layer: planner.LayerLastValid}},
	}
	diffs, replayed, skipped := ReplayStats(planner.DefaultConfig(), recs)
	if len(diffs) != 0 || replayed != 0 || skipped != 2 {
		t.Errorf("got %d diffs, replayed %d, skipped %d; want 0, 0, 2", len(diffs), replayed, skipped)
	}
}

func TestReplayStatsEmpty(t *testing.T) {
	diffs, replayed, skipped := ReplayStats(planner.DefaultConfig(), nil)
	if len(diffs) != 0 || replayed != 0 || skipped != 0 {
		t.Errorf("got %d diffs, replayed %d, skipped %d; want all zero", len(diffs), replayed, skipped)
	}
}

func TestDiffersIsNaNSafe(t *testing.T) {
	nan := math.NaN()
	cases := []struct {
		a, b float64
		want bool
	}{
		{1, 1, false}, {1, 1 + 1e-12, false}, {1, 1.1, true},
		{nan, 1, true}, {1, nan, true}, {nan, nan, true},
		{math.Inf(1), math.Inf(1), true}, // Inf-Inf is NaN: reported, never silently equal
	}
	for _, c := range cases {
		if got := differs(c.a, c.b); got != c.want {
			t.Errorf("differs(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestReplayDetectsChangedBehaviour(t *testing.T) {
	sc := baseScenario()
	var buf bytes.Buffer
	log := NewDecisionLog(&buf)
	Run(sc, plannerCtrl(sc), log)
	recs, err := ReadDecisionLog(&buf)
	if err != nil {
		t.Fatal(err)
	}
	cfg := planner.DefaultConfig()
	cfg.MarginKWh = 80 // a different planner
	if diffs := Replay(cfg, recs); len(diffs) == 0 {
		t.Error("a different configuration must produce visible differences")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestDecisionLogKeepsFirstError(t *testing.T) {
	log := NewDecisionLog(failingWriter{})
	if err := log.Record(0, planner.Input{}, planner.Plan{}); err == nil {
		t.Error("expected the write error")
	}
	if log.Err() == nil {
		t.Error("Err() must keep the error")
	}
}
