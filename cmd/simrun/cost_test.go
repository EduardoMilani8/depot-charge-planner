package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/planner"
	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

// blankP99 removes the p99 column, which is wall-clock time and differs between runs.
func blankP99(out string) string {
	names := map[string]bool{}
	for _, n := range sim.ControllerNames {
		names[n] = true
	}
	trailingNumber := regexp.MustCompile(`\s+\d+$`)
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		f := strings.Fields(l)
		switch {
		case len(f) > 0 && f[0] == "controller":
			lines[i] = strings.TrimSuffix(l, "p99 µs")
			lines[i] = strings.TrimRight(lines[i], " ")
		case len(f) > 0 && names[f[0]]:
			lines[i] = trailingNumber.ReplaceAllString(l, "")
		}
	}
	return strings.Join(lines, "\n")
}

// Without the cost flags the output is what it was before the feature (golden captured
// from the previous commit), p99 excluded.
func TestRunWithoutCostFlagsIsUnchanged(t *testing.T) {
	const want = `profile: none, limit: 300 kW, follow swaps: true, swap back: after 30 min, need > 10 kWh
controller   ready%  shortfall kWh  peak kW  plan violations  overshoot min  energy kWh  cost R$  plan changes  moves/run
fifo         66.7    330.4          300      0                0              951         1153     8             0.0
edf          66.7    330.4          300      0                0              951         1153     9             0.0
fifo-unplug  100.0   0.0            300      0                0              1310        1476     9             3.0
safe         66.7    347.2          300      0                0              1122        1307     6             0.0
planner      100.0   0.0            300      0                0              1367        1527     77            3.0
`
	var out, errOut bytes.Buffer
	if code := run([]string{"-buses", "6", "-chargers", "3", "-limit", "300", "-seeds", "2"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if got := blankP99(out.String()); got != want {
		t.Errorf("output changed:\n%s\nwant:\n%s", got, want)
	}
	if errOut.Len() != 0 {
		t.Errorf("unexpected stderr %q", errOut.String())
	}
}

// costArgs is a depot where every bus has a plug and the buses arrive in the peak window
// (18:00-21:00), so the cost-aware planner has something to do.
func costArgs(extra ...string) []string {
	return append([]string{"-buses", "20", "-chargers", "20", "-limit", "1000", "-seeds", "3",
		"-arrival-from", "0", "-arrival-to", "180"}, extra...)
}

func runOK(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("%v: exit %d: %s", args, code, errOut.String())
	}
	return out.String(), errOut.String()
}

// Cost-aware without demand prices changes the planner's energy cost but adds no bill
// columns and no savings line.
func TestRunCostAwareWithoutDemandHasNoBillColumns(t *testing.T) {
	plain, _ := runOK(t, costArgs()...)
	aware, _ := runOK(t, costArgs("-cost-aware")...)
	for _, out := range []string{plain, aware} {
		for _, s := range []string{"demand kW", "demand R$/mês", "monthly R$", "economia mensal"} {
			if strings.Contains(out, s) {
				t.Errorf("no demand prices: %q must not appear:\n%s", s, out)
			}
		}
	}
	if !strings.Contains(aware, "cost-aware") || strings.Contains(plain, "cost-aware") {
		t.Errorf("only the cost-aware run says so in its header:\n%s\n%s", plain, aware)
	}
	row := func(out, name string) string {
		for _, l := range strings.Split(blankP99(out), "\n") {
			if strings.HasPrefix(l, name+" ") {
				return l
			}
		}
		t.Fatalf("no %s row in\n%s", name, out)
		return ""
	}
	for _, n := range []string{"fifo", "edf", "fifo-unplug", "safe"} {
		if row(plain, n) != row(aware, n) {
			t.Errorf("-cost-aware changed the %s row:\n%s\n%s", n, row(plain, n), row(aware, n))
		}
	}
	if row(plain, "planner") == row(aware, "planner") {
		t.Errorf("-cost-aware did not change the planner row:\n%s", row(aware, "planner"))
	}
}

// The bill columns and the savings line must agree with the metrics sim reports.
func TestRunBillColumnsAndSavingsMatchTheMetrics(t *testing.T) {
	args := costArgs("-cost-aware", "-demand-peak-price", "40", "-demand-offpeak-price", "20", "-nights-per-month", "22")
	out, errOut := runOK(t, args...)
	if errOut != "" {
		t.Errorf("unexpected stderr %q", errOut)
	}
	// Same scenario through the library.
	p := sim.DefaultGenParams()
	p.NumBuses, p.NumChargers, p.LimitKW = 20, 20, 1000
	p.ArrivalFromMin, p.ArrivalToMin = 0, 180
	p.Demand = sim.DemandParams{PeakPrice: 40, OffPeakPrice: 20, NightsPerMonth: 22}
	cfg := planner.DefaultConfig()
	cfg.Cost = &planner.CostPolicy{DeferMarginMin: 45}
	res, err := sim.Compare(context.Background(), p, cfg, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]sim.Metrics{}
	for _, r := range res {
		if r.Aggregate.Bill == nil {
			t.Fatalf("%s: no bill with demand prices", r.Name)
		}
		byName[r.Name] = r.Aggregate
	}

	lines := strings.Split(blankP99(out), "\n")
	header := strings.Fields(strings.NewReplacer("shortfall kWh", "shortfall_kWh", "peak kW", "peak_kW", "plan violations", "plan_violations",
		"overshoot min", "overshoot_min", "energy kWh", "energy_kWh", "cost R$", "cost_R$", "demand kW", "demand_kW",
		"demand R$/mês", "demand_R$/mês", "monthly R$", "monthly_R$", "plan changes", "plan_changes", "p99 µs", "p99_µs").Replace(lines[1]))
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	if !strings.Contains(lines[1], "demand kW") || !strings.Contains(lines[1], "demand R$/mês") || !strings.Contains(lines[1], "monthly R$") {
		t.Fatalf("header lacks the bill columns: %q", lines[1])
	}
	for _, l := range lines[2:] {
		f := strings.Fields(l)
		if len(f) == 0 || byName[f[0]].Bill == nil {
			continue
		}
		b := byName[f[0]].Bill
		got := []string{f[col["demand_kW"]], f[col["demand_R$/mês"]], f[col["monthly_R$"]]}
		want := []string{fmt.Sprintf("%.0f", b.DemandKW), fmt.Sprintf("%.0f", b.DemandBRL), fmt.Sprintf("%.0f", b.MonthlyBRL)}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: bill columns %v, want %v", f[0], got, want)
				break
			}
		}
	}
	// The savings line: planner against fifo and fifo-unplug, from MonthlyBRL.
	var line string
	for _, l := range lines {
		if strings.HasPrefix(l, "economia mensal estimada") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no savings line:\n%s", out)
	}
	if !strings.Contains(line, "estimativa") && !strings.Contains(out, "estimativa") {
		t.Errorf("the monthly figure must be labelled as an estimate:\n%s", out)
	}
	pl := byName["planner"].Bill.MonthlyBRL
	for _, base := range []string{"fifo", "fifo-unplug"} {
		bm := byName[base].Bill.MonthlyBRL
		want := fmt.Sprintf("contra %s R$ %.0f (%.1f%%)", base, bm-pl, 100*(bm-pl)/bm)
		if !strings.Contains(line, want) {
			t.Errorf("savings line %q lacks %q", line, want)
		}
	}
}

func TestRunDemandPricesAloneStillBillAndCompare(t *testing.T) {
	out, _ := runOK(t, costArgs("-demand-peak-price", "40", "-demand-offpeak-price", "20")...)
	if !strings.Contains(out, "monthly R$") || !strings.Contains(out, "economia mensal estimada") {
		t.Errorf("demand prices alone (no -cost-aware) still bill and compare:\n%s", out)
	}
}

func TestRunRejectsInvalidCostFlags(t *testing.T) {
	cases := []struct {
		args []string
		want string // fragment of the message
	}{
		{[]string{"-demand-target", "500"}, "-cost-aware"},
		{[]string{"-defer-margin", "30"}, "-cost-aware"},
		{[]string{"-cost-aware", "-defer-margin", "-1"}, "defer-margin"},
		{[]string{"-cost-aware", "-defer-margin", "241"}, "defer-margin"},
		{[]string{"-cost-aware", "-defer-margin", "NaN"}, "defer-margin"},
		{[]string{"-cost-aware", "-demand-target", "-1"}, "demand-target"},
		{[]string{"-cost-aware", "-demand-target", "Inf"}, "demand-target"},
		{[]string{"-demand-peak-price", "-1"}, "demand-peak-price"},
		{[]string{"-demand-peak-price", "NaN"}, "demand-peak-price"},
		{[]string{"-demand-offpeak-price", "Inf"}, "demand-offpeak-price"},
		{[]string{"-demand-offpeak-price", "1e9"}, "demand-offpeak-price"},
		{[]string{"-demand-contracted", "-5"}, "demand-contracted"},
		{[]string{"-demand-contracted", "NaN"}, "demand-contracted"},
		{[]string{"-demand-mode", "amarela"}, "demand-mode"},
		{[]string{"-arrival-from", "-1", "-arrival-to", "100"}, "arrival-from"},
		{[]string{"-arrival-from", "600", "-arrival-to", "601"}, "arrival-from"},
		{[]string{"-arrival-from", "100", "-arrival-to", "100"}, "arrival-to"},
		{[]string{"-arrival-from", "100", "-arrival-to", "50"}, "arrival-to"},
		{[]string{"-arrival-from", "0", "-arrival-to", "601"}, "arrival-to"},
		{[]string{"-arrival-from", "100"}, "arrival-to"},
		{[]string{"-arrival-from", "21:00", "-arrival-to", "100"}, "arrival-from"},
		{[]string{"-nights-per-month", "-1"}, "nights-per-month"},
		{[]string{"-nights-per-month", "32"}, "nights-per-month"},
	}
	for _, tc := range cases {
		var out, errOut bytes.Buffer
		if code := run(tc.args, &out, &errOut); code != 2 {
			t.Errorf("%v: exit %d, want 2", tc.args, code)
		}
		if !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("%v: stderr %q lacks %q", tc.args, errOut.String(), tc.want)
		}
		if out.Len() != 0 {
			t.Errorf("%v: unexpected stdout %q", tc.args, out.String())
		}
	}
}

// Verde bills one demand charge at the off-peak price: a peak price is not an error but
// it must be said that it is ignored.
func TestRunVerdeSaysThatThePeakPriceIsIgnored(t *testing.T) {
	_, errOut := runOK(t, "-buses", "6", "-chargers", "3", "-limit", "300", "-seeds", "1",
		"-demand-mode", "verde", "-demand-peak-price", "40", "-demand-offpeak-price", "20")
	if !strings.Contains(errOut, "demand-peak-price") || !strings.Contains(errOut, "ignored") {
		t.Errorf("stderr %q must say the peak price is ignored in verde", errOut)
	}
	_, errOut = runOK(t, "-buses", "6", "-chargers", "3", "-limit", "300", "-seeds", "1",
		"-demand-mode", "verde", "-demand-offpeak-price", "20")
	if errOut != "" {
		t.Errorf("verde without a peak price needs no note: %q", errOut)
	}
	// Azul with the same prices bills differently from verde.
	az, _ := runOK(t, "-buses", "6", "-chargers", "3", "-limit", "300", "-seeds", "1",
		"-demand-peak-price", "40", "-demand-offpeak-price", "20")
	ve, _ := runOK(t, "-buses", "6", "-chargers", "3", "-limit", "300", "-seeds", "1",
		"-demand-mode", "verde", "-demand-peak-price", "40", "-demand-offpeak-price", "20")
	if blankP99(az) == blankP99(ve) {
		t.Error("azul and verde gave the same table")
	}
	if !strings.Contains(ve, "verde") || !strings.Contains(az, "azul") {
		t.Errorf("the header must name the mode:\n%s\n%s", az, ve)
	}
}

func TestRunDemandTargetAboveTheLimitIsAllowedAndNoted(t *testing.T) {
	_, errOut := runOK(t, "-buses", "6", "-chargers", "3", "-limit", "300", "-seeds", "1",
		"-cost-aware", "-demand-target", "5000")
	if !strings.Contains(errOut, "demand-target") || !strings.Contains(errOut, "limit") {
		t.Errorf("stderr %q must say the target is above the limit and has no effect", errOut)
	}
}

func TestRunCostAwareIsDeterministic(t *testing.T) {
	args := costArgs("-cost-aware", "-demand-target", "600", "-demand-peak-price", "40", "-demand-offpeak-price", "20")
	a, _ := runOK(t, args...)
	b, _ := runOK(t, args...)
	if blankP99(a) != blankP99(b) {
		t.Errorf("two runs differ:\n%s\n%s", a, b)
	}
}

// The decision log of a cost-aware run must replay with no differences: its header
// carries the policy the planner really ran (clock and peak window from the scenario).
func TestRunDecisionLogOfACostAwareRunReplays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	runOK(t, "-buses", "20", "-chargers", "20", "-limit", "1000", "-seeds", "1",
		"-arrival-from", "0", "-arrival-to", "180", "-cost-aware", "-defer-margin", "30", "-demand-target", "700", "-log", path)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	diffs, replayed, _, err := sim.ReplayFile(f)
	if err != nil || replayed == 0 || len(diffs) != 0 {
		t.Fatalf("replayed %d records, %d diffs, err %v: want >0, 0, nil", replayed, len(diffs), err)
	}
	f.Seek(0, 0)
	h, recs, err := sim.ReadDecisionLogWithHeader(f)
	if err != nil || h == nil || h.Config.Cost == nil {
		t.Fatalf("header %+v err %v: the policy must be logged", h, err)
	}
	c := h.Config.Cost
	if c.PeakFromMin != 18*60 || c.PeakToMin != 21*60 || c.ClockAtZeroMin != 18*60 || c.DeferMarginMin != 30 || c.DemandTargetKW != 700 {
		t.Errorf("logged policy %+v does not carry the scenario clock/peak and the flags", *c)
	}
	// The schedule was announced: some record lists a bus that has not arrived yet.
	announced := false
	for _, r := range recs {
		for _, b := range r.Input.Buses {
			announced = announced || b.ArrivalMin > r.Minute
		}
	}
	if !announced {
		t.Error("the log has no announced arrivals: the run was not cost-aware")
	}
}
