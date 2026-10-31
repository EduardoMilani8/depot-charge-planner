package lab

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// garagem.csv with demand prices: the replay API carries the real bill and the simulated
// rows' bill; the demo without the columns has none of those keys.
func TestReplayWithDemandPrices(t *testing.T) {
	files := demoFiles(t)
	files["garagem.csv"] = "limite_kw,ponta_inicio,ponta_fim,preco_ponta,preco_fora_ponta,demanda_preco_ponta,demanda_preco_fora_ponta\n400,18:00,21:00,2.70,0.90,40,20\n"
	rec := do(New(), newReq("POST", "/api/replay", body(t, files, nil)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %.300s", rec.Code, rec.Body)
	}
	var r struct {
		DemandBilled   bool `json:"demand_billed"`
		NightsPerMonth int  `json:"nights_per_month"`
		Nights         []struct {
			Real struct {
				DemandKW        *float64 `json:"demand_kw"`
				DemandBRL       *float64 `json:"demand_brl"`
				MonthlyBRL      *float64 `json:"monthly_brl"`
				DemandEstimated bool     `json:"demand_estimated"`
			} `json:"real"`
			Controllers []struct {
				Aggregate struct {
					Bill *struct {
						DemandKW float64 `json:"demand_kw"`
					} `json:"bill"`
				} `json:"aggregate"`
			} `json:"controllers"`
		} `json:"nights"`
	}
	decode(t, rec, &r)
	if !r.DemandBilled || r.NightsPerMonth != 30 || len(r.Nights) != 2 {
		t.Fatalf("report: %+v", r)
	}
	// the hand-computed demo numbers (see internal/realdata/bill_test.go)
	near := func(p *float64, want float64) bool { return p != nil && math.Abs(*p-want) < 1e-6 }
	if n := r.Nights[1].Real; !near(n.DemandKW, 140) || !near(n.DemandBRL, 2800) || n.DemandEstimated {
		t.Errorf("night 2 real = %+v", n)
	}
	if n := r.Nights[0].Real; !near(n.DemandKW, 120) || !near(n.DemandBRL, 4400) || !n.DemandEstimated {
		t.Errorf("night 1 real = %+v", n)
	}
	for _, n := range r.Nights {
		for _, c := range n.Controllers {
			if c.Aggregate.Bill == nil {
				t.Errorf("simulated row without bill")
			}
		}
	}
	// and the demo as it is has no such keys
	plain := do(New(), newReq("POST", "/api/replay", body(t, demoFiles(t), nil)))
	var any map[string]json.RawMessage
	decode(t, plain, &any)
	for _, k := range []string{"demand_billed", "nights_per_month"} {
		if _, ok := any[k]; ok {
			t.Errorf("key %s without demand prices", k)
		}
	}
	for _, k := range []string{`"demand_kw"`, `"monthly_brl"`, `"bill"`} {
		if strings.Contains(plain.Body.String(), k) {
			t.Errorf("%s in the response without demand prices", k)
		}
	}
}
