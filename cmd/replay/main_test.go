package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/EduardoMilani8/depot-charge-planner/internal/realdata"
)

const demoDir = "../../internal/realdata/testdata/demo"

func runCmd(args ...string) (code int, out, errOut string) {
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

// copyDemo copies the demo spreadsheets into a temp dir and applies edit to the
// named file's text (edit may be nil); a file mapped to "" is removed.
func copyDemo(t *testing.T, edits map[string]func(string) string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range realdata.FileNames {
		b, err := os.ReadFile(filepath.Join(demoDir, name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if f, ok := edits[name]; ok {
			if f == nil {
				continue // removed
			}
			s = f(s)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func replace(old, new string) func(string) string {
	return func(s string) string {
		if !strings.Contains(s, old) {
			panic("fixture lacks " + old)
		}
		return strings.Replace(s, old, new, 1)
	}
}

var gap = regexp.MustCompile(`\s{2,}`)

// tables returns every table of the output as rows of cells (split on 2+ spaces),
// each table starting at its "controller ..." header.
func tables(out string) [][][]string {
	var all [][][]string
	var cur [][]string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "controller"):
			if cur != nil {
				all = append(all, cur)
			}
			cur = [][]string{gap.Split(strings.TrimSpace(l), -1)}
		case cur != nil && (strings.HasPrefix(l, "real") || isControllerRow(l)):
			cur = append(cur, gap.Split(strings.TrimSpace(l), -1))
		default:
			if cur != nil {
				all = append(all, cur)
				cur = nil
			}
		}
	}
	if cur != nil {
		all = append(all, cur)
	}
	return all
}

func isControllerRow(l string) bool {
	for _, n := range realdata.ReplayNames {
		if strings.HasPrefix(l, n+" ") {
			return true
		}
	}
	return false
}

func col(t *testing.T, header []string, name string) int {
	t.Helper()
	for i, h := range header {
		if h == name {
			return i
		}
	}
	t.Fatalf("no column %q in %v", name, header)
	return -1
}

func TestRunPrintsRealVersusSimulatedTables(t *testing.T) {
	code, out, errOut := runCmd("-dir", demoDir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if errOut != "" {
		t.Errorf("stderr = %q", errOut)
	}
	for _, want := range []string{"2026-03-04", "2026-03-05", "Premissas:", "real", "planner (sem rodízio)", "66.7", "(estimado)",
		"controller", "ready%", "shortfall kWh", "peak kW", "plan violations", "overshoot min", "energy kWh", "cost R$", "plan changes", "moves/run",
		"Agregado", "Ônibus não prontos na realidade", "B02"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(strings.ToLower(out), "p99") {
		t.Errorf("p99 must never be printed:\n%s", out)
	}
	// the warnings of the demo: B03 of night 2 has no measured outcome (first, from the
	// load) and no session (from the nights)
	if n := strings.Count(out, "aviso:"); n != 2 || !strings.HasPrefix(out, "aviso: onibus.csv: soc_saida_real_pct vazio em 1 ônibus") ||
		!strings.Contains(out, "aviso: sessoes.csv: noite 2026-03-05: 1 de 3 ônibus sem sessão: energia, pico e custo reais cobrem só os demais") {
		t.Errorf("want the soc_saida_real_pct warning first and the partial one (%d found):\n%s", n, out)
	}
	tbs := tables(out)
	if len(tbs) != 3 { // 2 nights + aggregate
		t.Fatalf("tables = %d, want 3:\n%s", len(tbs), out)
	}
	for i, tb := range tbs {
		if len(tb) != 1+1+len(realdata.ReplayNames) || tb[1][0] != "real" {
			t.Fatalf("table %d = %v", i, tb)
		}
		for j, name := range realdata.ReplayNames {
			if tb[2+j][0] != name {
				t.Errorf("table %d row %d = %q, want %q", i, 2+j, tb[2+j][0], name)
			}
		}
		for _, row := range tb {
			if len(row) != len(tb[0]) {
				t.Errorf("table %d: row %v has %d cells, header %d", i, row, len(row), len(tb[0]))
			}
		}
	}
	// night 1 real row: ready 66.7, peak and cost are estimated (no power that night)
	hdr, real := tbs[0][0], tbs[0][1]
	if real[col(t, hdr, "ready%")] != "66.7" {
		t.Errorf("real ready%% = %q", real[col(t, hdr, "ready%")])
	}
	if !strings.HasSuffix(real[col(t, hdr, "peak kW")], "(estimado)") || !strings.HasSuffix(real[col(t, hdr, "cost R$")], "(estimado)") {
		t.Errorf("real row must seal estimated peak and cost: %v", real)
	}
	for _, c := range []string{"plan violations", "overshoot min", "plan changes", "moves/run"} {
		if real[col(t, hdr, c)] != "—" {
			t.Errorf("real %s = %q, want —", c, real[col(t, hdr, c)])
		}
	}
	// night 2 has measured power: the peak is not estimated, but sessions cover only 2 of 3 buses
	if p := tbs[1][1][col(t, hdr, "peak kW")]; p != "140 (parcial: 2 de 3 ônibus)" {
		t.Errorf("night 2 real peak = %q, want the measured 140 sealed as partial", p)
	}
	for _, c := range []string{"energy kWh", "cost R$"} {
		if v := tbs[1][1][col(t, hdr, c)]; !strings.HasSuffix(v, "(parcial: 2 de 3 ônibus)") {
			t.Errorf("night 2 real %s = %q, want the partial seal", c, v)
		}
		if v := tbs[0][1][col(t, hdr, c)]; strings.Contains(v, "parcial") {
			t.Errorf("night 1 covers its 3 buses, real %s = %q", c, v)
		}
	}
	if v := tbs[2][1][col(t, hdr, "energy kWh")]; v != "272 (parcial: 5 de 6 ônibus)" {
		t.Errorf("aggregate real energy = %q, want 272 sealed (5 of 6 buses)", v)
	}
	if !strings.Contains(out, "Nota: energia, pico e custo reais cobrem só parte da noite (parcial: 2 de 3 ônibus)") {
		t.Errorf("partial note missing:\n%s", out)
	}
	// the not-ready list of night 1 comes before the night 2 heading
	i1, i2 := strings.Index(out, "Ônibus não prontos"), strings.Index(out, "Noite 2026-03-05")
	if i1 < 0 || i2 < i1 {
		t.Errorf("not-ready list must close night 1")
	}
	// simulated rows have a cost
	if c := tbs[0][2+4][col(t, hdr, "cost R$")]; c == "—" {
		t.Errorf("comparable cost must be shown: %v", tbs[0][2+4])
	}
}

func TestRunDeterministicAcrossWorkers(t *testing.T) {
	_, a, _ := runCmd("-dir", demoDir, "-workers", "1")
	_, b, _ := runCmd("-dir", demoDir, "-workers", "4")
	if a == "" || a != b {
		t.Errorf("text output depends on -workers:\n%s\n---\n%s", a, b)
	}
}

func TestRunDirRequiredAndUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no dir", nil, "-dir é obrigatório"},
		{"blank dir", []string{"-dir", "  "}, "-dir é obrigatório"},
		{"unknown flag", []string{"-dir", demoDir, "-nonsense"}, "nonsense"},
		{"stray argument", []string{"-dir", demoDir, "stray"}, "argumento inesperado"},
		{"workers 0", []string{"-dir", demoDir, "-workers", "0"}, "-workers"},
		{"workers negative", []string{"-dir", demoDir, "-workers", "-2"}, "-workers"},
		{"workers huge", []string{"-dir", demoDir, "-workers", "100000"}, "-workers"},
		{"soc-noise NaN", []string{"-dir", demoDir, "-soc-noise", "NaN"}, "-soc-noise"},
		{"soc-noise Inf", []string{"-dir", demoDir, "-soc-noise", "Inf"}, "-soc-noise"},
		{"soc-noise negative", []string{"-dir", demoDir, "-soc-noise", "-1"}, "-soc-noise"},
		{"soc-noise huge", []string{"-dir", demoDir, "-soc-noise", "1e9"}, "-soc-noise"},
		{"cooldown negative", []string{"-dir", demoDir, "-swap-back-cooldown", "-1"}, "-swap-back-cooldown"},
		{"min-need NaN", []string{"-dir", demoDir, "-swap-back-min-need", "NaN"}, "-swap-back-min-need"},
		{"min-need Inf", []string{"-dir", demoDir, "-swap-back-min-need", "Inf"}, "-swap-back-min-need"},
		{"min-need negative", []string{"-dir", demoDir, "-swap-back-min-need", "-5"}, "-swap-back-min-need"},
		{"not a number", []string{"-dir", demoDir, "-soc-noise", "abc"}, "soc-noise"},
	}
	for _, tc := range cases {
		code, out, errOut := runCmd(tc.args...)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2", tc.name, code)
		}
		if out != "" {
			t.Errorf("%s: stdout = %q, want nothing", tc.name, out)
		}
		if !strings.Contains(errOut, tc.want) {
			t.Errorf("%s: stderr = %q, want it to mention %q", tc.name, errOut, tc.want)
		}
	}
}

func TestRunHelpExitsZero(t *testing.T) {
	code, _, errOut := runCmd("-h")
	if code != 0 || !strings.Contains(errOut, "Uso: replay -dir PASTA") {
		t.Errorf("-h: exit %d, stderr %q", code, errOut)
	}
}

func TestRunMissingDirectory(t *testing.T) {
	code, out, errOut := runCmd("-dir", filepath.Join(t.TempDir(), "nao-existe"))
	if code != 2 || out != "" {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	if !strings.Contains(errOut, "pasta não encontrada") || !strings.Contains(errOut, "nao-existe") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestRunDirWithOnlyGaragem(t *testing.T) {
	dir := t.TempDir()
	b, err := os.ReadFile(filepath.Join(demoDir, "garagem.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "garagem.csv"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCmd("-dir", dir)
	if code != 2 || out != "" || !strings.Contains(errOut, "arquivo obrigatório ausente") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestRunDataErrorNamesFileLineAndColumn(t *testing.T) {
	dir := copyDemo(t, map[string]func(string) string{"onibus.csv": replace("B01,300,2026-03-04 20:00,30,", "B01,300,2026-03-04 20:00,abc,")})
	code, out, errOut := runCmd("-dir", dir)
	if code != 2 || out != "" {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	for _, want := range []string{"onibus.csv, linha 2, coluna soc_chegada_pct"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}
	if strings.Contains(errOut, "panic") || strings.Contains(errOut, dir) {
		t.Errorf("stderr leaks internals: %q", errOut)
	}
}

func TestRunJSON(t *testing.T) {
	code, out, errOut := runCmd("-dir", demoDir, "-json")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.HasPrefix(out, "{\n  \"") {
		t.Errorf("JSON must be indented: %.40q", out)
	}
	var rep realdata.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rep.Nights) != 2 || rep.Nights[0].Key != "2026-03-04" || len(rep.Aggregate) != len(realdata.ReplayNames) || len(rep.Assumptions) == 0 {
		t.Errorf("report = %d nights, %d aggregate rows, %d assumptions", len(rep.Nights), len(rep.Aggregate), len(rep.Assumptions))
	}
	if rep.Nights[0].Real.PeakKW == nil || !rep.Nights[0].Real.PeakEstimated || !rep.CostComparable {
		t.Errorf("night 1 real = %+v, comparable %v", rep.Nights[0].Real, rep.CostComparable)
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(out), &generic); err != nil {
		t.Fatal(err)
	}
	if len(generic["nights"].([]any)) != 2 {
		t.Errorf("generic nights = %v", generic["nights"])
	}
	// the text report is not mixed into the JSON
	if strings.Contains(out, "Premissas:") || strings.Contains(out, "aviso:") {
		t.Errorf("JSON mode printed text:\n%s", out)
	}
}

func TestRunJSONKeepsWarningsInsideTheReport(t *testing.T) {
	dir := copyDemo(t, map[string]func(string) string{"garagem.csv": replace("limite_kw,", "extra,limite_kw,")})
	// give every row of garagem.csv the extra cell
	b, _ := os.ReadFile(filepath.Join(dir, "garagem.csv"))
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = "x," + lines[i]
	}
	if err := os.WriteFile(filepath.Join(dir, "garagem.csv"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCmd("-dir", dir, "-json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var rep realdata.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON (warnings must not be printed outside it): %v\n%s", err, out)
	}
	found := false
	for _, w := range rep.Warnings {
		found = found || w.File == "garagem.csv"
	}
	if !found {
		t.Errorf("the unknown column warning is missing from the JSON: %+v", rep.Warnings)
	}
}

func TestRunWarningsComeFirst(t *testing.T) {
	dir := copyDemo(t, nil)
	b, _ := os.ReadFile(filepath.Join(dir, "garagem.csv"))
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	lines[0] += ",extra"
	for i := 1; i < len(lines); i++ {
		lines[i] += ",x"
	}
	if err := os.WriteFile(filepath.Join(dir, "garagem.csv"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCmd("-dir", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	w, g, p := strings.Index(out, "aviso:"), strings.Index(out, "aviso: garagem.csv"), strings.Index(out, "Premissas:")
	if w != 0 || g < 0 || p < g {
		t.Errorf("warnings must open the output (aviso at %d, garagem at %d, Premissas at %d):\n%s", w, g, p, out)
	}
}

func TestRunSoCNoiseChangesAssumptions(t *testing.T) {
	_, plain, _ := runCmd("-dir", demoDir)
	code, noisy, errOut := runCmd("-dir", demoDir, "-soc-noise", "3")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(plain, "perfeitas") || strings.Contains(plain, "ruído de") {
		t.Errorf("default assumptions:\n%s", plain)
	}
	if !strings.Contains(noisy, "ruído de 3 kWh") || strings.Contains(noisy, "perfeitas") {
		t.Errorf("noise assumption missing:\n%s", noisy)
	}
}

func TestRunSwapBackFlagsAreShown(t *testing.T) {
	_, def, _ := runCmd("-dir", demoDir)
	if !strings.Contains(def, "após 30 min") || !strings.Contains(def, "> 10 kWh") {
		t.Errorf("default swap-back not shown:\n%s", def)
	}
	code, out, errOut := runCmd("-dir", demoDir, "-swap-back-cooldown", "0", "-swap-back-min-need", "0")
	if code != 0 || !strings.Contains(out, "após 0 min") || !strings.Contains(out, "> 0 kWh") {
		t.Errorf("exit %d (%s), output:\n%s", code, errOut, out)
	}
}

func TestRunShowsDashForMissingData(t *testing.T) {
	dir := copyDemo(t, map[string]func(string) string{"sessoes.csv": nil, "potencia.csv": nil})
	code, out, errOut := runCmd("-dir", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	tbs := tables(out)
	hdr := tbs[0][0]
	for i, tb := range tbs {
		real := tb[1]
		for _, c := range []string{"peak kW", "energy kWh", "cost R$"} {
			if real[col(t, hdr, c)] != "—" {
				t.Errorf("table %d: real %s = %q, want —", i, c, real[col(t, hdr, c)])
			}
		}
	}
	if strings.Contains(out, "(estimado)") {
		t.Errorf("nothing is estimated without sessions or power:\n%s", out)
	}
	// the simulated rows keep their numbers
	if tbs[0][2][col(t, hdr, "cost R$")] == "—" {
		t.Errorf("the tariff exists: simulated cost must be shown")
	}
}

func TestRunPartialRealOutcomeIsExplained(t *testing.T) {
	// night 2 has B03 without soc_saida_real_pct
	_, out, _ := runCmd("-dir", demoDir)
	if !strings.Contains(out, "só os 2 de 3 ônibus com soc_saida_real_pct") {
		t.Errorf("partial outcome note missing:\n%s", out)
	}
}

func TestRunNoRealOutcomeIsNotReportedAsAllReady(t *testing.T) {
	// without soc_saida_real_pct nobody "was ready": the outcome is unknown
	dir := copyDemo(t, map[string]func(string) string{
		"sessoes.csv": nil, "potencia.csv": nil,
		"onibus.csv": func(string) string {
			return "onibus_id,capacidade_kwh,chegada,soc_chegada_pct,saida_prevista,soc_saida_exigido_pct,potencia_max_bateria_kw\n" +
				"B01,300,2026-03-04 20:00,30,2026-03-05 05:00,90,150\n" +
				"B02,300,2026-03-04 22:00,40,2026-03-05 06:00,85,150\n"
		}})
	code, out, errOut := runCmd("-dir", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "Resultado real desconhecido") {
		t.Errorf("unknown real outcome not said:\n%s", out)
	}
	if strings.Contains(out, "Nenhum dos") {
		t.Errorf("must not claim that no bus was late:\n%s", out)
	}
	if !strings.Contains(out, "Agregado (1 noite;") {
		t.Errorf("singular aggregate title missing:\n%s", out)
	}
	// with outcomes the message names how many buses it is about
	_, out2, _ := runCmd("-dir", copyDemo(t, map[string]func(string) string{"onibus.csv": func(s string) string {
		s = strings.Replace(s, ",70,2026-03-05 06:00", ",85,2026-03-05 06:00", 1) // B02 of night 1 now ready
		return strings.Replace(s, ",88,2026-03-06 05:00", ",90,2026-03-06 05:00", 1)
	}}))
	for _, want := range []string{
		"Nenhum dos 3 ônibus com resultado real ficou sem a carga exigida.",
		"Nenhum dos 2 ônibus com resultado real ficou sem a carga exigida.", // night 2: B03 has no outcome
	} {
		if !strings.Contains(out2, want) {
			t.Errorf("all-ready message %q missing:\n%s", want, out2)
		}
	}
}

func TestRunCostNotComparable(t *testing.T) {
	for name, edit := range map[string]func(string) string{
		"window crossing midnight": replace("18:00,21:00", "22:00,06:00"),
		"no tariff":                func(string) string { return "limite_kw\n400\n" },
	} {
		dir := copyDemo(t, map[string]func(string) string{"garagem.csv": edit})
		code, out, errOut := runCmd("-dir", dir)
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", name, code, errOut)
		}
		if !strings.Contains(out, "não é comparável") {
			t.Errorf("%s: note missing:\n%s", name, out)
		}
		tbs := tables(out)
		hdr := tbs[0][0]
		for i, tb := range tbs {
			for _, row := range tb[2:] {
				if row[col(t, hdr, "cost R$")] != "—" {
					t.Errorf("%s: table %d, %s cost = %q, want —", name, i, row[0], row[col(t, hdr, "cost R$")])
				}
			}
		}
		if name == "window crossing midnight" { // the real cost stays visible
			if c := tbs[1][1][col(t, hdr, "cost R$")]; c == "—" {
				t.Errorf("real cost must stay visible with a midnight window: %v", tbs[1][1])
			}
		}
	}
	// and the demo, comparable, has no such note
	if _, out, _ := runCmd("-dir", demoDir); strings.Contains(out, "não é comparável") {
		t.Errorf("comparable demo shows the note")
	}
}

func TestRunNoNightsToReplay(t *testing.T) {
	long := "onibus_id,capacidade_kwh,chegada,soc_chegada_pct,saida_prevista,soc_saida_exigido_pct,potencia_max_bateria_kw,soc_saida_real_pct,saida_real\n" +
		"B01,300,2026-03-04 12:30,30,2026-03-05 05:00,90,150,,\n" +
		"B02,300,2026-03-04 22:00,40,2026-03-06 12:31,85,150,,\n"
	dir := copyDemo(t, map[string]func(string) string{
		"onibus.csv":  func(string) string { return long },
		"sessoes.csv": nil, "potencia.csv": nil,
	})
	code, out, errOut := runCmd("-dir", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "aviso:") || !strings.Contains(out, "Nenhuma noite") || len(tables(out)) != 0 {
		t.Errorf("output:\n%s", out)
	}
	code, out, _ = runCmd("-dir", dir, "-json")
	if code != 0 || !strings.Contains(out, `"nights": []`) {
		t.Errorf("json exit %d:\n%s", code, out)
	}
}
