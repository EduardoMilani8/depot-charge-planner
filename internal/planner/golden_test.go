package planner

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/model"
)

var update = flag.Bool("update", false, "rewrite golden files")

func goldenCases() map[string]Input {
	eff := func(c model.Charger) model.Charger { c.Efficiency = 0.95; return c }
	offline := eff(testCharger("C2"))
	offline.Status = model.ChargerOffline
	offline.LastCommandedKW = 40
	faulted := eff(testCharger("C3"))
	faulted.Status = model.ChargerFaulted
	stale := testBus("S", "C4", 120, 250, 500)
	stale.SoCAgeMin = 100
	return map[string]Input{
		"tight_limit": {
			Site:     model.Site{LimitKW: 200, StepMin: 1},
			Chargers: []model.Charger{eff(testCharger("C1")), eff(testCharger("C2")), eff(testCharger("C3"))},
			Buses: []model.Bus{
				testBus("B1", "C1", 60, 250, 180),
				testBus("B2", "C2", 120, 250, 300),
				testBus("B3", "C3", 200, 250, 600),
			},
		},
		"mixed_states": {
			Site:     model.Site{LimitKW: 400, StepMin: 1},
			Chargers: []model.Charger{eff(testCharger("C1")), offline, faulted, eff(testCharger("C4"))},
			Buses: []model.Bus{
				testBus("A", "C1", 300, 250, 600), // full battery: need 0 even with the margin
				testBus("B", "C2", 100, 250, 400),
				testBus("C", "C3", 90, 250, 300),
				stale,
				testBus("W", "", 100, 250, 95),
			},
		},
	}
}

func TestGolden(t *testing.T) {
	for name, in := range goldenCases() {
		t.Run(name, func(t *testing.T) {
			plan := New(DefaultConfig()).Plan(in)
			got, err := json.MarshalIndent(struct {
				Input Input
				Plan  Plan
			}{in, plan}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join("testdata", name+".golden.json")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden file %s: run `go test ./internal/planner -run Golden -update` and review it", path)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("plan changed for %s; if intended, rerun with -update and review the diff\n--- got ---\n%s", name, got)
			}
		})
	}
}
