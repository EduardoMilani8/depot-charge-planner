package realdata

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func demoFiles(t *testing.T) map[string][]byte {
	t.Helper()
	m := map[string][]byte{}
	for _, n := range FileNames {
		b, err := os.ReadFile(filepath.Join("testdata", "demo", n))
		if err != nil {
			t.Fatal(err)
		}
		m[n] = b
	}
	return m
}

// mutate returns a copy of m with old replaced by new in file (old must occur).
func mutate(t *testing.T, m map[string][]byte, file, old, new string) map[string][]byte {
	t.Helper()
	if !strings.Contains(string(m[file]), old) {
		t.Fatalf("fixture %s does not contain %q", file, old)
	}
	out := map[string][]byte{}
	for k, v := range m {
		out[k] = v
	}
	out[file] = []byte(strings.Replace(string(m[file]), old, new, 1))
	return out
}

func dataOnly(d *Dataset) Dataset {
	c := *d
	c.Warnings = nil
	return c
}

func TestLoadDirDemo(t *testing.T) {
	d, err := LoadDir(filepath.Join("testdata", "demo"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(d.Chargers) != 3 || len(d.Buses) != 6 || len(d.Sessions) != 5 || len(d.Power) != 6 {
		t.Fatalf("counts: %d chargers, %d buses, %d sessions, %d samples", len(d.Chargers), len(d.Buses), len(d.Sessions), len(d.Power))
	}
	if !d.HasSessions || !d.HasPower {
		t.Errorf("HasSessions=%v HasPower=%v", d.HasSessions, d.HasPower)
	}
	g := d.Garage
	if !g.HasTariff || g.PeakFromMin != 1080 || g.PeakToMin != 1260 || g.LimitKW != 400 || g.PeakPrice != 2.70 || g.OffPeakPrice != 0.90 {
		t.Errorf("garage = %+v", g)
	}
	if c := d.Chargers[0]; c.ID != "C01" || c.MaxKW != 150 || c.MinKW != 5 || c.Efficiency != 0.94 {
		t.Errorf("charger = %+v", c)
	}
	b := d.Buses[5]
	if b.ID != "B03" || b.RealSoCPct != nil || b.RealDeparture != nil || b.Line != 7 {
		t.Errorf("last bus = %+v", b)
	}
	b = d.Buses[0]
	if b.RealSoCPct == nil || *b.RealSoCPct != 91 || b.RealDeparture == nil || !b.RealDeparture.Equal(time.Date(2026, 3, 5, 5, 2, 0, 0, time.UTC)) {
		t.Errorf("first bus real = %+v", b)
	}
	if b.MaxBatteryKW != 150 || b.CapacityKWh != 300 || b.SoCArrivalPct != 30 || b.RequiredPct != 90 {
		t.Errorf("first bus = %+v", b)
	}
	if s := d.Sessions[3]; s.Line != 5 || s.BusID != "B01" || s.ChargerID != "C01" || s.EnergyKWh != 50 {
		t.Errorf("session = %+v", s)
	}
	if p := d.Power[3]; p.ChargerID != "C02" || p.KW != 40 || !p.At.Equal(time.Date(2026, 3, 5, 22, 15, 0, 0, time.UTC)) {
		t.Errorf("sample = %+v", p)
	}
	for _, w := range d.Warnings {
		if strings.Contains(w.Message, "padrões usados") {
			t.Errorf("demo should use no defaults, got warning %v", w)
		}
	}
}

func TestNightKey(t *testing.T) {
	cases := map[string]string{
		"2026-03-06 00:20": "2026-03-05",
		"2026-03-04 20:00": "2026-03-04",
		"2026-03-04 12:00": "2026-03-04",
		"2026-03-04 11:59": "2026-03-03",
		"2026-01-01 03:00": "2025-12-31",
	}
	for in, want := range cases {
		tm, err := time.ParseInLocation("2006-01-02 15:04", in, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		if got := NightKey(tm); got != want {
			t.Errorf("NightKey(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestLoadBrazilianExcelEquivalent(t *testing.T) {
	plain, err := Load(demoFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	br := map[string][]byte{}
	for n, b := range demoFiles(t) {
		s := strings.ReplaceAll(string(b), ",", ";")
		s = strings.ReplaceAll(s, ".", ",")
		s = strings.ReplaceAll(s, "\n", "\r\n") + "\r\n\r\n"
		br[n] = append([]byte("\xEF\xBB\xBF"), s...)
	}
	got, err := Load(br)
	if err != nil {
		t.Fatalf("Load (BR): %v", err)
	}
	a, b := dataOnly(plain), dataOnly(got)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("datasets differ:\n%+v\n%+v", a, b)
	}
}

func TestLoadDatesDDMMYYYY(t *testing.T) {
	m := mutate(t, demoFiles(t), "onibus.csv", "2026-03-04 20:00,30", "04/03/2026 20:00,30")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Buses[0].Arrival.Equal(time.Date(2026, 3, 4, 20, 0, 0, 0, time.UTC)) {
		t.Errorf("arrival = %v", d.Buses[0].Arrival)
	}
}

func TestLoadErrors(t *testing.T) {
	demo := demoFiles(t)
	type tc struct {
		name              string
		file, old, new    string
		wantFile, wantCol string
		wantLine          int
		wantMsg           string
	}
	cases := []tc{
		// garagem.csv
		{"limite zero", "garagem.csv", "400,18:00", "0,18:00", "garagem.csv", "limite_kw", 2, ""},
		{"limite vazio", "garagem.csv", "400,18:00", ",18:00", "garagem.csv", "limite_kw", 2, "obrigatório"},
		{"limite enorme", "garagem.csv", "400,18:00", "1e9,18:00", "garagem.csv", "limite_kw", 2, ""},
		{"limite texto", "garagem.csv", "400,18:00", "muito,18:00", "garagem.csv", "limite_kw", 2, ""},
		{"ponta invalida", "garagem.csv", "18:00,21:00", "25:00,21:00", "garagem.csv", "ponta_inicio", 2, "HH:MM"},
		{"ponta fim invalida", "garagem.csv", "18:00,21:00", "18:00,21:75", "garagem.csv", "ponta_fim", 2, "HH:MM"},
		{"preco negativo", "garagem.csv", "2.70,0.90", "-2.70,0.90", "garagem.csv", "preco_ponta", 2, ""},
		{"preco fora negativo", "garagem.csv", "2.70,0.90", "2.70,-1", "garagem.csv", "preco_fora_ponta", 2, ""},
		{"coluna limite ausente", "garagem.csv", "limite_kw", "limite", "garagem.csv", "limite_kw", 1, "coluna obrigatória ausente"},
		// carregadores.csv
		{"carregador duplicado", "carregadores.csv", "C02,", "C01,", "carregadores.csv", "carregador_id", 3, "duplicado"},
		{"carregador sem id", "carregadores.csv", "C03,", ",", "carregadores.csv", "carregador_id", 4, "obrigatório"},
		{"potencia max zero", "carregadores.csv", "C01,150", "C01,0", "carregadores.csv", "potencia_max_kw", 2, ""},
		{"potencia max vazia", "carregadores.csv", "C01,150", "C01,", "carregadores.csv", "potencia_max_kw", 2, "obrigatório"},
		{"potencia max enorme", "carregadores.csv", "C01,150", "C01,5001", "carregadores.csv", "potencia_max_kw", 2, ""},
		{"potencia min negativa", "carregadores.csv", "C01,150,5", "C01,150,-1", "carregadores.csv", "potencia_min_kw", 2, ""},
		{"potencia min acima da max", "carregadores.csv", "C01,150,5", "C01,150,200", "carregadores.csv", "potencia_min_kw", 2, ""},
		{"eficiencia acima de 1", "carregadores.csv", "C01,150,5,0.94", "C01,150,5,1.5", "carregadores.csv", "eficiencia", 2, ""},
		{"eficiencia zero", "carregadores.csv", "C01,150,5,0.94", "C01,150,5,0", "carregadores.csv", "eficiencia", 2, ""},
		{"coluna potencia_max ausente", "carregadores.csv", "potencia_max_kw", "pmax", "carregadores.csv", "potencia_max_kw", 1, "coluna obrigatória ausente"},
		// onibus.csv
		{"soc chegada 120", "onibus.csv", "2026-03-04 23:30,20,", "2026-03-04 23:30,120,", "onibus.csv", "soc_chegada_pct", 4, ""},
		{"soc chegada negativo", "onibus.csv", "2026-03-04 23:30,20,", "2026-03-04 23:30,-1,", "onibus.csv", "soc_chegada_pct", 4, ""},
		{"soc chegada vazio", "onibus.csv", "2026-03-04 23:30,20,", "2026-03-04 23:30,,", "onibus.csv", "soc_chegada_pct", 4, "obrigatório"},
		{"soc exigido zero", "onibus.csv", "05:30,90,150", "05:30,0,150", "onibus.csv", "soc_saida_exigido_pct", 4, ""},
		{"soc exigido acima de 100", "onibus.csv", "05:30,90,150", "05:30,101,150", "onibus.csv", "soc_saida_exigido_pct", 4, ""},
		{"capacidade zero", "onibus.csv", "B02,300,2026-03-04 22:00", "B02,0,2026-03-04 22:00", "onibus.csv", "capacidade_kwh", 3, ""},
		{"capacidade enorme", "onibus.csv", "B02,300,2026-03-04 22:00", "B02,2001,2026-03-04 22:00", "onibus.csv", "capacidade_kwh", 3, ""},
		{"saida antes da chegada", "onibus.csv", "2026-03-05 05:30,90", "2026-03-04 22:30,90", "onibus.csv", "saida_prevista", 4, "depois da chegada"},
		{"saida igual a chegada", "onibus.csv", "2026-03-05 05:30,90", "2026-03-04 23:30,90", "onibus.csv", "saida_prevista", 4, "depois da chegada"},
		{"chegada invalida", "onibus.csv", "2026-03-04 22:00", "ontem de noite", "onibus.csv", "chegada", 3, ""},
		{"onibus sem id", "onibus.csv", "B02,300,2026-03-04 22:00", ",300,2026-03-04 22:00", "onibus.csv", "onibus_id", 3, "obrigatório"},
		{"onibus duplicado na noite", "onibus.csv", "B02,300,2026-03-05 22:00", "B01,300,2026-03-05 22:00", "onibus.csv", "onibus_id", 6, "duplicado"},
		{"potencia bateria zero", "onibus.csv", "05:30,90,150,90", "05:30,90,0,90", "onibus.csv", "potencia_max_bateria_kw", 4, ""},
		{"soc real 101", "onibus.csv", "150,90,2026-03-05 05:30", "150,101,2026-03-05 05:30", "onibus.csv", "soc_saida_real_pct", 4, ""},
		{"saida real invalida", "onibus.csv", "2026-03-05 05:30\nB01", "amanha\nB01", "onibus.csv", "saida_real", 4, ""},
		{"saida real antes da chegada", "onibus.csv", "2026-03-05 05:30\nB01", "2026-03-04 20:00\nB01", "onibus.csv", "saida_real", 4, ""},
		{"coluna chegada ausente", "onibus.csv", ",chegada,", ",hora_chegada,", "onibus.csv", "chegada", 1, "coluna obrigatória ausente"},
		{"coluna soc_chegada ausente", "onibus.csv", "soc_chegada_pct", "soc_chegada", "onibus.csv", "soc_chegada_pct", 1, "coluna obrigatória ausente"},
		// sessoes.csv
		{"sessao de onibus inexistente", "sessoes.csv", "B03,C03", "B99,C03", "sessoes.csv", "onibus_id", 4, ""},
		{"sessao em noite sem o onibus", "sessoes.csv", "B01,C01,2026-03-05 22:00,2026-03-05 22:30", "B01,C01,2026-03-10 22:00,2026-03-10 22:30", "sessoes.csv", "onibus_id", 5, "noite"},
		{"sessao de carregador inexistente", "sessoes.csv", "B03,C03", "B03,C09", "sessoes.csv", "carregador_id", 4, ""},
		{"sessao fim antes do inicio", "sessoes.csv", "2026-03-05 02:00", "2026-03-04 21:00", "sessoes.csv", "fim", 3, ""},
		{"sessao fim igual ao inicio", "sessoes.csv", "2026-03-05 02:00", "2026-03-04 22:00", "sessoes.csv", "fim", 3, ""},
		{"sessao energia negativa", "sessoes.csv", ",120", ",-120", "sessoes.csv", "energia_kwh", 3, ""},
		{"sessao energia vazia", "sessoes.csv", ",120", ",", "sessoes.csv", "energia_kwh", 3, "obrigatório"},
		{"sessao inicio invalido", "sessoes.csv", "2026-03-04 22:00", "22h", "sessoes.csv", "inicio", 3, ""},
		{"sessoes sem coluna", "sessoes.csv", "energia_kwh", "kwh", "sessoes.csv", "energia_kwh", 1, "coluna obrigatória ausente"},
		// potencia.csv
		{"potencia carregador desconhecido", "potencia.csv", "22:15,C02", "22:15,C77", "potencia.csv", "carregador_id", 5, ""},
		{"potencia negativa", "potencia.csv", "22:00,C02,20", "22:00,C02,-20", "potencia.csv", "potencia_kw", 3, ""},
		{"potencia instante invalido", "potencia.csv", "2026-03-05 22:15,C01", "xx,C01", "potencia.csv", "instante", 4, ""},
		{"potencia sem coluna", "potencia.csv", "potencia_kw", "kw", "potencia.csv", "potencia_kw", 1, "coluna obrigatória ausente"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(mutate(t, demo, c.file, c.old, c.new))
			var fe *FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("want *FieldError, got %v", err)
			}
			if fe.File != c.wantFile || fe.Line != c.wantLine || fe.Column != c.wantCol {
				t.Errorf("got %s/%d/%s (%q), want %s/%d/%s", fe.File, fe.Line, fe.Column, fe.Message, c.wantFile, c.wantLine, c.wantCol)
			}
			if c.wantMsg != "" && !strings.Contains(fe.Message, c.wantMsg) {
				t.Errorf("message %q does not contain %q", fe.Message, c.wantMsg)
			}
			if fe.Message == "" {
				t.Errorf("empty message")
			}
		})
	}
}

func TestLoadEmptyDataFiles(t *testing.T) {
	demo := demoFiles(t)
	for _, f := range []string{FileGaragem, FileCarregadores, FileOnibus} {
		m := map[string][]byte{}
		for k, v := range demo {
			m[k] = v
		}
		header := strings.SplitN(string(demo[f]), "\n", 2)[0] + "\n"
		m[f] = []byte(header)
		_, err := Load(m)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.File != f {
			t.Errorf("%s with header only: err = %v", f, err)
		}
		m[f] = nil
		if _, err := Load(m); !errors.As(err, &fe) || fe.File != f {
			t.Errorf("%s empty: err = %v", f, err)
		}
	}
}

func TestLoadMissingRequiredFile(t *testing.T) {
	for _, f := range []string{FileGaragem, FileCarregadores, FileOnibus} {
		m := demoFiles(t)
		delete(m, f)
		_, err := Load(m)
		var fe *FieldError
		if !errors.As(err, &fe) {
			t.Fatalf("%s: want *FieldError, got %v", f, err)
		}
		if fe.File != f || fe.Message != "arquivo obrigatório ausente" || fe.Line != 0 || fe.Column != "" {
			t.Errorf("%s: got %+v", f, fe)
		}
	}
	if _, err := Load(nil); err == nil {
		t.Error("Load(nil) should fail")
	}
}

func TestLoadUnknownFileWarns(t *testing.T) {
	m := demoFiles(t)
	m["extra.csv"] = []byte("x\n1\n")
	m["../etc/passwd"] = []byte("x")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range d.Warnings {
		if w.File == "extra.csv" || w.File == "../etc/passwd" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("want 2 warnings for unknown files, got %d: %v", n, d.Warnings)
	}
}

func TestLoadOptionalFilesAbsent(t *testing.T) {
	m := demoFiles(t)
	delete(m, FileSessoes)
	delete(m, FilePotencia)
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	if d.HasSessions || d.HasPower || d.Sessions != nil || d.Power != nil {
		t.Errorf("HasSessions=%v HasPower=%v Sessions=%v Power=%v", d.HasSessions, d.HasPower, d.Sessions, d.Power)
	}
	// header-only optional files count as absent
	m[FileSessoes] = []byte("onibus_id,carregador_id,inicio,fim,energia_kwh\n")
	d, err = Load(m)
	if err != nil || d.HasSessions {
		t.Errorf("header-only sessoes: err=%v HasSessions=%v", err, d.HasSessions)
	}
}

func TestLoadNoTariff(t *testing.T) {
	cases := map[string]string{
		"only limit":    "limite_kw\n400\n",
		"missing price": "limite_kw,ponta_inicio,ponta_fim,preco_ponta,preco_fora_ponta\n400,18:00,21:00,2.70,\n",
		"missing time":  "limite_kw,ponta_inicio,ponta_fim,preco_ponta,preco_fora_ponta\n400,,21:00,2.70,0.90\n",
	}
	for name, csv := range cases {
		m := demoFiles(t)
		m[FileGaragem] = []byte(csv)
		d, err := Load(m)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		g := d.Garage
		if g.HasTariff || g.PeakFromMin != 0 || g.PeakToMin != 0 || g.PeakPrice != 0 || g.OffPeakPrice != 0 || g.LimitKW != 400 {
			t.Errorf("%s: garage = %+v", name, g)
		}
		found := false
		for _, w := range d.Warnings {
			if w.File == FileGaragem && strings.Contains(w.Message, "tarifa incompleta: custo não será calculado") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: missing tariff warning: %v", name, d.Warnings)
		}
	}
}

func TestLoadGarageExtraRows(t *testing.T) {
	m := mutate(t, demoFiles(t), FileGaragem, "400,18:00,21:00,2.70,0.90\n", "400,18:00,21:00,2.70,0.90\n999,,,,\n")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	if d.Garage.LimitKW != 400 {
		t.Errorf("limit = %v", d.Garage.LimitKW)
	}
	found := false
	for _, w := range d.Warnings {
		if w.File == FileGaragem && strings.Contains(w.Message, "primeira") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning about extra rows: %v", d.Warnings)
	}
}

func TestLoadPeakWindowCrossingMidnight(t *testing.T) {
	m := mutate(t, demoFiles(t), FileGaragem, "18:00,21:00", "22:30,6:15")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	if d.Garage.PeakFromMin != 22*60+30 || d.Garage.PeakToMin != 6*60+15 {
		t.Errorf("garage = %+v", d.Garage)
	}
}

func countWarnings(d *Dataset, file, sub string) int {
	n := 0
	for _, w := range d.Warnings {
		if w.File == file && strings.Contains(w.Message, sub) {
			n++
		}
	}
	return n
}

func TestLoadDefaultsGroupedPerFile(t *testing.T) {
	m := demoFiles(t)
	m[FileCarregadores] = []byte("carregador_id,potencia_max_kw\nC01,150\nC02,150\nC03,100\n")
	m[FileOnibus] = []byte(strings.Join([]string{
		"onibus_id,capacidade_kwh,chegada,soc_chegada_pct,saida_prevista,soc_saida_exigido_pct",
		"B01,300,2026-03-04 20:00,30,2026-03-05 05:00,90",
		"B02,300,2026-03-04 22:00,40,2026-03-05 06:00,85",
		"B03,300,2026-03-04 23:30,20,2026-03-05 05:30,90",
	}, "\n") + "\n")
	m[FileSessoes] = []byte("onibus_id,carregador_id,inicio,fim,energia_kwh\nB01,C01,2026-03-04 20:00,2026-03-05 00:00,200\n")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	if n := countWarnings(d, FileCarregadores, "padrões usados"); n != 1 {
		t.Errorf("carregadores.csv defaults warnings = %d, want 1: %v", n, d.Warnings)
	}
	if n := countWarnings(d, FileOnibus, "padrões usados"); n != 1 {
		t.Errorf("onibus.csv defaults warnings = %d, want 1: %v", n, d.Warnings)
	}
	for _, c := range d.Chargers {
		if c.MinKW != 5 || c.Efficiency != 0.94 {
			t.Errorf("charger defaults = %+v", c)
		}
	}
	for _, b := range d.Buses {
		if b.MaxBatteryKW != 0 || b.RealSoCPct != nil || b.RealDeparture != nil {
			t.Errorf("bus = %+v", b)
		}
	}
	if n := countWarnings(d, FileOnibus, "soc_saida_real_pct"); n != 1 {
		t.Errorf("soc_saida_real_pct warnings = %d, want 1: %v", n, d.Warnings)
	}
}

func TestLoadUnknownColumnWarnsOnce(t *testing.T) {
	m := mutate(t, demoFiles(t), FileOnibus, "onibus_id,", "onibus_id,placa,")
	lines := strings.Split(string(m[FileOnibus]), "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = strings.Replace(lines[i], ",", ",XYZ,", 1)
		}
	}
	m[FileOnibus] = []byte(strings.Join(lines, "\n"))
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	if n := countWarnings(d, FileOnibus, "placa"); n != 1 {
		t.Errorf("unknown column warnings = %d: %v", n, d.Warnings)
	}
}

func TestLoadUnsortedPowerSortedWithWarning(t *testing.T) {
	m := demoFiles(t)
	m[FilePotencia] = []byte(strings.Join([]string{
		"instante,carregador_id,potencia_kw",
		"2026-03-05 22:30,C01,0",
		"2026-03-05 22:00,C01,100",
		"2026-03-05 22:15,C01,100",
		"2026-03-05 22:00,C02,20",
	}, "\n") + "\n")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	last := map[string]time.Time{}
	for _, p := range d.Power {
		if p.At.Before(last[p.ChargerID]) {
			t.Errorf("power samples of %s not sorted: %v", p.ChargerID, d.Power)
		}
		last[p.ChargerID] = p.At
	}
	if len(d.Power) != 4 {
		t.Errorf("samples = %d", len(d.Power))
	}
	if n := countWarnings(d, FilePotencia, "ordem"); n != 1 {
		t.Errorf("order warnings = %d: %v", n, d.Warnings)
	}
	// the sorted demo itself gives no such warning
	d, _ = Load(demoFiles(t))
	if n := countWarnings(d, FilePotencia, "ordem"); n != 0 {
		t.Errorf("sorted power warned: %v", d.Warnings)
	}
}

func TestLoadPowerAboveTwiceMaxWarns(t *testing.T) {
	m := mutate(t, demoFiles(t), FilePotencia, "22:15,C01,100", "22:15,C01,301")
	m = mutate(t, m, FilePotencia, "22:00,C01,100", "22:00,C01,400")
	d, err := Load(m)
	if err != nil {
		t.Fatalf("above 2x must be a warning, got %v", err)
	}
	if n := countWarnings(d, FilePotencia, "2×"); n != 1 {
		t.Errorf("want one grouped warning, got %d: %v", n, d.Warnings)
	}
	// exactly 2x is fine
	m = mutate(t, demoFiles(t), FilePotencia, "22:15,C01,100", "22:15,C01,300")
	d, err = Load(m)
	if err != nil || countWarnings(d, FilePotencia, "2×") != 0 {
		t.Errorf("300 kW on a 150 kW charger: err=%v warnings=%v", err, d.Warnings)
	}
}

func busesCSV(rows int, nightOf func(i int) int) []byte {
	var sb strings.Builder
	sb.WriteString("onibus_id,capacidade_kwh,chegada,soc_chegada_pct,saida_prevista,soc_saida_exigido_pct\n")
	base := time.Date(2025, 1, 1, 20, 0, 0, 0, time.UTC)
	for i := 0; i < rows; i++ {
		a := base.AddDate(0, 0, nightOf(i))
		fmt.Fprintf(&sb, "B%d,300,%s,30,%s,90\n", i, a.Format("2006-01-02 15:04"), a.Add(8*time.Hour).Format("2006-01-02 15:04"))
	}
	return []byte(sb.String())
}

func TestLoadLimits(t *testing.T) {
	var fe *FieldError
	only := func(onibus []byte) map[string][]byte {
		m := demoFiles(t)
		delete(m, FileSessoes)
		delete(m, FilePotencia)
		m[FileOnibus] = onibus
		return m
	}
	// 1000 buses in one night is fine, 1001 is not
	if _, err := Load(only(busesCSV(1000, func(int) int { return 0 }))); err != nil {
		t.Errorf("1000 buses: %v", err)
	}
	_, err := Load(only(busesCSV(1001, func(int) int { return 0 })))
	if !errors.As(err, &fe) || fe.File != FileOnibus || fe.Line != 1002 || !strings.Contains(fe.Message, "1000") {
		t.Errorf("1001 buses: %v", err)
	}
	// 366 nights fine, 367 not
	if _, err := Load(only(busesCSV(366, func(i int) int { return i }))); err != nil {
		t.Errorf("366 nights: %v", err)
	}
	_, err = Load(only(busesCSV(367, func(i int) int { return i })))
	if !errors.As(err, &fe) || fe.File != FileOnibus || fe.Line != 368 || !strings.Contains(fe.Message, "366") {
		t.Errorf("367 nights: %v", err)
	}
	// total buses above sim.MaxBuses (10000): 11 nights x 1000
	_, err = Load(only(busesCSV(11000, func(i int) int { return i / 1000 })))
	if !errors.As(err, &fe) || fe.File != FileOnibus || !strings.Contains(fe.Message, "10000") {
		t.Errorf("11000 buses: %v", err)
	}
	// per-file row cap: 1,000,000 data rows
	var sb strings.Builder
	sb.WriteString("limite_kw\n")
	for i := 0; i < 1_000_001; i++ {
		sb.WriteString("1\n")
	}
	m := demoFiles(t)
	m[FileGaragem] = []byte(sb.String())
	_, err = Load(m)
	if !errors.As(err, &fe) || fe.File != FileGaragem || !strings.Contains(fe.Message, "1.000.000") {
		t.Errorf("row cap: %v", err)
	}
	// chargers above sim.MaxChargers
	sb.Reset()
	sb.WriteString("carregador_id,potencia_max_kw\n")
	for i := 0; i < 10001; i++ {
		fmt.Fprintf(&sb, "C%d,50\n", i)
	}
	m = demoFiles(t)
	m[FileCarregadores] = []byte(sb.String())
	_, err = Load(m)
	if !errors.As(err, &fe) || fe.File != FileCarregadores || !strings.Contains(fe.Message, "10000") {
		t.Errorf("10001 chargers: %v", err)
	}
}

func TestLoadDirReadsOnlyFixedNames(t *testing.T) {
	dir := t.TempDir()
	for n, b := range demoFiles(t) {
		if err := os.WriteFile(filepath.Join(dir, n), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.csv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if countWarnings(d, "extra.csv", "") != 0 {
		t.Errorf("LoadDir must not read other files: %v", d.Warnings)
	}
	// optional file absent from the directory
	if err := os.Remove(filepath.Join(dir, FilePotencia)); err != nil {
		t.Fatal(err)
	}
	d, err = LoadDir(dir)
	if err != nil || d.HasPower {
		t.Errorf("without potencia.csv: err=%v HasPower=%v", err, d.HasPower)
	}
	// required file absent
	if err := os.Remove(filepath.Join(dir, FileOnibus)); err != nil {
		t.Fatal(err)
	}
	var fe *FieldError
	if _, err = LoadDir(dir); !errors.As(err, &fe) || fe.File != FileOnibus || fe.Message != "arquivo obrigatório ausente" {
		t.Errorf("without onibus.csv: %v", err)
	}
	// file that is a directory
	if err := os.Mkdir(filepath.Join(dir, FileOnibus), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadDir(dir); !errors.As(err, &fe) || fe.File != FileOnibus {
		t.Errorf("onibus.csv as directory: %v", err)
	}
	// nonexistent directory
	if _, err = LoadDir(filepath.Join(dir, "nao-existe")); !errors.As(err, &fe) {
		t.Errorf("missing dir: %v", err)
	}
}

func TestLoadNeverPanicsOnRandomBytes(t *testing.T) {
	rng := rand.New(rand.NewSource(20261008))
	demo := demoFiles(t)
	randBytes := func() []byte {
		b := make([]byte, rng.Intn(200))
		for i := range b {
			switch rng.Intn(4) {
			case 0:
				b[i] = byte(rng.Intn(256))
			case 1:
				b[i] = ",;\n\r\"\xEF\xBB\xBF"[rng.Intn(8)]
			default:
				b[i] = "0123456789:-. abcCB"[rng.Intn(19)]
			}
		}
		return b
	}
	for i := 0; i < 200; i++ {
		m := map[string][]byte{}
		for _, n := range FileNames {
			switch rng.Intn(3) {
			case 0:
				m[n] = randBytes()
			case 1:
				// valid file with a few corrupted bytes
				b := append([]byte(nil), demo[n]...)
				for k := rng.Intn(6); k >= 0 && len(b) > 0; k-- {
					b[rng.Intn(len(b))] = randBytes0(rng)
				}
				m[n] = b
			default:
				if rng.Intn(2) == 0 {
					m[n] = demo[n]
				}
			}
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("iteration %d panicked: %v\nfiles: %q", i, r, m)
				}
			}()
			d, err := Load(m)
			if err != nil {
				var fe *FieldError
				if !errors.As(err, &fe) {
					t.Fatalf("iteration %d: error is %T, want *FieldError", i, err)
				}
			} else if d == nil {
				t.Fatalf("iteration %d: nil dataset without error", i)
			}
		}()
	}
}

func randBytes0(rng *rand.Rand) byte {
	return byte(rng.Intn(256))
}
