package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
	"github.com/EduardoMilani8/depot-charge-planner/web"
)

const smallBody = `{"buses":10,"chargers":5,"limit_kw":500,"profile":"mild","seeds":3}`

func newReq(method, target, body string) *http.Request {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	req.Host = "127.0.0.1:8080"
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func do(s *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("invalid JSON (%v): %.300s", err, rec.Body.String())
	}
}

func TestDefaults(t *testing.T) {
	rec := do(New(), newReq("GET", "/api/defaults", ""))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var d struct {
		Params      Params   `json:"params"`
		Controllers []string `json:"controllers"`
		Limits      struct {
			MaxBuses int `json:"max_buses"`
		} `json:"limits"`
		Presets []struct {
			ID     string `json:"id"`
			Label  string `json:"label"`
			Params Params `json:"params"`
		} `json:"presets"`
	}
	decode(t, rec, &d)
	if d.Params != defaultParams() {
		t.Errorf("defaults %+v", d.Params)
	}
	if strings.Join(d.Controllers, ",") != strings.Join(sim.ControllerNames, ",") {
		t.Errorf("controllers %v", d.Controllers)
	}
	if d.Limits.MaxBuses != sim.MaxBuses {
		t.Errorf("max buses %d", d.Limits.MaxBuses)
	}
	if len(d.Presets) < 3 {
		t.Fatalf("want at least 3 presets, got %d", len(d.Presets))
	}
	for _, p := range d.Presets {
		if p.ID == "" || p.Label == "" {
			t.Errorf("preset without id/label: %+v", p)
		}
		if err := p.Params.validate(); err != nil {
			t.Errorf("preset %s is invalid: %+v", p.ID, err)
		}
	}
}

func TestCompareShape(t *testing.T) {
	rec := do(New(), newReq("POST", "/api/compare", smallBody))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r struct {
		Controllers []struct {
			Name      string      `json:"name"`
			Aggregate sim.Metrics `json:"aggregate"`
			Seeds     []struct {
				Seed    int64       `json:"seed"`
				Metrics sim.Metrics `json:"metrics"`
			} `json:"seeds"`
		} `json:"controllers"`
		P99Note string `json:"p99_note"`
	}
	decode(t, rec, &r)
	if len(r.Controllers) != len(sim.ControllerNames) {
		t.Fatalf("%d controllers", len(r.Controllers))
	}
	if r.P99Note == "" {
		t.Error("the p99 note is missing")
	}
	g, cfg := mustParams(t, smallBody).toSim()
	want, err := sim.Compare(context.Background(), g, cfg, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range r.Controllers {
		if c.Name != sim.ControllerNames[i] || len(c.Seeds) != 3 {
			t.Errorf("controller %d: %s with %d seeds", i, c.Name, len(c.Seeds))
		}
		if c.Aggregate.ReadyPct != want[i].Aggregate.ReadyPct || c.Aggregate.CostBRL != want[i].Aggregate.CostBRL {
			t.Errorf("%s differs from sim.Compare: %+v vs %+v", c.Name, c.Aggregate, want[i].Aggregate)
		}
		if c.Name == "planner" && c.Aggregate.PlanViolations != 0 {
			t.Errorf("planner violated the limit %d times", c.Aggregate.PlanViolations)
		}
	}
}

func mustParams(t *testing.T, body string) Params {
	t.Helper()
	p := defaultParams()
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCompareRejectsBadInput(t *testing.T) {
	cases := []struct{ name, body, field string }{
		{"zero buses", `{"buses":0}`, "buses"},
		{"too many buses", `{"buses":10001}`, "buses"},
		{"zero chargers", `{"chargers":0}`, "chargers"},
		{"negative limit", `{"limit_kw":-1}`, "limit_kw"},
		{"huge limit", `{"limit_kw":1e308}`, "limit_kw"},
		{"unknown profile", `{"profile":"x"}`, "profile"},
		{"zero seeds", `{"seeds":0}`, "seeds"},
		{"too many seeds", `{"seeds":1001}`, "seeds"},
		{"negative reading age", `{"reading_age_min":-3}`, "reading_age_min"},
		{"negative cooldown", `{"swap_back_cooldown_min":-1}`, "swap_back_cooldown_min"},
		{"negative min need", `{"swap_back_min_need_kwh":-1}`, "swap_back_min_need_kwh"},
		{"work cap", `{"buses":600,"seeds":40}`, "seeds"},
		{"wrong type", `{"buses":"many"}`, ""},
		{"unknown field", `{"nonsense":1}`, ""},
		{"malformed", `{`, ""},
		{"trailing data", `{} {}`, ""},
	}
	s := New()
	for _, tc := range cases {
		rec := do(s, newReq("POST", "/api/compare", tc.body))
		if rec.Code != 400 {
			t.Errorf("%s: status %d, want 400 (%s)", tc.name, rec.Code, rec.Body)
			continue
		}
		var e struct{ Error, Field string }
		decode(t, rec, &e)
		if e.Error == "" {
			t.Errorf("%s: empty error message", tc.name)
		}
		if tc.field != "" && e.Field != tc.field {
			t.Errorf("%s: field %q, want %q", tc.name, e.Field, tc.field)
		}
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: content type %q", tc.name, rec.Header().Get("Content-Type"))
		}
	}
}

func TestBodyTooLarge(t *testing.T) {
	body := `{"buses":10` + strings.Repeat(" ", 70<<10) + `}`
	rec := do(New(), newReq("POST", "/api/compare", body))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", rec.Code)
	}
}

const runBody = `{"buses":10,"chargers":5,"limit_kw":500,"profile":"mild","seed":2,"controller":"planner"}`

type runResp struct {
	Controller string      `json:"controller"`
	Seed       int64       `json:"seed"`
	Metrics    sim.Metrics `json:"metrics"`
	Scenario   struct {
		HorizonMin int `json:"horizon_min"`
		Chargers   []struct {
			ID string `json:"id"`
		} `json:"chargers"`
		Faults []struct {
			To int `json:"to"`
		} `json:"faults"`
	} `json:"scenario"`
	Series struct {
		LimitKW    []float64 `json:"limit_kw"`
		PhysicalKW []float64 `json:"physical_kw"`
		Layer      []string  `json:"layer"`
		Chargers   []struct {
			ID         string    `json:"id"`
			Status     []int     `json:"status"`
			PhysicalKW []float64 `json:"physical_kw"`
		} `json:"chargers"`
		Buses []struct {
			ID      string `json:"id"`
			State   []int  `json:"state"`
			Charger []int  `json:"charger"`
		} `json:"buses"`
	} `json:"series"`
	Reasons   []string `json:"reasons"`
	Decisions []struct {
		Minute int `json:"minute"`
		Buses  []struct {
			B string `json:"b"`
			R int    `json:"r"`
		} `json:"buses"`
	} `json:"decisions"`
	Outcomes []struct {
		ID string `json:"id"`
	} `json:"outcomes"`
}

func TestRunShape(t *testing.T) {
	rec := do(New(), newReq("POST", "/api/run", runBody))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r runResp
	decode(t, rec, &r)
	n := r.Scenario.HorizonMin + 1
	if len(r.Scenario.Chargers) != 5 || len(r.Series.Chargers) != 5 || len(r.Series.Buses) != 10 || len(r.Outcomes) != 10 {
		t.Fatalf("%d/%d chargers, %d buses, %d outcomes", len(r.Scenario.Chargers), len(r.Series.Chargers), len(r.Series.Buses), len(r.Outcomes))
	}
	if len(r.Series.LimitKW) != n || len(r.Series.PhysicalKW) != n || len(r.Series.Layer) != n {
		t.Errorf("series lengths %d/%d/%d, want %d", len(r.Series.LimitKW), len(r.Series.PhysicalKW), len(r.Series.Layer), n)
	}
	for _, c := range r.Series.Chargers {
		if len(c.Status) != n || len(c.PhysicalKW) != n {
			t.Errorf("charger %s series length", c.ID)
		}
	}
	for _, b := range r.Series.Buses {
		if len(b.State) != n || len(b.Charger) != n {
			t.Errorf("bus %s series length", b.ID)
		}
		for _, ci := range b.Charger {
			if ci < -1 || ci >= 5 {
				t.Fatalf("bus %s charger index %d out of range", b.ID, ci)
			}
		}
	}
	for _, f := range r.Scenario.Faults {
		if f.To > r.Scenario.HorizonMin {
			t.Errorf("fault window ends at %d, past the horizon %d", f.To, r.Scenario.HorizonMin)
		}
	}
	if len(r.Decisions) == 0 || r.Decisions[0].Minute != 0 {
		t.Fatalf("decisions: %+v", r.Decisions)
	}
	withReason := 0
	for _, d := range r.Decisions {
		for _, b := range d.Buses {
			if b.R < -1 || b.R >= len(r.Reasons) {
				t.Fatalf("reason index %d out of range (%d reasons)", b.R, len(r.Reasons))
			}
			if b.R >= 0 {
				withReason++
			}
		}
	}
	if withReason == 0 {
		t.Error("the planner explains nothing: no bus has a reason")
	}
	if r.Metrics.PlanViolations != 0 {
		t.Errorf("plan violations %d", r.Metrics.PlanViolations)
	}
}

func TestRunBaselineDoesNotExplain(t *testing.T) {
	body := strings.Replace(runBody, `"planner"`, `"fifo"`, 1)
	rec := do(New(), newReq("POST", "/api/run", body))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r runResp
	decode(t, rec, &r)
	for _, d := range r.Decisions {
		if len(d.Buses) != 0 {
			t.Fatalf("fifo has no per-bus explanation, got %+v", d.Buses)
		}
	}
}

func TestRunIsDeterministic(t *testing.T) {
	s := New()
	a := do(s, newReq("POST", "/api/run", runBody)).Body.Bytes()
	b := do(s, newReq("POST", "/api/run", runBody)).Body.Bytes()
	if !bytes.Equal(a, b) {
		t.Fatal("the same request gave different bytes")
	}
}

func TestRunSizeUnder2MB(t *testing.T) {
	body := `{"profile":"severe","seed":1,"controller":"planner"}` // defaults: 50 buses, 25 chargers
	rec := do(New(), newReq("POST", "/api/run", body))
	if rec.Code != 200 {
		t.Fatalf("status %d: %.200s", rec.Code, rec.Body)
	}
	t.Logf("run response: %d bytes", rec.Body.Len())
	if rec.Body.Len() > 2<<20 {
		t.Fatalf("response is %d bytes, want under 2 MB", rec.Body.Len())
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	cases := []struct{ name, body, field string }{
		{"unknown controller", `{"controller":"nope"}`, "controller"},
		{"seed zero", `{"seed":0}`, "seed"},
		{"seed too big", `{"seed":1001}`, "seed"},
		{"too many buses for a run", `{"buses":1001}`, "buses"},
		{"bad profile", `{"profile":"x"}`, "profile"},
	}
	s := New()
	for _, tc := range cases {
		rec := do(s, newReq("POST", "/api/run", tc.body))
		var e struct{ Error, Field string }
		decode(t, rec, &e)
		if rec.Code != 400 || e.Field != tc.field {
			t.Errorf("%s: status %d field %q (%s), want 400 %q", tc.name, rec.Code, e.Field, e.Error, tc.field)
		}
	}
}

func TestHostIsChecked(t *testing.T) {
	s := New()
	for _, host := range []string{"evil.example.com", "evil.example.com:8080", "127.0.0.1.evil.com", "192.168.0.10:8080", ""} {
		req := newReq("GET", "/api/defaults", "")
		req.Host = host
		if rec := do(s, req); rec.Code != http.StatusForbidden {
			t.Errorf("host %q: status %d, want 403", host, rec.Code)
		}
		req = newReq("GET", "/", "")
		req.Host = host
		if rec := do(s, req); rec.Code != http.StatusForbidden {
			t.Errorf("host %q on /: status %d, want 403", host, rec.Code)
		}
	}
	for _, host := range []string{"127.0.0.1", "127.0.0.1:8080", "localhost", "localhost:9000", "[::1]:8080", "[::1]"} {
		req := newReq("GET", "/api/defaults", "")
		req.Host = host
		if rec := do(s, req); rec.Code != 200 {
			t.Errorf("host %q: status %d, want 200", host, rec.Code)
		}
	}
}

func TestOriginIsChecked(t *testing.T) {
	s := New()
	req := newReq("POST", "/api/compare", smallBody)
	req.Header.Set("Origin", "http://evil.example.com")
	if rec := do(s, req); rec.Code != http.StatusForbidden {
		t.Errorf("foreign origin: status %d, want 403", rec.Code)
	}
	req = newReq("POST", "/api/compare", smallBody)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	if rec := do(s, req); rec.Code != 200 {
		t.Errorf("local origin: status %d, want 200", rec.Code)
	}
}

func TestMethodAndContentType(t *testing.T) {
	s := New()
	if rec := do(s, newReq("GET", "/api/compare", "")); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET compare: %d", rec.Code)
	}
	if rec := do(s, newReq("POST", "/api/defaults", "{}")); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST defaults: %d", rec.Code)
	}
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		req := newReq("POST", "/api/compare", smallBody)
		req.Header.Del("Content-Type")
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if rec := do(s, req); rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("content type %q: %d, want 415", ct, rec.Code)
		}
	}
}

func TestBusyServerAnswers503(t *testing.T) {
	s := New()
	for i := 0; i < cap(s.sem); i++ {
		s.sem <- struct{}{}
	}
	rec := do(s, newReq("POST", "/api/compare", smallBody))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
	for i := 0; i < cap(s.sem); i++ {
		<-s.sem
	}
	if rec := do(s, newReq("POST", "/api/compare", smallBody)); rec.Code != 200 {
		t.Fatalf("after releasing the slots: %d", rec.Code)
	}
}

func TestTimeoutAnswers504(t *testing.T) {
	s := New()
	s.timeout = time.Nanosecond
	rec := do(s, newReq("POST", "/api/compare", smallBody))
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status %d, want 504: %s", rec.Code, rec.Body)
	}
}

func TestStaticIndexIsServed(t *testing.T) {
	rec := do(New(), newReq("GET", "/", ""))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>") {
		t.Fatalf("status %d: %.200s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("cache control %q", rec.Header().Get("Cache-Control"))
	}
}

var (
	htmlRef = regexp.MustCompile(`(?:src|href)="([^"#?]+)"`)
	jsImp   = regexp.MustCompile(`(?:from|import)\s*\(?\s*["'](\.{1,2}/[^"']+)["']`)
)

// Every file the page references (and every relative JS import) must be embedded.
func TestEmbeddedAssetsExist(t *testing.T) {
	exists := func(p string) bool { _, err := fs.Stat(web.Static, "static/"+p); return err == nil }
	idx, err := fs.ReadFile(web.Static, "static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	var queue []string
	for _, m := range htmlRef.FindAllStringSubmatch(string(idx), -1) {
		ref := m[1]
		if strings.Contains(ref, "://") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "data:") {
			continue
		}
		if !exists(ref) {
			t.Errorf("index.html references %q, which is not embedded", ref)
		}
		if strings.HasSuffix(ref, ".js") {
			queue = append(queue, ref)
		}
	}
	seen := map[string]bool{}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		if seen[f] {
			continue
		}
		seen[f] = true
		src, err := fs.ReadFile(web.Static, "static/"+f)
		if err != nil {
			t.Errorf("cannot read %s: %v", f, err)
			continue
		}
		for _, m := range jsImp.FindAllStringSubmatch(string(src), -1) {
			target := path.Join(path.Dir(f), m[1])
			if !exists(target) {
				t.Errorf("%s imports %q, which is not embedded", f, m[1])
				continue
			}
			queue = append(queue, target)
		}
	}
}
