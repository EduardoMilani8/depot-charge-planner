package sim

import (
	"encoding/json"
	"io"
	"math"
	"sort"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// DecisionRecord is one planning cycle: what the planner saw and what it decided.
type DecisionRecord struct {
	Minute int           `json:"minute"`
	Input  planner.Input `json:"input"`
	Plan   planner.Plan  `json:"plan"`
}

// DecisionLog writes records as JSON lines and remembers the first write error.
type DecisionLog struct {
	enc *json.Encoder
	err error
}

var _ Recorder = (*DecisionLog)(nil)

// NewDecisionLog returns a Recorder that appends one JSON object per line to w.
func NewDecisionLog(w io.Writer) *DecisionLog { return &DecisionLog{enc: json.NewEncoder(w)} }

func (l *DecisionLog) Record(minute int, in planner.Input, p planner.Plan) error {
	if l.err != nil {
		return l.err
	}
	l.err = l.enc.Encode(DecisionRecord{Minute: minute, Input: in, Plan: p})
	return l.err
}

// Err returns the first write error, if any. Callers must check Err() after Run:
// Run ignores Record errors.
func (l *DecisionLog) Err() error { return l.err }

// ReadDecisionLog parses a JSON-lines decision log, in order, and stops at the first
// malformed record.
func ReadDecisionLog(r io.Reader) ([]DecisionRecord, error) {
	dec := json.NewDecoder(r)
	var out []DecisionRecord
	for {
		var rec DecisionRecord
		err := dec.Decode(&rec)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
}

// ReplayDiff is one charger whose replayed setpoint differs from the logged one at
// Minute. A charger missing from one side counts as 0 kW there.
type ReplayDiff struct {
	Minute    int
	ChargerID string
	Logged    float64
	Replayed  float64
}

// differs reports whether two setpoints are different. It is NaN-safe: a NaN on
// either side always counts as a difference.
func differs(a, b float64) bool { return !(math.Abs(a-b) <= 1e-9) }

// Replay reruns the normal layer on every logged normal-layer decision and reports
// the setpoints that differ. Field problems become reproducible test cases this way.
//
// Records of other layers (last-valid, safe) are skipped, so an empty result is only
// meaningful when at least one record was replayed; use ReplayStats to know.
func Replay(cfg planner.Config, recs []DecisionRecord) []ReplayDiff {
	diffs, _, _ := ReplayStats(cfg, recs)
	return diffs
}

// ReplayStats is Replay plus counts: replayed is the number of normal-layer records
// re-executed and skipped the number of records of any other layer.
func ReplayStats(cfg planner.Config, recs []DecisionRecord) (diffs []ReplayDiff, replayed, skipped int) {
	for _, rec := range recs {
		if rec.Plan.Layer != planner.LayerNormal {
			skipped++
			continue
		}
		replayed++
		got := map[string]float64{}
		for _, s := range planner.Enforce(rec.Input, planner.PlanNormal(cfg, rec.Input)).Setpoints {
			got[s.ChargerID] = s.KW
		}
		logged := map[string]float64{}
		for _, s := range rec.Plan.Setpoints {
			logged[s.ChargerID] = s.KW
		}
		ids := map[string]bool{}
		for id := range logged {
			ids[id] = true
		}
		for id := range got {
			ids[id] = true
		}
		sorted := make([]string, 0, len(ids))
		for id := range ids {
			sorted = append(sorted, id)
		}
		sort.Strings(sorted)
		for _, id := range sorted {
			if differs(logged[id], got[id]) {
				diffs = append(diffs, ReplayDiff{Minute: rec.Minute, ChargerID: id, Logged: logged[id], Replayed: got[id]})
			}
		}
	}
	return diffs, replayed, skipped
}
