package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPrintsComparisonTable(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"-buses", "6", "-chargers", "3", "-limit", "300", "-seeds", "2"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, name := range []string{"fifo", "edf", "safe", "planner", "ready%"} {
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
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Errorf("decision log not written: %v", err)
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
