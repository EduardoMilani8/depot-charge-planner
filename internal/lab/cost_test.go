package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

// costBody is a depot where every bus has a plug and the buses arrive in the peak window.
const costBody = `{"buses":12,"chargers":12,"limit_kw":600,"seeds":3,"arrival_from_min":0,"arrival_to_min":180,"cost_aware":true,` +
	`"demand_peak_price":40,"demand_offpeak_price":20}`

type compareBody struct {
	Params      map[string]any `json:"params"`
	Warnings    []string       `json:"warnings"`
	Controllers []struct {
		Name      string      `json:"name"`
		Aggregate sim.Metrics `json:"aggregate"`
		Seeds     []struct {
			Metrics sim.Metrics `json:"metrics"`
		} `json:"seeds"`
	} `json:"controllers"`
}

func postCompare(t *testing.T, body string) (compareBody, []byte) {
	t.Helper()
	rec := do(New(), newReq("POST", "/api/compare", body))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r compareBody
	decode(t, rec, &r)
	return r, rec.Body.Bytes()
}

// New parameters are optional and omitted when unset, so Params keeps serialising as it
// did and a UI reads the absence as the default.
func TestDefaultParamsSerialiseWithoutTheCostFields(t *testing.T) {
	b, err := json.Marshal(defaultParams())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"cost_aware", "defer_margin_min", "demand_target_kw", "demand_peak_price", "demand_offpeak_price",
		"demand_contracted_kw", "demand_verde", "arrival_from_min", "arrival_to_min", "nights_per_month"} {
		if bytes.Contains(b, []byte(`"`+k+`"`)) {
			t.Errorf("default params carry %q: %s", k, b)
		}
	}
	rec := do(New(), newReq("POST", "/api/compare", smallBody))
	for _, k := range []string{`"bill"`, `"warnings"`, `"cost_aware"`} {
		if bytes.Contains(rec.Body.Bytes(), []byte(k)) {
			t.Errorf("a request without cost parameters must not mention %s", k)
		}
	}
}

func TestDefaultsExposeTheCostPresetsAndLimits(t *testing.T) {
	rec := do(New(), newReq("GET", "/api/defaults", ""))
	var d struct {
		Limits  map[string]any `json:"limits"`
		Presets []struct {
			ID, Label, Description string
			Params                 Params
		} `json:"presets"`
	}
	decode(t, rec, &d)
	byLabel := map[string]Params{}
	for _, p := range d.Presets {
		byLabel[p.Label] = p.Params
		if err := p.Params.validate(); err != nil {
			t.Errorf("preset %q invalid: %+v", p.Label, err)
		}
		if p.Description == "" {
			t.Errorf("preset %q has no description", p.Label)
		}
	}
	peak, ok := byLabel["Chegadas na ponta (custo)"]
	if !ok {
		t.Fatalf("preset \"Chegadas na ponta (custo)\" missing: %+v", d.Presets)
	}
	// Arrivals inside the peak window (minutes 0..180 = 18:00-21:00), every bus with a plug.
	if !peak.CostAware || peak.ArrivalToMin != 180 || peak.ArrivalFromMin != 0 || peak.Chargers < peak.Buses {
		t.Errorf("peak-arrivals preset %+v", peak)
	}
	bill, ok := byLabel["Fatura com demanda (ilustrativa)"]
	if !ok {
		t.Fatal("preset \"Fatura com demanda (ilustrativa)\" missing")
	}
	if !bill.CostAware || bill.DemandPeakPrice <= 0 || bill.DemandOffPeakPrice <= 0 || bill.DemandTargetKW <= 0 || bill.DemandTargetKW >= bill.LimitKW {
		t.Errorf("demand-bill preset %+v", bill)
	}
	for _, k := range []string{"max_defer_margin_min", "max_arrival_min", "max_demand_price", "max_nights_per_month",
		"default_defer_margin_min", "default_nights_per_month", "demand_verde_note"} {
		if _, ok := d.Limits[k]; !ok {
			t.Errorf("limits lack %q: %v", k, d.Limits)
		}
	}
	if d.Limits["default_nights_per_month"] != 30.0 || d.Limits["default_defer_margin_min"] != 45.0 ||
		d.Limits["max_arrival_min"] != float64(sim.MaxArrivalMin) {
		t.Errorf("limits %v", d.Limits)
	}
	if note, _ := d.Limits["demand_verde_note"].(string); !strings.Contains(note, "Verde") || !strings.Contains(note, "ignorado") {
		t.Errorf("the verde note must say the peak demand price is ignored: %q", note)
	}
}

// The presets run (planner, seed 1) and the bill-carrying one shows a bill.
func TestCostPresetsRun(t *testing.T) {
	s := New()
	for _, label := range []string{"Chegadas na ponta (custo)", "Fatura com demanda (ilustrativa)"} {
		var p Params
		for _, pr := range presets() {
			if pr.Label == label {
				p = pr.Params
			}
		}
		body, _ := json.Marshal(runRequest{Params: p, Seed: 1, Controller: "planner"})
		rec := do(s, newReq("POST", "/api/run", string(body)))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %.200s", label, rec.Code, rec.Body)
		}
		hasBill := bytes.Contains(rec.Body.Bytes(), []byte(`"bill":{`))
		if want := p.DemandPeakPrice > 0 || p.DemandOffPeakPrice > 0; hasBill != want {
			t.Errorf("%s: bill present = %v, want %v", label, hasBill, want)
		}
	}
}

func TestCostParamsRejectBadInput(t *testing.T) {
	cases := []struct{ name, body, field string }{
		{"negative defer margin", `{"cost_aware":true,"defer_margin_min":-1}`, "defer_margin_min"},
		{"huge defer margin", `{"cost_aware":true,"defer_margin_min":241}`, "defer_margin_min"},
		{"defer margin without cost_aware", `{"defer_margin_min":30}`, "defer_margin_min"},
		{"negative target", `{"cost_aware":true,"demand_target_kw":-1}`, "demand_target_kw"},
		{"huge target", `{"cost_aware":true,"demand_target_kw":1e308}`, "demand_target_kw"},
		{"target without cost_aware", `{"demand_target_kw":500}`, "demand_target_kw"},
		{"negative peak price", `{"demand_peak_price":-1}`, "demand_peak_price"},
		{"huge peak price", `{"demand_peak_price":1e7}`, "demand_peak_price"},
		{"negative offpeak price", `{"demand_offpeak_price":-0.5}`, "demand_offpeak_price"},
		{"huge offpeak price", `{"demand_offpeak_price":1e308}`, "demand_offpeak_price"},
		{"negative contract", `{"demand_contracted_kw":-1}`, "demand_contracted_kw"},
		{"huge contract", `{"demand_contracted_kw":1e308}`, "demand_contracted_kw"},
		{"arrival from negative", `{"arrival_from_min":-1,"arrival_to_min":100}`, "arrival_from_min"},
		{"arrival from at the max", `{"arrival_from_min":600,"arrival_to_min":600}`, "arrival_from_min"},
		{"arrival to before from", `{"arrival_from_min":100,"arrival_to_min":50}`, "arrival_to_min"},
		{"arrival to equals from", `{"arrival_from_min":100,"arrival_to_min":100}`, "arrival_to_min"},
		{"arrival to past the max", `{"arrival_from_min":0,"arrival_to_min":601}`, "arrival_to_min"},
		{"arrival from alone", `{"arrival_from_min":100}`, "arrival_to_min"},
		{"negative nights", `{"nights_per_month":-1}`, "nights_per_month"},
		{"too many nights", `{"nights_per_month":32}`, "nights_per_month"},
		{"wrong type", `{"cost_aware":"yes"}`, ""},
	}
	s := New()
	for _, path := range []string{"/api/compare", "/api/run"} {
		for _, tc := range cases {
			rec := do(s, newReq("POST", path, tc.body))
			var e struct{ Error, Field string }
			decode(t, rec, &e)
			if rec.Code != 400 || e.Error == "" || (tc.field != "" && e.Field != tc.field) {
				t.Errorf("%s %s: status %d field %q (%s), want 400 %q", path, tc.name, rec.Code, e.Field, e.Error, tc.field)
			}
			if strings.Contains(e.Error, "e+") {
				t.Errorf("%s: message %q has an exponent", tc.name, e.Error)
			}
		}
	}
}

func TestCostMessagesAreInPortugueseWithPlainNumbers(t *testing.T) {
	s := New()
	for _, tc := range []struct{ body, want string }{
		{`{"cost_aware":true,"defer_margin_min":999}`, "de 0 a 240 minutos"},
		{`{"cost_aware":true,"demand_target_kw":1e308}`, fmt.Sprintf("de 0 a %.0f kW", float64(sim.MaxLimitKW))},
		{`{"demand_target_kw":500}`, "cost_aware"},
		{`{"demand_peak_price":1e7}`, fmt.Sprintf("de 0 a %.0f R$/kW", float64(sim.MaxDemandPrice))},
		{`{"demand_contracted_kw":-3}`, "kW"},
		{`{"arrival_from_min":-1,"arrival_to_min":100}`, "18:00"},
		{`{"arrival_from_min":100,"arrival_to_min":50}`, fmt.Sprintf("até %d minutos", sim.MaxArrivalMin)},
		{`{"nights_per_month":99}`, "de 1 a 31"},
	} {
		rec := do(s, newReq("POST", "/api/compare", tc.body))
		var e struct{ Error string }
		decode(t, rec, &e)
		if rec.Code != 400 || !strings.Contains(e.Error, tc.want) {
			t.Errorf("%s: status %d, message %q, want it to contain %q", tc.body, rec.Code, e.Error, tc.want)
		}
	}
}

// A target above the site limit is allowed (it is inert); so are the edges of every range.
func TestCostParamsEdgesAreAccepted(t *testing.T) {
	for _, body := range []string{
		`{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"cost_aware":true,"demand_target_kw":5000}`,
		`{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"cost_aware":true,"defer_margin_min":240}`,
		`{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"cost_aware":true,"defer_margin_min":0}`,
		`{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"arrival_from_min":599,"arrival_to_min":600}`,
		`{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"arrival_from_min":0,"arrival_to_min":1}`,
		`{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"nights_per_month":31,"demand_offpeak_price":10,"demand_contracted_kw":100}`,
		`{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"nights_per_month":0}`,
	} {
		if rec := do(New(), newReq("POST", "/api/compare", body)); rec.Code != 200 {
			t.Errorf("%s: status %d %s", body, rec.Code, rec.Body)
		}
	}
}

// bill rides along in the metrics only when demand is billed.
func TestBillIsPresentOnlyWithDemand(t *testing.T) {
	with, raw := postCompare(t, costBody)
	if !bytes.Contains(raw, []byte(`"bill":{`)) {
		t.Fatalf("no bill with demand prices: %.300s", raw)
	}
	for _, c := range with.Controllers {
		if c.Aggregate.Bill == nil {
			t.Errorf("%s: aggregate without bill", c.Name)
		}
		for i, sd := range c.Seeds {
			if sd.Metrics.Bill == nil {
				t.Errorf("%s seed %d: no bill", c.Name, i+1)
			}
		}
	}
	// A contract alone also bills demand; cost_aware alone does not.
	if _, raw := postCompare(t, `{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"demand_contracted_kw":100}`); !bytes.Contains(raw, []byte(`"bill":{`)) {
		t.Error("a contract alone bills demand")
	}
	if _, raw := postCompare(t, `{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"cost_aware":true}`); bytes.Contains(raw, []byte(`"bill"`)) {
		t.Error("cost_aware without demand prices must not produce a bill")
	}
	// Verde with only a peak price bills nothing (the peak price is ignored).
	if _, raw := postCompare(t, `{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"demand_verde":true,"demand_peak_price":40}`); bytes.Contains(raw, []byte(`"bill"`)) {
		t.Error("verde ignores the peak demand price: no bill")
	}
	// /api/run too.
	rec := do(New(), newReq("POST", "/api/run", `{"buses":4,"chargers":4,"limit_kw":300,"demand_offpeak_price":20,"nights_per_month":22}`))
	var r struct {
		Metrics sim.Metrics `json:"metrics"`
	}
	decode(t, rec, &r)
	if r.Metrics.Bill == nil || r.Metrics.Bill.MonthlyBRL <= r.Metrics.CostBRL*21 {
		t.Errorf("run bill %+v with cost %v: monthly must be about 22 nights of energy plus demand", r.Metrics.Bill, r.Metrics.CostBRL)
	}
}

// The API agrees with the library, and only the planner reacts to cost_aware.
func TestCompareWithCostParamsMatchesTheLibrary(t *testing.T) {
	got, _ := postCompare(t, costBody)
	p := mustParams(t, costBody)
	g, cfg := p.toSim()
	if cfg.Cost == nil {
		t.Fatal("cost_aware did not reach the planner config")
	}
	want, err := sim.Compare(context.Background(), g, cfg, p.Seeds, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range got.Controllers {
		w := want[i].Aggregate
		if c.Aggregate.CostBRL != w.CostBRL || c.Aggregate.Bill.MonthlyBRL != w.Bill.MonthlyBRL || c.Aggregate.Bill.DemandKW != w.Bill.DemandKW {
			t.Errorf("%s differs from sim.Compare:\n%+v\n%+v", c.Name, c.Aggregate.Bill, w.Bill)
		}
	}
	off, _ := postCompare(t, strings.Replace(costBody, `"cost_aware":true,`, "", 1))
	for i, c := range got.Controllers {
		same := c.Aggregate.CostBRL == off.Controllers[i].Aggregate.CostBRL && c.Aggregate.Bill.MonthlyBRL == off.Controllers[i].Aggregate.Bill.MonthlyBRL
		if c.Name == "planner" && same {
			t.Error("cost_aware did not change the planner")
		}
		if c.Name != "planner" && !same {
			t.Errorf("cost_aware changed %s", c.Name)
		}
	}
}

func TestRunWithCostParamsIsDeterministic(t *testing.T) {
	body := `{"buses":12,"chargers":12,"limit_kw":600,"seed":2,"controller":"planner","arrival_from_min":0,"arrival_to_min":180,` +
		`"cost_aware":true,"demand_target_kw":400,"demand_peak_price":40,"demand_offpeak_price":20}`
	s := New()
	a := do(s, newReq("POST", "/api/run", body))
	b := do(s, newReq("POST", "/api/run", body))
	if a.Code != 200 || !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
		t.Fatalf("status %d; two identical /api/run requests gave different bytes", a.Code)
	}
	var r struct {
		Params    map[string]any `json:"params"`
		Decisions []struct {
			Notes []int `json:"notes"`
		} `json:"decisions"`
		Notes   []string    `json:"notes"`
		Metrics sim.Metrics `json:"metrics"`
	}
	decode(t, a, &r)
	if r.Params["cost_aware"] != true || r.Params["demand_target_kw"] != 400.0 {
		t.Errorf("the response must echo the cost parameters: %v", r.Params)
	}
	if r.Metrics.Bill == nil || r.Metrics.Bill.DemandKW > 400.0+1 {
		t.Errorf("bill %+v: the target 400 kW should hold the demand", r.Metrics.Bill)
	}
}

// Verde ignores the peak demand price: the API says so in the response (a UI cannot
// read it from the absence of anything else), and a target above the limit or a
// cost-aware planner without a plug per bus are flagged too.
func TestWarnings(t *testing.T) {
	r, _ := postCompare(t, `{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"demand_verde":true,"demand_peak_price":40,"demand_offpeak_price":20}`)
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "Verde") || !strings.Contains(r.Warnings[0], "ignorado") {
		t.Errorf("verde with a peak price: warnings %q", r.Warnings)
	}
	r, _ = postCompare(t, `{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"demand_verde":true,"demand_offpeak_price":20}`)
	if len(r.Warnings) != 0 {
		t.Errorf("verde without a peak price needs no warning: %q", r.Warnings)
	}
	r, _ = postCompare(t, `{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"cost_aware":true,"demand_target_kw":900}`)
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "meta de demanda") || !strings.Contains(r.Warnings[0], "não tem efeito") {
		t.Errorf("target above the limit: warnings %q", r.Warnings)
	}
	r, _ = postCompare(t, `{"buses":6,"chargers":3,"limit_kw":300,"seeds":1,"cost_aware":true}`)
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "tomada") {
		t.Errorf("fewer chargers than buses: warnings %q", r.Warnings)
	}
	rec := do(New(), newReq("POST", "/api/run", `{"buses":4,"chargers":4,"limit_kw":300,"demand_verde":true,"demand_peak_price":40,"demand_offpeak_price":20}`))
	var run struct{ Warnings []string }
	decode(t, rec, &run)
	if len(run.Warnings) != 1 {
		t.Errorf("/api/run warnings %q", run.Warnings)
	}
}

// The monthly estimate follows nights_per_month.
func TestNightsPerMonthScalesTheEstimate(t *testing.T) {
	month := func(n int) float64 {
		r, _ := postCompare(t, fmt.Sprintf(`{"buses":4,"chargers":4,"limit_kw":300,"seeds":1,"demand_offpeak_price":20,"nights_per_month":%d}`, n))
		return r.Controllers[0].Aggregate.Bill.MonthlyBRL
	}
	if a, b := month(10), month(20); !(b > a) {
		t.Errorf("monthly R$ with 20 nights (%v) must exceed 10 nights (%v)", b, a)
	}
}
