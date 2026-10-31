package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

const modelsDir = "../../docs/dados-reais/modelos"

var p99 = regexp.MustCompile(`"plan_p99_micros": [0-9]+`)

// Without the new optional columns and flags the output is byte-identical to what the replay
// printed before the energy-cost feature (goldens captured from the previous commit).
func TestRunWithoutCostColumnsIsUnchanged(t *testing.T) {
	for _, c := range []struct{ dir, golden string }{
		{demoDir, "testdata/demo.golden.txt"},
		{modelsDir, "testdata/modelos.golden.txt"},
	} {
		want, err := os.ReadFile(c.golden)
		if err != nil {
			t.Fatal(err)
		}
		code, out, errOut := runCmd("-dir", c.dir)
		if code != 0 || errOut != "" {
			t.Fatalf("%s: exit %d, stderr %q", c.dir, code, errOut)
		}
		if out != string(want) {
			t.Errorf("%s: text output changed:\n%s\n--- want\n%s", c.dir, out, want)
		}
	}
	want, err := os.ReadFile("testdata/demo.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCmd("-dir", demoDir, "-json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := p99.ReplaceAllString(out, `"plan_p99_micros": 0`); got != string(want) {
		t.Errorf("JSON output changed")
	}
}

func demandDir(t *testing.T, row string) string {
	t.Helper()
	return copyDemo(t, map[string]func(string) string{"garagem.csv": func(string) string {
		return "limite_kw,ponta_inicio,ponta_fim,preco_ponta,preco_fora_ponta,demanda_preco_ponta,demanda_preco_fora_ponta,demanda_contratada_kw,modo_demanda,noites_por_mes\n" +
			"400,18:00,21:00,2.70,0.90," + row + "\n"
	}})
}

func TestRunDemandColumnsAndSeals(t *testing.T) {
	code, out, errOut := runCmd("-dir", demandDir(t, "40,20,,,22"))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	tbs := tables(out)
	if len(tbs) != 3 {
		t.Fatalf("tables = %d:\n%s", len(tbs), out)
	}
	hdr := tbs[0][0]
	kw, monthly, cost := col(t, hdr, "demand kW"), col(t, hdr, "monthly R$"), col(t, hdr, "cost R$")
	if kw != cost+1 || monthly != kw+1 {
		t.Errorf("columns after cost R$: %v", hdr)
	}
	// night 1 (sessions): demand 120 kW, month 522 x 22 + 50 x 40 + 120 x 20 = 11484 + 4400 = 15884, both estimated
	if r := tbs[0][1]; r[kw] != "120 (estimado)" || r[monthly] != "15884 (estimado)" {
		t.Errorf("night 1 real row: %v", r)
	}
	// night 2 (readings): demand 140 kW measured, month 58.5 x 22 + 140 x 20 = 1287 + 2800 = 4087, sealed partial (2 of 3 buses)
	if r := tbs[1][1]; r[kw] != "140 (medido) (parcial: 2 de 3 ônibus)" || r[monthly] != "4087 (medido) (parcial: 2 de 3 ônibus)" {
		t.Errorf("night 2 real row: %v", r)
	}
	// every simulated row has the two columns, with numbers
	for i, tb := range tbs {
		for _, row := range tb[2:] {
			if len(row) != len(hdr) {
				t.Fatalf("table %d: %v", i, row)
			}
			for _, c := range []int{kw, monthly} {
				if row[c] == "—" || row[c] == "" {
					t.Errorf("table %d %s: column %s = %q", i, row[0], hdr[c], row[c])
				}
			}
		}
	}
	for _, want := range []string{
		"monthly R$ = estimativa: energia da noite x 22 noites + demanda",
		"economia mensal estimada do planner (R$/mês, negativa = custa mais): contra fifo R$ ",
		", contra fifo-unplug R$ ",
		"um fifo que deixa ônibus sem carga gasta menos, então não é uma comparação equivalente",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// the savings sentence is computed from the aggregate rows' monthly figures (unrounded)
	_, js, _ := runCmd("-dir", demandDir(t, "40,20,,,22"), "-json")
	var rep struct {
		Aggregate []struct {
			Name      string `json:"name"`
			Aggregate struct {
				Bill *struct {
					MonthlyBRL float64 `json:"monthly_brl"`
				} `json:"bill"`
			} `json:"aggregate"`
		} `json:"aggregate"`
	}
	if err := json.Unmarshal([]byte(js), &rep); err != nil {
		t.Fatal(err)
	}
	monthlyOf := map[string]float64{}
	for _, c := range rep.Aggregate {
		if c.Aggregate.Bill == nil {
			t.Fatalf("%s has no bill in the JSON", c.Name)
		}
		monthlyOf[c.Name] = c.Aggregate.Bill.MonthlyBRL
	}
	for _, base := range []string{"fifo", "fifo-unplug"} {
		diff, pct := monthlyOf[base]-monthlyOf["planner"], 100*(monthlyOf[base]-monthlyOf["planner"])/monthlyOf[base]
		want := fmt.Sprintf("contra %s R$ %.0f (%.1f%%)", base, diff, pct)
		if !strings.Contains(out, want) {
			t.Errorf("savings should read %q:\n%s", want, out)
		}
	}
}

func TestRunWithoutDemandPricesHasNoDemandColumns(t *testing.T) {
	_, out, _ := runCmd("-dir", demoDir)
	for _, bad := range []string{"demand kW", "monthly R$", "economia mensal"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q in the output without demand prices", bad)
		}
	}
	// the columns present but empty change nothing
	_, out2, _ := runCmd("-dir", demandDir(t, ",,,,"))
	if out2 != out {
		t.Errorf("empty demand cells changed the output:\n%s", out2)
	}
}

func TestRunVerdeAndContractAreBilled(t *testing.T) {
	// Verde 40 / 20 with a 100 kW contract: night 2 measured 140 kW -> 100 x 20 + 40 x 2 x 20 = 3600
	_, out, _ := runCmd("-dir", demandDir(t, "40,20,100,verde,30"))
	tbs := tables(out)
	hdr := tbs[0][0]
	if r := tbs[1][1]; !strings.HasPrefix(r[col(t, hdr, "monthly R$")], "5355 (medido)") { // 58.5 x 30 + 3600
		t.Errorf("night 2 real row: %v", r)
	}
	if !strings.Contains(out, "modo_demanda verde: demanda_preco_ponta é ignorado") {
		t.Errorf("Verde must say the peak demand price is ignored:\n%s", out)
	}
}

func TestRunCostAwareFlags(t *testing.T) {
	// a flag that only configures the cost-aware planner needs -cost-aware
	for _, args := range [][]string{
		{"-defer-margin", "30"},
		{"-demand-target", "200"},
		{"-announce-arrivals"},
	} {
		code, out, errOut := runCmd(append([]string{"-dir", demoDir}, args...)...)
		if code != 2 || out != "" || !strings.Contains(errOut, args[0]) || !strings.Contains(errOut, "-cost-aware") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want exit 2 naming the flag and -cost-aware", args, code, out, errOut)
		}
	}
	// invalid values, with -cost-aware
	for _, c := range []struct{ args, want string }{
		{"-defer-margin -1", "-defer-margin"},
		{"-defer-margin 241", "-defer-margin"},
		{"-demand-target -5", "-demand-target"},
		{"-demand-target 1e8", "-demand-target"},
		{"-demand-target NaN", "-demand-target"},
	} {
		args := append([]string{"-dir", demoDir, "-cost-aware"}, strings.Fields(c.args)...)
		code, out, errOut := runCmd(args...)
		if code != 2 || out != "" || !strings.Contains(errOut, c.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", c.args, code, out, errOut)
		}
	}
	// edges
	for _, a := range []string{"0", "240"} {
		if code, _, errOut := runCmd("-dir", demoDir, "-cost-aware", "-defer-margin", a); code != 0 {
			t.Errorf("-defer-margin %s: exit %d: %s", a, code, errOut)
		}
	}
}

func TestRunCostAwareIsBlindUnlessArrivalsAreAnnounced(t *testing.T) {
	// the planner cost on night 1 of the demo (peak 18:00-21:00, B01 arrives at 20:00)
	plannerCost := func(out string, night int) string {
		tbs := tables(out)
		hdr := tbs[night][0]
		return tbs[night][2+4][col(t, hdr, "cost R$")]
	}
	_, plain, plainErr := runCmd("-dir", demoDir)
	if plainErr != "" {
		t.Fatal(plainErr)
	}
	code, blind, blindErr := runCmd("-dir", demoDir, "-cost-aware")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, blindErr)
	}
	code, known, knownErr := runCmd("-dir", demoDir, "-cost-aware", "-announce-arrivals")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, knownErr)
	}
	// blind: the planner row is the plain planner row; one stderr line says why nothing is deferred
	if plannerCost(blind, 0) != plannerCost(plain, 0) {
		t.Errorf("blind cost-aware planner costs %s, plain %s", plannerCost(blind, 0), plannerCost(plain, 0))
	}
	lines := strings.Split(strings.TrimSpace(blindErr), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "-announce-arrivals") || !strings.Contains(lines[0], "adia") {
		t.Errorf("want one stderr note naming -announce-arrivals, got %q", blindErr)
	}
	if knownErr != "" {
		t.Errorf("announced run must be silent, stderr = %q", knownErr)
	}
	if plannerCost(known, 0) == plannerCost(plain, 0) {
		t.Errorf("announced arrivals: planner cost unchanged (%s), the deferral should act on night 1", plannerCost(known, 0))
	}
	for _, want := range []string{"sem agenda", "chegadas anunciadas"} {
		if !strings.Contains(blind+known, want) {
			t.Errorf("the assumptions should say %q", want)
		}
	}
	if !strings.Contains(blind, "sem agenda") || strings.Contains(blind, "chegadas anunciadas") {
		t.Errorf("blind output should describe the blind mode only")
	}
	// the demand target works without the schedule (a plain note is not needed for it)
	code, _, errOut := runCmd("-dir", demoDir, "-cost-aware", "-demand-target", "200")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestRunCostAwareDeterministicAcrossWorkers(t *testing.T) {
	args := []string{"-dir", demandDir(t, "40,20,,,"), "-cost-aware", "-announce-arrivals", "-demand-target", "250"}
	_, a, _ := runCmd(append(args, "-workers", "1")...)
	_, b, _ := runCmd(append(args, "-workers", "4")...)
	if a == "" || a != b {
		t.Errorf("output depends on -workers")
	}
}
