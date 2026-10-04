package sim

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
)

// fillDistinct sets every exported field reachable from v (structs, slices of one
// element, strings, ints, floats, bools) to a distinct non-zero value, so a round trip
// that drops or swaps any field is caught.
func fillDistinct(v reflect.Value, n *int) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				fillDistinct(v.Field(i), n)
			}
		}
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillDistinct(s.Index(0), n)
		v.Set(s)
	case reflect.String:
		*n++
		v.SetString("s" + strings.Repeat("x", *n))
	case reflect.Int, reflect.Int64:
		*n++
		v.SetInt(int64(*n))
	case reflect.Float64:
		*n++
		v.SetFloat(float64(*n) + 0.25)
	case reflect.Bool:
		v.SetBool(true)
	}
}

func TestDecisionLogRoundTripsEveryField(t *testing.T) {
	var rec DecisionRecord
	n := 0
	fillDistinct(reflect.ValueOf(&rec).Elem(), &n)
	rec.Plan.Layer = planner.LayerSafe
	rec.Input.Chargers[0].Status = model.ChargerOffline
	cfg := planner.Config{}
	fillDistinct(reflect.ValueOf(&cfg).Elem(), &n)

	var buf bytes.Buffer
	log := NewDecisionLogWithConfig(&buf, cfg)
	if err := log.Record(rec.Minute, rec.Input, rec.Plan); err != nil {
		t.Fatal(err)
	}
	h, recs, err := ReadDecisionLogWithHeader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if h == nil || h.Version != DecisionLogVersion || !reflect.DeepEqual(h.Config, cfg) {
		t.Errorf("header lost data: %+v, want config %+v", h, cfg)
	}
	if len(recs) != 1 || !reflect.DeepEqual(recs[0], rec) {
		t.Errorf("record lost data:\n got %+v\nwant %+v", recs, rec)
	}
}

func TestDecisionLogKeepsNonFiniteValues(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	in := planner.Input{
		Now:      3,
		Site:     model.Site{LimitKW: inf, StepMin: 1},
		Chargers: []model.Charger{{ID: "C1", MaxKW: nan, MinKW: math.Inf(-1), Efficiency: 1}},
		Buses:    []model.Bus{{ID: "B1", SoCKWh: nan, TargetKWh: inf, ChargerID: "C1"}},
	}
	plan := planner.Plan{Buses: []planner.BusStatus{{BusID: "B1", LaxityMin: math.Inf(-1), ShortfallKWh: nan}}}
	cfg := planner.DefaultConfig()
	cfg.SurplusLaxityMin = inf

	var buf bytes.Buffer
	log := NewDecisionLogWithConfig(&buf, cfg)
	if err := log.Record(3, in, plan); err != nil {
		t.Fatalf("a NaN/Inf record must not fail: %v", err)
	}
	if err := log.Record(4, validLogInput(), planner.Plan{}); err != nil {
		t.Fatalf("records after a NaN/Inf one must not be lost: %v", err)
	}
	h, recs, err := ReadDecisionLogWithHeader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	got := recs[0]
	if !math.IsInf(got.Input.Site.LimitKW, 1) || !math.IsNaN(got.Input.Chargers[0].MaxKW) ||
		!math.IsInf(got.Input.Chargers[0].MinKW, -1) || !math.IsNaN(got.Input.Buses[0].SoCKWh) ||
		!math.IsInf(got.Input.Buses[0].TargetKWh, 1) || !math.IsInf(got.Plan.Buses[0].LaxityMin, -1) ||
		!math.IsNaN(got.Plan.Buses[0].ShortfallKWh) {
		t.Errorf("non-finite values not preserved: %+v", got)
	}
	if !math.IsInf(h.Config.SurplusLaxityMin, 1) {
		t.Errorf("config Inf not preserved: %v", h.Config.SurplusLaxityMin)
	}
}

func validLogInput() planner.Input {
	return planner.Input{Site: model.Site{LimitKW: 100, StepMin: 1}}
}

func TestReadDecisionLogTruncatedTail(t *testing.T) {
	var buf bytes.Buffer
	log := NewDecisionLogWithConfig(&buf, planner.DefaultConfig())
	for m := 0; m < 3; m++ {
		if err := log.Record(m, validLogInput(), planner.Plan{}); err != nil {
			t.Fatal(err)
		}
	}
	data := buf.Bytes()
	cut := data[:len(data)-15] // the last record loses its end
	h, recs, err := ReadDecisionLogWithHeader(bytes.NewReader(cut))
	if !errors.Is(err, ErrTruncatedLog) {
		t.Errorf("want ErrTruncatedLog, got %v", err)
	}
	if h == nil || len(recs) != 2 || recs[1].Minute != 1 {
		t.Errorf("the complete records must be returned: header %v, %d records", h != nil, len(recs))
	}
	recs2, err := ReadDecisionLog(bytes.NewReader(cut))
	if !errors.Is(err, ErrTruncatedLog) || len(recs2) != 2 {
		t.Errorf("ReadDecisionLog: %d records, err %v", len(recs2), err)
	}
}

func TestReadDecisionLogStopsAtCorruptLine(t *testing.T) {
	var buf bytes.Buffer
	log := NewDecisionLog(&buf)
	_ = log.Record(0, validLogInput(), planner.Plan{})
	buf.WriteString("{not json}\n")
	_ = log.Record(1, validLogInput(), planner.Plan{})
	recs, err := ReadDecisionLog(&buf)
	if err == nil || errors.Is(err, ErrTruncatedLog) || len(recs) != 1 {
		t.Errorf("want the 1 record before the corrupt line and a non-truncation error, got %d, %v", len(recs), err)
	}
}

// Logs written before the header existed (format 1) still read, with a nil header and
// numeric layers.
func TestReadDecisionLogOldFormat(t *testing.T) {
	old := `{"minute":7,"input":{"Now":7,"Site":{"LimitKW":100,"StepMin":1},"Chargers":null,"Buses":null},"plan":{"Layer":2,"Setpoints":[{"ChargerID":"C1","KW":5}],"Buses":null,"Swaps":null,"Notes":null}}` + "\n"
	h, recs, err := ReadDecisionLogWithHeader(strings.NewReader(old))
	if err != nil || h != nil || len(recs) != 1 {
		t.Fatalf("header %v, %d records, err %v", h, len(recs), err)
	}
	if recs[0].Plan.Layer != planner.LayerSafe || recs[0].Plan.Setpoints[0].KW != 5 {
		t.Errorf("old record misread: %+v", recs[0])
	}
}

func TestDecisionLogWritesLayerByName(t *testing.T) {
	var buf bytes.Buffer
	_ = NewDecisionLog(&buf).Record(0, validLogInput(), planner.Plan{Layer: planner.LayerLastValid})
	if !strings.Contains(buf.String(), `"Layer":"last-valid"`) {
		t.Errorf("layer should be written by name: %s", buf.String())
	}
}

// A log with a header replays without the caller supplying the configuration.
func TestReplayFileUsesTheLoggedConfig(t *testing.T) {
	sc := baseScenario()
	cfg := planner.DefaultConfig()
	cfg.MarginKWh = 40 // differs from DefaultConfig: replaying with the default would differ
	cfg.Timeout = time.Second
	var buf bytes.Buffer
	log := NewDecisionLogWithConfig(&buf, cfg)
	Run(sc, NewPlannerController(cfg, sc), log)
	if err := log.Err(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	diffs, replayed, _, err := ReplayFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if replayed == 0 || len(diffs) != 0 {
		t.Errorf("replayed %d records, %d diffs (want >0 and 0)", replayed, len(diffs))
	}
	_, recs, _ := ReadDecisionLogWithHeader(bytes.NewReader(data))
	if d := Replay(planner.DefaultConfig(), recs); len(d) == 0 {
		t.Error("control: the default config must differ from the logged one")
	}
	if _, _, _, err := ReplayFile(strings.NewReader("")); err == nil {
		t.Error("a log without header cannot be replayed without a config")
	}
}

// A log whose last line was cut (e.g. the process died mid-write) still replays every
// complete record, and the truncation is reported.
func TestReplayFileReplaysTheCompleteRecordsOfATruncatedLog(t *testing.T) {
	sc := baseScenario()
	cfg := planner.DefaultConfig()
	cfg.Timeout = time.Second
	var buf bytes.Buffer
	log := NewDecisionLogWithConfig(&buf, cfg)
	Run(sc, NewPlannerController(cfg, sc), log)
	data := buf.Bytes()
	cut := data[:len(data)-15]
	_, recs, _ := ReadDecisionLogWithHeader(bytes.NewReader(cut))
	_, want, _ := ReplayStats(cfg, recs)
	diffs, replayed, _, err := ReplayFile(bytes.NewReader(cut))
	if !errors.Is(err, ErrTruncatedLog) {
		t.Errorf("want ErrTruncatedLog, got %v", err)
	}
	if want == 0 || replayed != want || len(diffs) != 0 {
		t.Errorf("replayed %d records (want %d > 0), %d diffs", replayed, want, len(diffs))
	}
}
