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

func NewDecisionLog(w io.Writer) *DecisionLog { return &DecisionLog{enc: json.NewEncoder(w)} }

func (l *DecisionLog) Record(minute int, in planner.Input, p planner.Plan) error {
	if l.err != nil {
		return l.err
	}
	l.err = l.enc.Encode(DecisionRecord{Minute: minute, Input: in, Plan: p})
	return l.err
}

func (l *DecisionLog) Err() error { return l.err }

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

type ReplayDiff struct {
	Minute    int
	ChargerID string
	Logged    float64
	Replayed  float64
}

// Replay reruns the normal layer on every logged normal-layer decision and reports
// the setpoints that differ. Field problems become reproducible test cases this way.
func Replay(cfg planner.Config, recs []DecisionRecord) []ReplayDiff {
	var diffs []ReplayDiff
	for _, rec := range recs {
		if rec.Plan.Layer != planner.LayerNormal {
			continue
		}
		replayed := planner.Enforce(rec.Input, planner.PlanNormal(cfg, rec.Input))
		logged := map[string]float64{}
		for _, s := range rec.Plan.Setpoints {
			logged[s.ChargerID] = s.KW
		}
		got := map[string]float64{}
		for _, s := range replayed.Setpoints {
			got[s.ChargerID] = s.KW
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
			if math.Abs(logged[id]-got[id]) > 1e-9 {
				diffs = append(diffs, ReplayDiff{Minute: rec.Minute, ChargerID: id, Logged: logged[id], Replayed: got[id]})
			}
		}
	}
	return diffs
}
