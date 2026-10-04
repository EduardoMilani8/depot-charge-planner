package sim

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// DecisionRecord is one planning cycle: what the planner saw and what it decided.
type DecisionRecord struct {
	Minute int
	Input  planner.Input
	Plan   planner.Plan
}

// Decision log format (JSON lines):
//
//   - Version 2 (current) starts with a header line
//     {"format":"depot-charge-planner/decision-log","version":2,"config":{...}} holding
//     the planner Config, followed by one record per line:
//     {"minute":..,"input":{..},"plan":{..}}.
//   - Version 1 (older logs) has no header; its records read the same way.
//
// Config fields added later are additive and keep version 2: a header written before
// them reads them as 0. In particular SwapBackCooldownMin and SwapBackMinNeedKWh read
// as 0 from older logs, which means the anti-thrash rule off (setpoints, the only
// thing Replay compares, do not depend on it).
//
// Inside records, field names are the Go field names of planner.Input and
// planner.Plan. Floats that JSON cannot represent are written as the strings "NaN",
// "+Inf" and "-Inf", so NaN and infinite readings round-trip exactly. Plan.Layer is
// written by name ("normal", "last-valid", "safe"); the numeric form of version 1
// (0 = normal, 1 = last-valid, 2 = safe) is still accepted when reading.
const (
	DecisionLogFormat  = "depot-charge-planner/decision-log"
	DecisionLogVersion = 2
)

// ErrTruncatedLog means the log ends in the middle of a line (e.g. the writer was
// killed). The records before that line are still returned.
var ErrTruncatedLog = errors.New("registro de decisões truncado: a última linha está incompleta")

// DecisionLogHeader is the first line of a version-2 log.
type DecisionLogHeader struct {
	Format  string
	Version int
	Config  planner.Config
}

// DecisionLog writes records as JSON lines and remembers the first write error.
type DecisionLog struct {
	w   io.Writer
	err error
}

var _ Recorder = (*DecisionLog)(nil)

// NewDecisionLog returns a Recorder that appends one JSON object per line to w,
// without a header (replaying it needs the planner Config from elsewhere).
func NewDecisionLog(w io.Writer) *DecisionLog { return &DecisionLog{w: w} }

// NewDecisionLogWithConfig writes a header with the format version and cfg first, so
// the log can be replayed on its own (ReplayFile). A header write error is kept in Err.
func NewDecisionLogWithConfig(w io.Writer, cfg planner.Config) *DecisionLog {
	l := &DecisionLog{w: w}
	l.writeLine(logHeader{Format: DecisionLogFormat, Version: DecisionLogVersion, Config: toLogConfig(cfg)})
	return l
}

func (l *DecisionLog) writeLine(v any) error {
	if l.err != nil {
		return l.err
	}
	b, err := json.Marshal(v)
	if err == nil {
		_, err = l.w.Write(append(b, '\n'))
	}
	l.err = err
	return err
}

// Record writes one cycle. Non-finite numbers never make it fail; only write errors do.
func (l *DecisionLog) Record(minute int, in planner.Input, p planner.Plan) error {
	return l.writeLine(logRecord{Minute: minute, Input: toLogInput(in), Plan: toLogPlan(p)})
}

// Err returns the first write error, if any. Callers must check Err() after Run:
// Run ignores Record errors.
func (l *DecisionLog) Err() error { return l.err }

// ReadDecisionLog parses a decision log (any version), in order, skipping the header.
// It stops at the first malformed line and returns the records read before it with
// the error; a last line cut short gives an error wrapping ErrTruncatedLog.
func ReadDecisionLog(r io.Reader) ([]DecisionRecord, error) {
	_, recs, err := ReadDecisionLogWithHeader(r)
	return recs, err
}

// ReadDecisionLogWithHeader is ReadDecisionLog that also returns the header (nil for
// version-1 logs, which have none).
func ReadDecisionLogWithHeader(r io.Reader) (*DecisionLogHeader, []DecisionRecord, error) {
	br := bufio.NewReader(r)
	var header *DecisionLogHeader
	var out []DecisionRecord
	for line := 1; ; line++ {
		raw, readErr := br.ReadBytes('\n')
		if readErr != nil && readErr != io.EOF {
			return header, out, readErr
		}
		complete := len(raw) > 0 && raw[len(raw)-1] == '\n'
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 {
			var probe struct {
				Format string `json:"format"`
			}
			err := json.Unmarshal(raw, &probe)
			switch {
			case err != nil && !complete:
				return header, out, fmt.Errorf("linha %d: %w", line, ErrTruncatedLog)
			case err != nil:
				return header, out, fmt.Errorf("linha %d: %w", line, err)
			case probe.Format != "":
				var h logHeader
				if err := json.Unmarshal(raw, &h); err != nil {
					return header, out, fmt.Errorf("linha %d (cabeçalho): %w", line, err)
				}
				if h.Format != DecisionLogFormat || h.Version > DecisionLogVersion {
					return header, out, fmt.Errorf("linha %d: formato não suportado %q versão %d", line, h.Format, h.Version)
				}
				header = &DecisionLogHeader{Format: h.Format, Version: h.Version, Config: h.Config.toConfig()}
			default:
				var rec logRecord
				if err := json.Unmarshal(raw, &rec); err != nil {
					return header, out, fmt.Errorf("linha %d: %w", line, err)
				}
				out = append(out, DecisionRecord{Minute: rec.Minute, Input: rec.Input.toInput(), Plan: rec.Plan.toPlan()})
			}
		}
		if readErr == io.EOF {
			return header, out, nil
		}
	}
}

// ReplayFile reads a version-2 log and replays it with the Config from its header
// (see ReplayStats). A log without header returns an error: use Replay with the
// configuration that produced it. If the last line was cut short, every complete
// record is still replayed and the error (wrapping ErrTruncatedLog) is returned with
// the results; any other read error returns no results.
func ReplayFile(r io.Reader) (diffs []ReplayDiff, replayed, skipped int, err error) {
	h, recs, err := ReadDecisionLogWithHeader(r)
	if err != nil && !errors.Is(err, ErrTruncatedLog) {
		return nil, 0, 0, err
	}
	if h == nil {
		return nil, 0, 0, errors.Join(errors.New("registro de decisões sem cabeçalho: informe a configuração (Replay)"), err)
	}
	diffs, replayed, skipped = ReplayStats(h.Config, recs)
	return diffs, replayed, skipped, err
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
//
// Each record is re-planned by a fresh planner.New(cfg) (so bad records are sanitized
// exactly as in the field) with a generous timeout, so a slow machine does not turn a
// replay into a timeout fallback. Only setpoints are compared. They depend on the
// record's input alone; the swap recommendations also depend on the Planner's memory
// of earlier cycles (anti-thrash: the buses it told to give way and their readings
// since), which a fresh Planner does not have, so swaps are not reproduced from a log.
func ReplayStats(cfg planner.Config, recs []DecisionRecord) (diffs []ReplayDiff, replayed, skipped int) {
	cfg.Timeout = time.Minute
	for _, rec := range recs {
		if rec.Plan.Layer != planner.LayerNormal {
			skipped++
			continue
		}
		replayed++
		got := map[string]float64{}
		for _, s := range planner.New(cfg).Plan(rec.Input).Setpoints {
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
