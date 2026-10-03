package sim

import (
	"bytes"
	"errors"
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
	if diffs := Replay(planner.DefaultConfig(), recs); len(diffs) != 0 {
		t.Errorf("replaying with the same config must reproduce every decision: %+v", diffs[:1])
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
