package lab

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

func TestSeriesJSON(t *testing.T) {
	got, err := json.Marshal(Series{1.04, math.NaN(), math.Inf(1), -0.04, 150, 37.56, 0.1 + 0.2})
	if err != nil {
		t.Fatal(err)
	}
	want := `[1,null,null,0,150,37.6,0.3]`
	if string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if b, _ := json.Marshal(Series(nil)); string(b) != "[]" {
		t.Fatalf("nil series: %s", b)
	}
}

func TestConvertReasonTableAndClamp(t *testing.T) {
	p := defaultParams()
	p.Buses, p.Chargers, p.LimitKW, p.Profile = 10, 5, 500, "severe"
	g, cfg := p.toSim()
	sc, ctrl, _ := sim.ForController("planner", cfg, sim.Generate(g, 1))
	tr := sim.NewTrace()
	m := sim.RunTraced(sc, ctrl, nil, tr)
	resp := buildRun(p, 1, "planner", sc, m, tr)
	if len(resp.Decisions) == 0 {
		t.Fatal("no decisions")
	}
	seen := map[string]bool{}
	for _, r := range resp.Reasons {
		if seen[r] {
			t.Fatalf("reason table repeats %q", r)
		}
		seen[r] = true
	}
	for _, f := range resp.Scenario.Faults {
		if f.To > resp.Scenario.HorizonMin {
			t.Fatalf("fault window not clamped: %+v", f)
		}
	}
	// A planner Plan with a non-finite shortfall must still serialize.
	d := decisionToDTO(sim.Decision{Buses: []planner.BusStatus{{BusID: "B1", ShortfallKWh: math.NaN()}}}, func(string) int { return -1 })
	if _, err := json.Marshal(d); err != nil {
		t.Fatalf("a NaN shortfall broke the JSON: %v", err)
	}
}

// The delta encoding of decisions must lose nothing: accumulating the listed buses and
// removing the gone ones gives, after every decision, exactly the status list the planner
// produced for it; note indices give back the exact planner texts.
func TestDecisionDeltasAreLossless(t *testing.T) {
	for _, profile := range []string{"severe", "mild"} {
		p := defaultParams()
		p.Buses, p.Chargers, p.LimitKW, p.Profile = 30, 15, 700, profile
		g, cfg := p.toSim()
		sc, ctrl, _ := sim.ForController("planner", cfg, sim.Generate(g, 4))
		tr := sim.NewTrace()
		m := sim.RunTraced(sc, ctrl, nil, tr)
		resp := buildRun(p, 4, "planner", sc, m, tr)
		if len(resp.Decisions) != len(tr.Decisions) || len(tr.Decisions) < 2 {
			t.Fatalf("%d decisions encoded, %d recorded", len(resp.Decisions), len(tr.Decisions))
		}
		state := map[string]busStatusDTO{}
		sawGone, sawPartial := false, false
		for i, dto := range resp.Decisions {
			if i > 0 && len(dto.Buses) < len(tr.Decisions[i].Buses) {
				sawPartial = true
			}
			for _, b := range dto.Buses {
				state[b.Bus] = b
			}
			if len(dto.Gone) > 0 {
				sawGone = true
			}
			for _, id := range dto.Gone {
				if _, ok := state[id]; !ok {
					t.Fatalf("decision %d: gone bus %s was never listed", i, id)
				}
				delete(state, id)
			}
			want := tr.Decisions[i]
			if len(state) != len(want.Buses) {
				t.Fatalf("%s decision %d (minute %d): %d buses rebuilt, planner had %d", profile, i, dto.Minute, len(state), len(want.Buses))
			}
			for _, b := range want.Buses {
				got, ok := state[b.BusID]
				reason := ""
				if ok && got.Reason >= 0 {
					reason = resp.Reasons[got.Reason]
				}
				if !ok || got.WillReach != b.WillReachTarget || got.Assessed != b.Assessed ||
					got.ShortfallKWh != math.Round(b.ShortfallKWh*10)/10 || reason != b.Reason || (b.Reason == "") != (got.Reason == -1) {
					t.Fatalf("%s decision %d bus %s: rebuilt %+v (%q), planner %+v", profile, i, b.BusID, got, reason, b)
				}
			}
			if len(dto.Notes) != len(want.Notes) {
				t.Fatalf("decision %d: %d notes, planner had %d", i, len(dto.Notes), len(want.Notes))
			}
			for k, ni := range dto.Notes {
				if resp.Notes[ni] != want.Notes[k] {
					t.Fatalf("decision %d note %d: %q, planner %q", i, k, resp.Notes[ni], want.Notes[k])
				}
			}
		}
		if !sawPartial {
			t.Errorf("%s: no decision was delta-encoded smaller than the full list; the test proves nothing", profile)
		}
		_ = sawGone // buses may or may not leave the list in a given run; the loop above checks them when they do
	}
}

func TestDeltaEncoderGoneAndFirstFull(t *testing.T) {
	e := newDeltaEncoder()
	st := func(id string, sf float64) busStatusDTO { return busStatusDTO{Bus: id, ShortfallKWh: sf, Reason: -1} }
	d0 := e.encode(fullDecision{Minute: 0, Buses: []busStatusDTO{st("B1", 0), st("B2", 1)}, Notes: []string{"a 1", "b"}})
	if len(d0.Buses) != 2 || len(d0.Gone) != 0 || len(d0.Notes) != 2 {
		t.Fatalf("first decision: %+v", d0)
	}
	d1 := e.encode(fullDecision{Minute: 1, Buses: []busStatusDTO{st("B1", 0), st("B3", 2)}, Notes: []string{"a 1"}})
	if len(d1.Buses) != 1 || d1.Buses[0].Bus != "B3" || len(d1.Gone) != 1 || d1.Gone[0] != "B2" {
		t.Fatalf("second decision: %+v", d1)
	}
	if d1.Notes[0] != d0.Notes[0] || len(e.NoteText) != 2 {
		t.Errorf("notes not interned: %v %v", d1.Notes, e.NoteText)
	}
	if b, _ := json.Marshal(d1); !strings.Contains(string(b), `"gone":["B2"]`) {
		t.Errorf("json %s", b)
	}
	d2 := e.encode(fullDecision{Minute: 2})
	if b, _ := json.Marshal(d2); !strings.Contains(string(b), `"gone":["B1","B3"]`) || !strings.Contains(string(b), `"notes":[]`) || !strings.Contains(string(b), `"buses":[]`) {
		t.Errorf("json %s", b)
	}
}
