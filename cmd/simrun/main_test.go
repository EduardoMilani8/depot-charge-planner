package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

func TestRunPrintsComparisonTable(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"-buses", "6", "-chargers", "3", "-limit", "300", "-seeds", "2"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, name := range []string{"fifo", "edf", "safe", "planner", "ready%", "energy kWh", "moves/run"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("output is missing %q:\n%s", name, out.String())
		}
	}
}

func TestRunRejectsBadArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-profile", "catastrophic"}, &out, &errOut); code != 2 {
		t.Errorf("unknown profile: exit %d", code)
	}
	if code := run([]string{"-seeds", "0"}, &out, &errOut); code != 2 {
		t.Errorf("zero seeds: exit %d", code)
	}
	if code := run([]string{"-nonsense"}, &out, &errOut); code != 2 {
		t.Errorf("unknown flag: exit %d", code)
	}
}

func TestRunWritesDecisionLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	var out, errOut bytes.Buffer
	code := run([]string{"-buses", "4", "-chargers", "2", "-limit", "200", "-seeds", "1", "-log", path}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("decision log not written: %v", err)
	}
	defer f.Close()
	h, recs, err := sim.ReadDecisionLogWithHeader(f)
	if err != nil || h == nil || len(recs) == 0 {
		t.Fatalf("unreadable log: header %v, %d records, err %v", h, len(recs), err)
	}
	if !reflect.DeepEqual(h.Config, planner.DefaultConfig()) {
		t.Errorf("header config %+v, want the default", h.Config)
	}
}

func TestRunRejectsInvalidValuesBeforeSimulating(t *testing.T) {
	cases := [][]string{
		{"-limit", "NaN"},
		{"-limit", "Inf"},
		{"-limit", "-5"},
		{"-buses", "1000000000"},
		{"-chargers", "1000000000"},
		{"-seeds", "1000000000"},
		{"-limit", "1e8"},
		{"stray"},
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
		if errOut.Len() == 0 {
			t.Errorf("%v: nothing written to stderr", args)
		}
		if out.Len() != 0 {
			t.Errorf("%v: unexpected stdout %q", args, out.String())
		}
	}
}

func TestRunFollowSwapsFlagAndUnplugBaseline(t *testing.T) {
	var on, off, errOut bytes.Buffer
	args := []string{"-buses", "8", "-chargers", "3", "-limit", "400", "-seeds", "2"}
	if code := run(args, &on, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if code := run(append(args, "-follow-swaps=false"), &off, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, out := range []string{on.String(), off.String()} {
		if !strings.Contains(out, "fifo-unplug") {
			t.Errorf("missing the fifo-unplug baseline:\n%s", out)
		}
	}
	if !strings.Contains(on.String(), "follow swaps: true") || !strings.Contains(off.String(), "follow swaps: false") {
		t.Errorf("the table must say whether swaps were followed:\n%s\n%s", on.String(), off.String())
	}
}
