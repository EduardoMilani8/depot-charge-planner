package lab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/realdata"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

const demoDir = "../realdata/testdata/demo"

// demoFiles reads the fictitious depot of the realdata package (2 nights).
func demoFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, n := range realdata.FileNames {
		b, err := os.ReadFile(filepath.Join(demoDir, n))
		if err != nil {
			t.Fatal(err)
		}
		files[n] = string(b)
	}
	return files
}

// body builds a request body: {"files": files, ...extra}.
func body(t *testing.T, files map[string]string, extra map[string]any) string {
	t.Helper()
	m := map[string]any{"files": files}
	for k, v := range extra {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type errBody struct {
	Error  string `json:"error"`
	Field  string `json:"field"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column string `json:"column"`
}

func TestImportDemo(t *testing.T) {
	rec := do(New(), newReq("POST", "/api/import", body(t, demoFiles(t), nil)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r struct {
		Nights []struct {
			Key          string  `json:"key"`
			Buses        int     `json:"buses"`
			WithOutcome  int     `json:"with_outcome"`
			Ready        int     `json:"ready"`
			RealReadyPct float64 `json:"real_ready_pct"`
			HasOutcome   bool    `json:"has_outcome"`
		} `json:"nights"`
		Warnings    []realdata.Warning `json:"warnings"`
		HasSessions bool               `json:"has_sessions"`
		HasPower    bool               `json:"has_power"`
		HasTariff   bool               `json:"has_tariff"`
	}
	decode(t, rec, &r)
	if len(r.Nights) != 2 || r.Nights[0].Key != "2026-03-04" || r.Nights[1].Key != "2026-03-05" {
		t.Fatalf("nights: %+v", r.Nights)
	}
	if n := r.Nights[0]; n.Buses != 3 || n.WithOutcome != 3 || !n.HasOutcome || n.Ready != 2 || n.RealReadyPct < 66.6 || n.RealReadyPct > 66.7 {
		t.Errorf("night 1: %+v", n)
	}
	if n := r.Nights[1]; n.Buses != 3 || n.WithOutcome != 2 || !n.HasOutcome {
		t.Errorf("night 2: %+v", n)
	}
	if !r.HasSessions || !r.HasPower || !r.HasTariff {
		t.Errorf("flags: sessions %v power %v tariff %v", r.HasSessions, r.HasPower, r.HasTariff)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"warnings":[`)) {
		t.Errorf("warnings must be a JSON array, never null: %.300s", rec.Body)
	}
}

func TestImportOnlyRequiredFiles(t *testing.T) {
	files := demoFiles(t)
	delete(files, "sessoes.csv")
	delete(files, "potencia.csv")
	rec := do(New(), newReq("POST", "/api/import", body(t, files, nil)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r struct {
		HasSessions bool `json:"has_sessions"`
		HasPower    bool `json:"has_power"`
	}
	decode(t, rec, &r)
	if r.HasSessions || r.HasPower {
		t.Errorf("flags: %+v", r)
	}
}

func TestImportCorruptedBusFileGivesFileLineColumn(t *testing.T) {
	files := demoFiles(t)
	files["onibus.csv"] = strings.Replace(files["onibus.csv"], "2026-03-04 20:00,30,", "2026-03-04 20:00,120,", 1)
	for _, route := range []string{"/api/import", "/api/replay"} {
		rec := do(New(), newReq("POST", route, body(t, files, nil)))
		if rec.Code != 400 {
			t.Fatalf("%s: status %d: %s", route, rec.Code, rec.Body)
		}
		var e errBody
		decode(t, rec, &e)
		if e.File != "onibus.csv" || e.Field != "onibus.csv" || e.Line != 2 || e.Column != "soc_chegada_pct" {
			t.Errorf("%s: %+v", route, e)
		}
		if !strings.Contains(e.Error, "soc_chegada_pct") || !strings.Contains(e.Error, "linha 2") {
			t.Errorf("%s: message %q", route, e.Error)
		}
	}
	// the same through /api/run
	rec := do(New(), newReq("POST", "/api/run", body(t, files, map[string]any{"night": "2026-03-04"})))
	var e errBody
	decode(t, rec, &e)
	if rec.Code != 400 || e.File != "onibus.csv" || e.Line != 2 || e.Column != "soc_chegada_pct" {
		t.Errorf("run: %d %+v", rec.Code, e)
	}
}

func TestImportErrorsAreBadRequests(t *testing.T) {
	missing := demoFiles(t)
	delete(missing, "onibus.csv")
	empty := demoFiles(t)
	empty["garagem.csv"] = ""
	for name, b := range map[string]string{
		"no files":          `{}`,
		"empty files":       `{"files":{}}`,
		"missing onibus":    body(t, missing, nil),
		"empty garagem":     body(t, empty, nil),
		"unknown JSON key":  `{"files":{},"extra":1}`,
		"number as content": `{"files":{"garagem.csv":5}}`,
		"files as list":     `{"files":["a"]}`,
		"broken JSON":       `{"files":`,
	} {
		rec := do(New(), newReq("POST", "/api/import", b))
		if rec.Code != 400 {
			t.Errorf("%s: status %d: %.200s", name, rec.Code, rec.Body)
			continue
		}
		var e errBody
		decode(t, rec, &e)
		if e.Error == "" {
			t.Errorf("%s: no message", name)
		}
	}
	rec := do(New(), newReq("POST", "/api/import", body(t, missing, nil)))
	var e errBody
	decode(t, rec, &e)
	if e.Field != "onibus.csv" || e.File != "onibus.csv" {
		t.Errorf("missing file: %+v", e)
	}
}

func TestImportWarnsAboutUnknownFileNames(t *testing.T) {
	files := demoFiles(t)
	files["../../etc/passwd"] = "root"
	files["notas.txt"] = "x"
	rec := do(New(), newReq("POST", "/api/import", body(t, files, nil)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r struct {
		Warnings []realdata.Warning `json:"warnings"`
	}
	decode(t, rec, &r)
	seen := map[string]bool{}
	for _, w := range r.Warnings {
		seen[w.File] = true
	}
	if !seen["notas.txt"] || !seen["../../etc/passwd"] {
		t.Errorf("warnings: %+v", r.Warnings)
	}
}

func TestImportLimitsTheNumberAndLengthOfFileNames(t *testing.T) {
	files := demoFiles(t)
	for i := 0; i < 40; i++ {
		files[fmt.Sprintf("extra%02d.csv", i)] = "x"
	}
	rec := do(New(), newReq("POST", "/api/import", body(t, files, nil)))
	var e errBody
	decode(t, rec, &e)
	if rec.Code != 400 || e.Field != "files" {
		t.Fatalf("too many files: %d %+v", rec.Code, e)
	}
	files = demoFiles(t)
	files[strings.Repeat("n", 5000)] = "x"
	rec = do(New(), newReq("POST", "/api/import", body(t, files, nil)))
	if rec.Code != 200 {
		t.Fatalf("long name: %d %.200s", rec.Code, rec.Body)
	}
	if rec.Body.Len() > 4000 {
		t.Errorf("the long file name was echoed in full (%d bytes)", rec.Body.Len())
	}
}

func TestBigRoutesAccept16MBAndOthersStay64KB(t *testing.T) {
	// A body above 64 KB is fine on the three routes (1 MB of an ignored file).
	files := demoFiles(t)
	files["notas.txt"] = strings.Repeat("a", 1<<20)
	if rec := do(New(), newReq("POST", "/api/import", body(t, files, nil))); rec.Code != 200 {
		t.Fatalf("1 MB import: %d %.200s", rec.Code, rec.Body)
	}
	// 17 MB: 413 naming 16 MB.
	huge := `{"files":{"onibus.csv":"` + strings.Repeat("a", 17<<20) + `"}}`
	for _, route := range []string{"/api/import", "/api/replay", "/api/run"} {
		rec := do(New(), newReq("POST", route, huge))
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s: status %d, want 413", route, rec.Code)
		}
		var e errBody
		decode(t, rec, &e)
		if !strings.Contains(e.Error, "16 MB") {
			t.Errorf("%s: message %q", route, e.Error)
		}
	}
	// Other routes (/api/run takes 16 MB for every request, with or without files): 70 KB is
	// still too much, and the message still says 64 KB.
	small := `{"buses":10` + strings.Repeat(" ", 70<<10) + `}`
	for _, route := range []string{"/api/compare"} {
		rec := do(New(), newReq("POST", route, small))
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s: status %d, want 413", route, rec.Code)
		}
		var e errBody
		decode(t, rec, &e)
		if !strings.Contains(e.Error, "64 KB") || strings.Contains(e.Error, "16 MB") {
			t.Errorf("%s: message %q", route, e.Error)
		}
	}
	// Trailing whitespace after the object counts against the route's limit too.
	trailing := `{}` + strings.Repeat(" ", 70<<10)
	rec := do(New(), newReq("POST", "/api/compare", trailing))
	var e errBody
	decode(t, rec, &e)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(e.Error, "64 KB") {
		t.Errorf("trailing: %d %q", rec.Code, e.Error)
	}
}

func TestRealRoutesMethodAndContentType(t *testing.T) {
	for _, route := range []string{"/api/import", "/api/replay"} {
		rec := do(New(), newReq("GET", route, ""))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
			t.Errorf("GET %s: %d allow %q", route, rec.Code, rec.Header().Get("Allow"))
		}
		req := newReq("POST", route, body(t, demoFiles(t), nil))
		req.Header.Set("Content-Type", "text/plain")
		if rec := do(New(), req); rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("text/plain %s: %d", route, rec.Code)
		}
	}
}

func TestReplayDemo(t *testing.T) {
	s := New()
	b := body(t, demoFiles(t), nil)
	rec := do(s, newReq("POST", "/api/replay", b))
	if rec.Code != 200 {
		t.Fatalf("status %d: %.300s", rec.Code, rec.Body)
	}
	var r struct {
		Nights []struct {
			Key         string `json:"key"`
			Buses       int    `json:"buses"`
			Controllers []struct {
				Name      string      `json:"name"`
				Aggregate sim.Metrics `json:"aggregate"`
				Seeds     []struct {
					Seed    int64       `json:"seed"`
					Metrics sim.Metrics `json:"metrics"`
				} `json:"seeds"`
			} `json:"controllers"`
			PerBus []struct {
				ID string `json:"id"`
			} `json:"per_bus"`
			Real struct {
				Buses int `json:"buses"`
			} `json:"real"`
		} `json:"nights"`
		Aggregate []struct {
			Name      string      `json:"name"`
			Aggregate sim.Metrics `json:"aggregate"`
		} `json:"aggregate"`
		RealAggregate  struct{ Buses int } `json:"real_aggregate"`
		CostComparable bool                `json:"cost_comparable"`
		Assumptions    []string            `json:"assumptions"`
		Warnings       []realdata.Warning  `json:"warnings"`
	}
	decode(t, rec, &r)
	if len(r.Nights) != 2 || r.Nights[0].Key != "2026-03-04" {
		t.Fatalf("nights: %+v", r.Nights)
	}
	for _, n := range r.Nights {
		if len(n.Controllers) != len(realdata.ReplayNames) {
			t.Fatalf("night %s: %d controllers", n.Key, len(n.Controllers))
		}
		for i, c := range n.Controllers {
			if c.Name != realdata.ReplayNames[i] {
				t.Errorf("night %s controller %d: %s", n.Key, i, c.Name)
			}
			if c.Aggregate.PlanP99Micros != 0 || len(c.Seeds) != 1 || c.Seeds[0].Metrics.PlanP99Micros != 0 {
				t.Errorf("night %s %s: p99 not zeroed", n.Key, c.Name)
			}
		}
		if len(n.PerBus) != n.Buses || n.Real.Buses != n.Buses {
			t.Errorf("night %s: %d rows for %d buses", n.Key, len(n.PerBus), n.Buses)
		}
	}
	if len(r.Aggregate) != len(realdata.ReplayNames) {
		t.Fatalf("aggregate has %d rows", len(r.Aggregate))
	}
	for _, c := range r.Aggregate {
		if c.Aggregate.PlanP99Micros != 0 {
			t.Errorf("aggregate %s: p99 %v", c.Name, c.Aggregate.PlanP99Micros)
		}
	}
	if r.RealAggregate.Buses != 6 || !r.CostComparable || len(r.Assumptions) == 0 {
		t.Errorf("real aggregate %+v cost comparable %v assumptions %d", r.RealAggregate, r.CostComparable, len(r.Assumptions))
	}
	if !strings.Contains(rec.Body.String(), `"warnings":[`) {
		t.Error("warnings must be an array")
	}
	// determinism: the same request gives the same bytes (p99 included, since it is zeroed)
	again := do(s, newReq("POST", "/api/replay", b))
	if !bytes.Equal(rec.Body.Bytes(), again.Body.Bytes()) {
		t.Fatal("two replays of the same files differ")
	}
	// and the planner never breaks the limit on the real nights
	for _, n := range r.Nights {
		for _, c := range n.Controllers {
			if c.Name == "planner" && c.Aggregate.PlanViolations != 0 {
				t.Errorf("night %s: planner violated the limit", n.Key)
			}
		}
	}
}

func TestReplaySoCNoiseChangesTheResult(t *testing.T) {
	s := New()
	plain := do(s, newReq("POST", "/api/replay", body(t, demoFiles(t), nil)))
	noisy := do(s, newReq("POST", "/api/replay", body(t, demoFiles(t), map[string]any{"soc_noise_kwh": 30})))
	if plain.Code != 200 || noisy.Code != 200 {
		t.Fatalf("status %d / %d: %.200s", plain.Code, noisy.Code, noisy.Body)
	}
	if bytes.Equal(plain.Body.Bytes(), noisy.Body.Bytes()) {
		t.Fatal("soc_noise_kwh did not change the response")
	}
	if !strings.Contains(noisy.Body.String(), "ruído de 30 kWh") {
		t.Errorf("the assumptions do not mention the noise: %.300s", noisy.Body)
	}
	// noisy replays are reproducible too
	again := do(s, newReq("POST", "/api/replay", body(t, demoFiles(t), map[string]any{"soc_noise_kwh": 30})))
	if !bytes.Equal(noisy.Body.Bytes(), again.Body.Bytes()) {
		t.Fatal("two noisy replays differ")
	}
}

func TestReplaySwapBackParametersAreUsedAndValidated(t *testing.T) {
	for _, c := range []struct {
		extra map[string]any
		field string
	}{
		{map[string]any{"soc_noise_kwh": -1}, "soc_noise_kwh"},
		{map[string]any{"soc_noise_kwh": 2e7}, "soc_noise_kwh"},
		{map[string]any{"swap_back_cooldown_min": -5}, "swap_back_cooldown_min"},
		{map[string]any{"swap_back_min_need_kwh": -1}, "swap_back_min_need_kwh"},
		{map[string]any{"swap_back_min_need_kwh": 2e7}, "swap_back_min_need_kwh"},
	} {
		rec := do(New(), newReq("POST", "/api/replay", body(t, demoFiles(t), c.extra)))
		var e errBody
		decode(t, rec, &e)
		if rec.Code != 400 || e.Field != c.field || e.Error == "" {
			t.Errorf("%v: %d %+v", c.extra, rec.Code, e)
		}
	}
	for name, b := range map[string]string{
		"unknown key":  `{"files":{},"seeds":3}`,
		"string noise": `{"files":{},"soc_noise_kwh":"x"}`,
		"huge noise":   `{"files":{},"soc_noise_kwh":1e999}`,
		"float cool":   `{"files":{},"swap_back_cooldown_min":1.5}`,
	} {
		if rec := do(New(), newReq("POST", "/api/replay", b)); rec.Code != 400 {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
	// Explicit defaults give the same answer as leaving them out.
	s := New()
	d := defaultParams()
	a := do(s, newReq("POST", "/api/replay", body(t, demoFiles(t), nil)))
	b := do(s, newReq("POST", "/api/replay", body(t, demoFiles(t), map[string]any{
		"soc_noise_kwh": 0, "swap_back_cooldown_min": d.SwapBackCooldownMin, "swap_back_min_need_kwh": d.SwapBackMinNeedKWh})))
	if !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
		t.Error("explicit defaults changed the response")
	}
}

func TestReplayWorkLimit(t *testing.T) {
	if e := replayWorkError(labMaxWork); e != nil {
		t.Errorf("%d buses refused: %+v", labMaxWork, e)
	}
	e := replayWorkError(labMaxWork + 1)
	if e == nil || e.status != 400 || !strings.HasPrefix(e.Message, "Trabalho demais") {
		t.Errorf("above the limit: %+v", e)
	}
}

func TestRealRoutesBusyAnswer503(t *testing.T) {
	s := New()
	for i := 0; i < cap(s.sem); i++ {
		s.sem <- struct{}{}
	}
	files := demoFiles(t)
	reqs := map[string]string{
		"/api/import": body(t, files, nil),
		"/api/replay": body(t, files, nil),
		"/api/run":    body(t, files, map[string]any{"night": "2026-03-04"}),
	}
	for route, b := range reqs {
		if rec := do(s, newReq("POST", route, b)); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d, want 503", route, rec.Code)
		}
	}
	for i := 0; i < cap(s.sem); i++ {
		<-s.sem
	}
	for route, b := range reqs {
		if rec := do(s, newReq("POST", route, b)); rec.Code != 200 {
			t.Errorf("%s after releasing: %d %.200s", route, rec.Code, rec.Body)
		}
	}
}

func TestRealRoutesTimeoutAnswers504(t *testing.T) {
	s := New()
	s.timeout = time.Nanosecond
	for route, b := range map[string]string{
		"/api/replay": body(t, demoFiles(t), nil),
		"/api/run":    body(t, demoFiles(t), map[string]any{"night": "2026-03-04"}),
	} {
		rec := do(s, newReq("POST", route, b))
		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("%s: status %d, want 504: %.200s", route, rec.Code, rec.Body)
		}
		var e errBody
		decode(t, rec, &e)
		if !strings.HasPrefix(e.Error, "Tempo esgotado") {
			t.Errorf("%s: message %q", route, e.Error)
		}
	}
	if len(s.sem) != 0 {
		t.Error("a slot was not released")
	}
}

func TestReplayErrorMapping(t *testing.T) {
	if e := replayError(context.DeadlineExceeded); e.status != 504 || !strings.HasPrefix(e.Message, "Tempo esgotado") {
		t.Errorf("deadline: %+v", e)
	}
	if e := replayError(context.Canceled); e.status != http.StatusRequestTimeout {
		t.Errorf("canceled: %+v", e)
	}
	e := replayError(errors.New("open /home/secret/x: boom"))
	if e.status != 500 || strings.Contains(e.Message, "secret") || strings.Contains(e.Message, "boom") {
		t.Errorf("other error leaks or is not 500: %+v", e)
	}
	if e := loadError(errors.New("open /home/secret/x: boom")); e.status != 400 || strings.Contains(e.Message, "secret") {
		t.Errorf("generic load error: %+v", e)
	}
}

// ---- /api/run with files ----

func runReal(t *testing.T, s *Server, controller string, extra map[string]any) (*runRespReal, []byte) {
	t.Helper()
	m := map[string]any{"night": "2026-03-04", "controller": controller}
	for k, v := range extra {
		m[k] = v
	}
	rec := do(s, newReq("POST", "/api/run", body(t, demoFiles(t), m)))
	if rec.Code != 200 {
		t.Fatalf("%s: status %d: %.300s", controller, rec.Code, rec.Body)
	}
	var r runRespReal
	decode(t, rec, &r)
	return &r, rec.Body.Bytes()
}

type runRespReal struct {
	runResp
	Source string `json:"source"`
	Params Params `json:"params"`
}

func TestRunRealNightEveryController(t *testing.T) {
	s := New()
	names := append(append([]string{}, sim.ControllerNames...), "planner (sem rodízio)")
	for _, name := range names {
		r, _ := runReal(t, s, name, nil)
		if r.Source != "Dados reais, noite 2026-03-04" {
			t.Errorf("%s: source %q", name, r.Source)
		}
		if r.Controller != name || r.Seed != 1 {
			t.Errorf("%s: echoed controller %q seed %d", name, r.Controller, r.Seed)
		}
		n := r.Scenario.HorizonMin + 1
		if len(r.Outcomes) != 3 || len(r.Series.Buses) != 3 || len(r.Series.Chargers) != 3 || len(r.Scenario.Chargers) != 3 {
			t.Fatalf("%s: %d outcomes, %d buses, %d chargers", name, len(r.Outcomes), len(r.Series.Buses), len(r.Series.Chargers))
		}
		if len(r.Series.LimitKW) != n || len(r.Series.PhysicalKW) != n || len(r.Series.Layer) != n {
			t.Errorf("%s: series lengths %d/%d/%d, want %d", name, len(r.Series.LimitKW), len(r.Series.PhysicalKW), len(r.Series.Layer), n)
		}
		for _, b := range r.Series.Buses {
			if len(b.State) != n || len(b.Charger) != n {
				t.Errorf("%s: bus %s series length", name, b.ID)
			}
		}
		if r.Metrics.PlanP99Micros != 0 {
			t.Errorf("%s: p99 %v", name, r.Metrics.PlanP99Micros)
		}
		if r.Params.Buses != 3 || r.Params.Chargers != 3 || r.Params.LimitKW != 400 || r.Params.Profile != "none" || r.Params.Seeds != 1 {
			t.Errorf("%s: params %+v", name, r.Params)
		}
		if err := r.Params.validate(); err != nil {
			t.Errorf("%s: the echoed params are not valid for the front end: %+v", name, err)
		}
		if wantFollow := name != "planner (sem rodízio)"; r.Params.FollowSwaps != wantFollow {
			t.Errorf("%s: follow_swaps %v", name, r.Params.FollowSwaps)
		}
	}
}

func TestRunRealNightMatchesTheDemoNight(t *testing.T) {
	r, raw := runReal(t, New(), "planner", nil)
	// the demo's first night: three 300 kWh buses
	byID := map[string]struct{ capKWh, initial, target float64 }{
		"B01": {300, 90, 270}, "B02": {300, 120, 255}, "B03": {300, 60, 270},
	}
	var full struct {
		Outcomes []struct {
			ID                string  `json:"id"`
			CapacityKWh       float64 `json:"capacity_kwh"`
			InitialSoCKWh     float64 `json:"initial_soc_kwh"`
			TrueTargetKWh     float64 `json:"true_target_kwh"`
			ForecastTargetKWh float64 `json:"forecast_target_kwh"`
		} `json:"outcomes"`
	}
	if err := json.Unmarshal(raw, &full); err != nil {
		t.Fatal(err)
	}
	if len(full.Outcomes) != 3 {
		t.Fatalf("%d outcomes", len(full.Outcomes))
	}
	for _, o := range full.Outcomes {
		w, ok := byID[o.ID]
		if !ok {
			t.Fatalf("unexpected bus %s", o.ID)
		}
		if o.CapacityKWh != w.capKWh || o.InitialSoCKWh != w.initial || o.TrueTargetKWh != w.target || o.ForecastTargetKWh != w.target {
			t.Errorf("%s: %+v, want %+v", o.ID, o, w)
		}
	}
	if r.Metrics.PlanViolations != 0 {
		t.Errorf("plan violations %d", r.Metrics.PlanViolations)
	}
	// the metrics are those of the replay for the same night
	rec := do(New(), newReq("POST", "/api/replay", body(t, demoFiles(t), nil)))
	var rep struct {
		Nights []struct {
			Controllers []struct {
				Name    string `json:"name"`
				Seeds   []struct{ Metrics sim.Metrics }
				Metrics sim.Metrics
			} `json:"controllers"`
		} `json:"nights"`
	}
	decode(t, rec, &rep)
	for _, c := range rep.Nights[0].Controllers {
		if c.Name == "planner" && (c.Seeds[0].Metrics.ReadyPct != r.Metrics.ReadyPct || c.Seeds[0].Metrics.EnergyKWh != r.Metrics.EnergyKWh) {
			t.Errorf("run %+v differs from replay %+v", r.Metrics, c.Seeds[0].Metrics)
		}
	}
}

func TestRunRealNightIsDeterministicAndIgnoresOtherParams(t *testing.T) {
	s := New()
	_, a := runReal(t, s, "planner", nil)
	_, b := runReal(t, s, "planner", nil)
	if !bytes.Equal(a, b) {
		t.Fatal("two runs of the same night differ")
	}
	// buses/chargers/profile/seed of the form are ignored when files come with the request
	_, c := runReal(t, s, "planner", map[string]any{"buses": 40, "chargers": 9, "limit_kw": 1, "profile": "severe", "seed": 7})
	if !bytes.Equal(a, c) {
		t.Fatal("the generator parameters changed an imported night")
	}
	// ... but the swap-back parameters are used (and echoed)
	r, _ := runReal(t, s, "planner", map[string]any{"swap_back_cooldown_min": 7, "swap_back_min_need_kwh": 3})
	if r.Params.SwapBackCooldownMin != 7 || r.Params.SwapBackMinNeedKWh != 3 {
		t.Errorf("swap-back params %+v", r.Params)
	}
}

func TestRunRealNightErrors(t *testing.T) {
	files := demoFiles(t)
	for name, c := range map[string]struct {
		extra map[string]any
		field string
	}{
		"unknown night":      {map[string]any{"night": "2030-01-01"}, "night"},
		"night not a date":   {map[string]any{"night": "ontem"}, "night"},
		"night missing":      {map[string]any{"night": ""}, "night"},
		"unknown controller": {map[string]any{"controller": "magia"}, "controller"},
		"bad cooldown":       {map[string]any{"swap_back_cooldown_min": -1}, "swap_back_cooldown_min"},
		"bad min need":       {map[string]any{"swap_back_min_need_kwh": -1}, "swap_back_min_need_kwh"},
	} {
		m := map[string]any{"night": "2026-03-04", "controller": "planner"}
		for k, v := range c.extra {
			m[k] = v
		}
		rec := do(New(), newReq("POST", "/api/run", body(t, files, m)))
		var e errBody
		decode(t, rec, &e)
		if rec.Code != 400 || e.Field != c.field || e.Error == "" {
			t.Errorf("%s: %d %+v", name, rec.Code, e)
		}
	}
	// night without files, and the extra controller without files
	for name, b := range map[string]string{
		"night alone":               `{"night":"2026-03-04"}`,
		"no-swap controller alone":  `{"controller":"planner (sem rodízio)"}`,
		"files with a non-string":   `{"files":{"garagem.csv":1},"night":"2026-03-04"}`,
		"unknown key with files":    `{"files":{},"night":"x","nope":1}`,
		"files only (night absent)": body(t, files, nil),
	} {
		if rec := do(New(), newReq("POST", "/api/run", b)); rec.Code != 400 {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
}

func TestRunRealNightKeepsRunLimits(t *testing.T) {
	// 501 chargers (labMaxRunChargers is 500) in a valid dataset
	files := demoFiles(t)
	var sb strings.Builder
	sb.WriteString("carregador_id,potencia_max_kw\n")
	for i := 0; i < labMaxRunChargers+1; i++ {
		fmt.Fprintf(&sb, "X%03d,150\n", i)
	}
	files["carregadores.csv"] = sb.String()
	delete(files, "sessoes.csv")
	delete(files, "potencia.csv")
	rec := do(New(), newReq("POST", "/api/run", body(t, files, map[string]any{"night": "2026-03-04"})))
	var e errBody
	decode(t, rec, &e)
	if rec.Code != 400 || e.Field != "chargers" {
		t.Fatalf("%d %+v", rec.Code, e)
	}
}

// ---- /api/run without files must not change ----

func TestRunWithoutFilesIsUnchanged(t *testing.T) {
	// SHA-256 of the response bytes as produced before this feature existed (commit cfa5728).
	// If the simulator or the planner change on purpose, regenerate these two.
	golden := map[string]string{
		"planner": "11f83f4495cfe5d1cbb8a48989e944bad9ea81cb29e6f465731a9ea35db330f5",
		"fifo":    "2a664c8d390bbe99c7d503e34946a005cebdac82344d7b2ed40bb550c77975b4",
	}
	s := New()
	for c, want := range golden {
		b := `{"buses":10,"chargers":5,"limit_kw":500,"profile":"mild","seed":2,"controller":"` + c + `"}`
		rec := do(s, newReq("POST", "/api/run", b))
		if rec.Code != 200 {
			t.Fatalf("%s: %d", c, rec.Code)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(rec.Body.Bytes())); got != want {
			t.Errorf("%s: response bytes changed (sha256 %s)", c, got)
		}
		if bytes.Contains(rec.Body.Bytes(), []byte(`"source"`)) {
			t.Errorf("%s: \"source\" must be absent without files", c)
		}
	}
}
