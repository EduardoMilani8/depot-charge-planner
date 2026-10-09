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
		{"onibus duplicado na noite", "onibus.csv", "B02,300,2026-03-05 22:00", "B01,300,2026-03-05 22:00", "onibus.csv", "onibus_id", 6, "já aparece na linha 5"},
		{"potencia bateria zero", "onibus.csv", "05:30,90,150,90", "05:30,90,0,90", "onibus.csv", "potencia_max_bateria_kw", 4, ""},
		{"soc real 101", "onibus.csv", "150,90,2026-03-05 05:30", "150,101,2026-03-05 05:30", "onibus.csv", "soc_saida_real_pct", 4, ""},
		{"saida real invalida", "onibus.csv", "2026-03-05 05:30\nB01", "amanha\nB01", "onibus.csv", "saida_real", 4, ""},
		{"saida real antes da chegada", "onibus.csv", "2026-03-05 05:30\nB01", "2026-03-04 20:00\nB01", "onibus.csv", "saida_real", 4, ""},
		{"coluna chegada ausente", "onibus.csv", ",chegada,", ",hora_chegada,", "onibus.csv", "chegada", 1, "coluna obrigatória ausente"},
		{"coluna soc_chegada ausente", "onibus.csv", "soc_chegada_pct", "soc_chegada", "onibus.csv", "soc_chegada_pct", 1, "coluna obrigatória ausente"},
		// sessoes.csv
		{"sessao de onibus inexistente", "sessoes.csv", "B03,C03", "B99,C03", "sessoes.csv", "onibus_id", 4, "não existe em onibus.csv"},
		{"sessao em noite sem o onibus", "sessoes.csv", "B01,C01,2026-03-05 22:00,2026-03-05 22:30", "B01,C01,2026-03-10 22:00,2026-03-10 22:30", "sessoes.csv", "onibus_id", 5, "noites desse ônibus: 2026-03-04, 2026-03-05"},
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
					b[rng.Intn(len(b))] = byte(rng.Intn(256))
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

func wantFieldErr(t *testing.T, err error, file string, line int, col string) {
	t.Helper()
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("want *FieldError, got %v", err)
	}
	if fe.File != file || fe.Line != line || fe.Column != col || fe.Message == "" {
		t.Errorf("got %s/%d/%s (%q), want %s/%d/%s", fe.File, fe.Line, fe.Column, fe.Message, file, line, col)
	}
}

// Caps on magnitudes: an accepted value at the cap, an error one step above.
func TestLoadMagnitudeCaps(t *testing.T) {
	demo := demoFiles(t)
	type tc struct {
		name, file, old, ok, bad, col string
		line                          int
	}
	cases := []tc{
		{"energia por sessao", FileSessoes, "2026-03-05 00:00,200", "2026-03-05 00:00,1000000", "2026-03-05 00:00,1000001", "energia_kwh", 2},
		{"energia absurda", FileSessoes, "2026-03-05 00:00,200", "2026-03-05 00:00,1000000", "2026-03-05 00:00,1e308", "energia_kwh", 2},
		{"potencia da leitura", FilePotencia, "22:00,C01,100", "22:00,C01,100000", "22:00,C01,100001", "potencia_kw", 2},
		{"potencia absurda", FilePotencia, "22:00,C01,100", "22:00,C01,100000", "22:00,C01,1e308", "potencia_kw", 2},
		{"permanencia prevista", FileOnibus, "2026-03-05 05:00,90", "2026-03-06 20:00,90", "2026-03-06 20:01,90", "saida_prevista", 2},
		{"ano errado na saida prevista", FileOnibus, "2026-03-05 05:00,90", "2026-03-06 20:00,90", "9999-03-05 05:00,90", "saida_prevista", 2},
		{"permanencia real", FileOnibus, "2026-03-05 05:02", "2026-03-06 20:00", "2026-03-06 20:01", "saida_real", 2},
		{"duracao da sessao", FileSessoes, "2026-03-05 00:00,200", "2026-03-06 20:00,200", "2026-03-06 20:01,200", "fim", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Load(mutate(t, demo, c.file, c.old, c.ok)); err != nil {
				t.Errorf("at the cap must be accepted: %v", err)
			}
			_, err := Load(mutate(t, demo, c.file, c.old, c.bad))
			wantFieldErr(t, err, c.file, c.line, c.col)
		})
	}
}

func TestLoadRangeBoundaries(t *testing.T) {
	demo := demoFiles(t)
	type tc struct {
		name, file, old, new, col string
		line                      int
		ok                        bool
	}
	cases := []tc{
		{"capacidade 1", FileOnibus, "B02,300,2026-03-04 22:00", "B02,1,2026-03-04 22:00", "capacidade_kwh", 3, true},
		{"capacidade 2000", FileOnibus, "B02,300,2026-03-04 22:00", "B02,2000,2026-03-04 22:00", "capacidade_kwh", 3, true},
		{"capacidade 0.9", FileOnibus, "B02,300,2026-03-04 22:00", "B02,0.9,2026-03-04 22:00", "capacidade_kwh", 3, false},
		{"capacidade 2001", FileOnibus, "B02,300,2026-03-04 22:00", "B02,2001,2026-03-04 22:00", "capacidade_kwh", 3, false},
		{"soc chegada 0", FileOnibus, "2026-03-04 23:30,20,", "2026-03-04 23:30,0,", "soc_chegada_pct", 4, true},
		{"soc chegada 100", FileOnibus, "2026-03-04 23:30,20,", "2026-03-04 23:30,100,", "soc_chegada_pct", 4, true},
		{"bateria 5000", FileOnibus, "05:30,90,150,90", "05:30,90,5000,90", "potencia_max_bateria_kw", 4, true},
		{"bateria 5001", FileOnibus, "05:30,90,150,90", "05:30,90,5001,90", "potencia_max_bateria_kw", 4, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(mutate(t, demo, c.file, c.old, c.new))
			if c.ok {
				if err != nil {
					t.Errorf("want accepted, got %v", err)
				}
				return
			}
			wantFieldErr(t, err, c.file, c.line, c.col)
		})
	}
}

func TestLoadPeakWindowEmptyIsError(t *testing.T) {
	_, err := Load(mutate(t, demoFiles(t), FileGaragem, "18:00,21:00", "18:00,18:00"))
	wantFieldErr(t, err, FileGaragem, 2, "ponta_fim")
}

func TestLoadGarageExtraRowsWarningLine(t *testing.T) {
	m := mutate(t, demoFiles(t), FileGaragem, "400,18:00,21:00,2.70,0.90\n", "400,18:00,21:00,2.70,0.90\n999,,,,\n")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range d.Warnings {
		if w.File == FileGaragem && strings.Contains(w.Message, "primeira") {
			if w.Line != 3 {
				t.Errorf("warning line = %d, want 3", w.Line)
			}
			return
		}
	}
	t.Errorf("no extra-rows warning: %v", d.Warnings)
}

func TestLoadHeaderOnlyOptionalFilesOneWarning(t *testing.T) {
	m := demoFiles(t)
	m[FileSessoes] = []byte("onibus_id,carregador_id,inicio,fim,energia_kwh\n")
	m[FilePotencia] = []byte("instante,carregador_id,potencia_kw\n")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	if d.HasSessions || d.HasPower || d.Sessions != nil || d.Power != nil {
		t.Errorf("must be treated as absent: %+v", d)
	}
	for _, f := range []string{FileSessoes, FilePotencia} {
		if n := countWarnings(d, f, ""); n != 1 {
			t.Errorf("%s: %d warnings, want exactly 1: %v", f, n, d.Warnings)
		}
	}
}

func TestLoadDuplicatePowerReadingsWarn(t *testing.T) {
	m := demoFiles(t)
	m[FilePotencia] = []byte(strings.Join([]string{
		"instante,carregador_id,potencia_kw",
		"2026-03-05 22:00,C01,100",
		"2026-03-05 22:00,C02,20",
		"2026-03-05 22:15,C01,100",
		"2026-03-05 22:00,C01,90", // line 5: second reading of C01 at 22:00
	}, "\n") + "\n")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	var got []Warning
	for _, w := range d.Warnings {
		if w.File == FilePotencia && strings.Contains(w.Message, "repetid") {
			got = append(got, w)
		}
	}
	if len(got) != 1 || got[0].Line != 5 {
		t.Errorf("want one duplicate warning at line 5, got %v (all: %v)", got, d.Warnings)
	} else if m := got[0].Message; !strings.Contains(m, "última leitura") || strings.Contains(m, "duplicadas") {
		// the integral keeps the last reading of an instant: the text must say so, not "sums are duplicated"
		t.Errorf("duplicate-reading warning does not say the last reading wins: %q", m)
	}
	// the demo has the same instant for different chargers only: no warning
	d, _ = Load(demoFiles(t))
	if n := countWarnings(d, FilePotencia, "repetid"); n != 0 {
		t.Errorf("demo warned about duplicates: %v", d.Warnings)
	}
}

// Power is sorted per charger, not globally: interleaved chargers whose samples
// are each in order stay in file order, even when that is not global time order.
func TestLoadPowerNotGloballySorted(t *testing.T) {
	m := demoFiles(t)
	m[FilePotencia] = []byte(strings.Join([]string{
		"instante,carregador_id,potencia_kw",
		"2026-03-05 22:30,C01,10",
		"2026-03-05 22:00,C02,20",
	}, "\n") + "\n")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Power) != 2 || d.Power[0].ChargerID != "C01" || countWarnings(d, FilePotencia, "ordem") != 0 {
		t.Errorf("per-charger order must be kept untouched: %+v %v", d.Power, d.Warnings)
	}
}

func TestLoadDefaultMinWordingMentionsSmallCharger(t *testing.T) {
	m := demoFiles(t)
	m[FileCarregadores] = []byte("carregador_id,potencia_max_kw\nC01,150\nC02,150\nC03,3\n")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	if d.Chargers[2].MinKW != 3 || d.Chargers[0].MinKW != 5 {
		t.Errorf("min defaults = %+v", d.Chargers)
	}
	for _, w := range d.Warnings {
		if w.File == FileCarregadores && strings.Contains(w.Message, "padrões usados") {
			if !strings.Contains(w.Message, "potência máxima do carregador") || !strings.Contains(w.Message, "menor") {
				t.Errorf("wording must state min(5, potencia_max_kw): %q", w.Message)
			}
			return
		}
	}
	t.Errorf("no defaults warning: %v", d.Warnings)
}

func TestLoadDirErrorHidesPathAndOSText(t *testing.T) {
	dir := t.TempDir()
	for n, b := range demoFiles(t) {
		if err := os.WriteFile(filepath.Join(dir, n), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.Remove(filepath.Join(dir, FileOnibus))
	if err := os.Mkdir(filepath.Join(dir, FileOnibus), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := LoadDir(dir)
	var fe *FieldError
	if !errors.As(err, &fe) || fe.File != FileOnibus {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), dir) || strings.Contains(strings.ToLower(err.Error()), "no such") || strings.Contains(err.Error(), "/") {
		t.Errorf("error leaks path or OS text: %q", err.Error())
	}
	// unreadable file: the OS error text carries the absolute path
	os.Remove(filepath.Join(dir, FileOnibus))
	if err := os.WriteFile(filepath.Join(dir, FileOnibus), []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	if f, oerr := os.Open(filepath.Join(dir, FileOnibus)); oerr == nil {
		f.Close() // running as root: permissions do not apply
	} else {
		_, err = LoadDir(dir)
		if !errors.As(err, &fe) || fe.File != FileOnibus || strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "permission") {
			t.Errorf("unreadable file error leaks path or OS text: %v", err)
		}
	}
	_, err = LoadDir(filepath.Join(dir, "nao-existe"))
	if err == nil || strings.Contains(err.Error(), "nao-existe") || strings.Contains(err.Error(), dir) {
		t.Errorf("missing dir error leaks path: %v", err)
	}
}

func TestLoadDuplicateKeyFarFutureYear(t *testing.T) {
	// readings outside the years UnixNano can represent (1678-2262) must neither
	// collide with each other nor lose a true duplicate
	power := "instante,carregador_id,potencia_kw\n" +
		"2300-01-01 10:00,C01,10\n" +
		"2300-01-01 10:01,C01,10\n" +
		"9999-12-31 23:59,C01,10\n" +
		"1500-06-01 10:00,C01,10\n" +
		"1500-06-01 10:00,C02,10\n"
	d, err := Load(withFile(demoFiles(t), FilePotencia, power))
	if err != nil {
		t.Fatal(err)
	}
	if n := countWarnings(d, FilePotencia, "repetida"); n != 0 {
		t.Errorf("false duplicate warning: %v", d.Warnings)
	}
	d, err = Load(withFile(demoFiles(t), FilePotencia, power+"2300-01-01 10:00,C01,20\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n := countWarnings(d, FilePotencia, "repetida"); n != 1 {
		t.Errorf("true duplicate in year 2300 not reported: %v", d.Warnings)
	}
}

// reserveFiles is a one-bus depot: R1 is parked 22:00 -> 15:00 (a reserve bus) and topped up at 12:10.
func reserveFiles(onibus, sessoes string) map[string][]byte {
	return map[string][]byte{
		FileGaragem:      []byte("limite_kw\n400\n"),
		FileCarregadores: []byte("carregador_id,potencia_max_kw\nC01,150\n"),
		FileOnibus:       []byte("onibus_id,capacidade_kwh,chegada,soc_chegada_pct,saida_prevista,soc_saida_exigido_pct\n" + onibus),
		FileSessoes:      []byte("onibus_id,carregador_id,inicio,fim,energia_kwh\n" + sessoes),
	}
}

func TestLoadSessionOfAReserveBusParkedPastNoon(t *testing.T) {
	// the 12:10 top-up of the next day is NightKey 2026-03-05, a night R1 has no stay in:
	// it still belongs to the stay that began on the 4th
	d, err := Load(reserveFiles("R1,300,2026-03-04 22:00,30,2026-03-05 15:00,90\n",
		"R1,C01,2026-03-04 22:30,2026-03-05 01:30,100\nR1,C01,2026-03-05 12:10,2026-03-05 12:40,20\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d.Sessions[0].Night != "2026-03-04" || d.Sessions[1].Night != "2026-03-04" {
		t.Errorf("nights = %q, %q; want both 2026-03-04", d.Sessions[0].Night, d.Sessions[1].Night)
	}
	ns, _ := d.Nights(NightOptions{})
	if len(ns) != 1 || ns[0].Real.EnergyKWh == nil || *ns[0].Real.EnergyKWh != 120 {
		t.Fatalf("the top-up counts in the night of the stay: %+v", ns)
	}
	if ns[0].Real.Partial {
		t.Errorf("the only bus has sessions: %+v", ns[0].Real)
	}
}

func TestLoadSessionWindowUsesTheRealDepartureAndTwoHours(t *testing.T) {
	// planned departure 15:00, real 17:00: a session at 18:30 is still inside (17:00 + 2 h)
	on := "R1,300,2026-03-04 22:00,30,2026-03-05 15:00,90,,,2026-03-05 17:00\n"
	files := reserveFiles("", "R1,C01,2026-03-05 18:30,2026-03-05 19:00,10\n")
	files[FileOnibus] = []byte("onibus_id,capacidade_kwh,chegada,soc_chegada_pct,saida_prevista,soc_saida_exigido_pct,potencia_max_bateria_kw,soc_saida_real_pct,saida_real\n" + on)
	if d, err := Load(files); err != nil || d.Sessions[0].Night != "2026-03-04" {
		t.Fatalf("18:30 is within 2 h of the real departure: %v", err)
	}
	// 19:01 is past the window and the bus has no stay in night 2026-03-05: error naming its nights
	files[FileSessoes] = []byte("onibus_id,carregador_id,inicio,fim,energia_kwh\nR1,C01,2026-03-05 19:01,2026-03-05 19:30,10\n")
	_, err := Load(files)
	var fe *FieldError
	if !errors.As(err, &fe) || fe.File != FileSessoes || fe.Line != 2 || fe.Column != "onibus_id" ||
		!strings.Contains(fe.Message, "noites desse ônibus: 2026-03-04") || strings.Contains(fe.Message, "não existe") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadSessionPicksTheRightOfTwoStaysOfTheSameBus(t *testing.T) {
	// stay A arrives 2026-03-04 22:00 (night 03-04); stay B arrives 2026-03-05 22:00 (night 03-05)
	on := "R1,300,2026-03-04 22:00,30,2026-03-05 06:00,90\nR1,300,2026-03-05 22:00,30,2026-03-06 06:00,90\n"
	d, err := Load(reserveFiles(on,
		"R1,C01,2026-03-04 22:30,2026-03-05 01:00,50\nR1,C01,2026-03-05 22:30,2026-03-06 01:00,60\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Sessions[0].Night != "2026-03-04" || d.Sessions[1].Night != "2026-03-05" {
		t.Errorf("nights = %q, %q", d.Sessions[0].Night, d.Sessions[1].Night)
	}
	// windows that overlap (A leaves 22:30, B arrives 22:00 the same day): the stay that began last wins
	on = "R1,300,2026-03-04 22:00,30,2026-03-05 22:30,90\nR1,300,2026-03-05 22:00,30,2026-03-06 06:00,90\n"
	d, err = Load(reserveFiles(on, "R1,C01,2026-03-05 22:10,2026-03-05 23:00,10\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Sessions[0].Night != "2026-03-05" {
		t.Errorf("overlap: night = %q, want the later stay's 2026-03-05", d.Sessions[0].Night)
	}
}

func TestLoadTwoStaysOfOneBusInOneNightExplainTheRule(t *testing.T) {
	// arrivals at 13:00 and at 22:00 on the 4th both belong to night 2026-03-04 (arrival minus 12 h)
	on := "R1,300,2026-03-04 13:00,30,2026-03-04 15:00,90\nR1,300,2026-03-04 22:00,30,2026-03-05 06:00,90\n"
	_, err := Load(reserveFiles(on, ""))
	var fe *FieldError
	if !errors.As(err, &fe) || fe.File != FileOnibus || fe.Line != 3 || fe.Column != "onibus_id" {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"duplicado na noite 2026-03-04", "linha 2", "chegada menos 12 h", "dois períodos do mesmo ônibus na mesma noite não são aceitos", "separe-os ou remova o período de dia"} {
		if !strings.Contains(fe.Message, want) {
			t.Errorf("message %q lacks %q", fe.Message, want)
		}
	}
}

// pctBusFiles is a three-bus depot whose SoC columns are given as text, to test how
// percentages are read (fractions, "%" suffix). B01..B03 stay on different nights.
func pctBusFiles(arr, req, real [3]string) map[string][]byte {
	var b strings.Builder
	b.WriteString("onibus_id,capacidade_kwh,chegada,soc_chegada_pct,saida_prevista,soc_saida_exigido_pct,potencia_max_bateria_kw,soc_saida_real_pct,saida_real\n")
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&b, "B0%d,300,2026-03-0%d 22:00,%s,2026-03-0%d 06:00,%s,150,%s,\n", i+1, i+4, arr[i], i+5, req[i], real[i])
	}
	m := reserveFiles("", "")
	m[FileOnibus] = []byte(b.String())
	delete(m, FileSessoes)
	return m
}

func TestLoadRefusesPercentagesGivenAsFractions(t *testing.T) {
	ok := [3]string{"30", "40", "20"}
	req := [3]string{"90", "85", "90"}
	real := [3]string{"91", "70", "90"}
	if _, err := Load(pctBusFiles(ok, req, real)); err != nil {
		t.Fatalf("control: %v", err)
	}
	const msg = "os valores parecem frações (0 a 1); use percentuais de 0 a 100"
	check := func(name string, files map[string][]byte, col string, line int) {
		t.Helper()
		_, err := Load(files)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.File != FileOnibus || fe.Column != col || fe.Line != line || fe.Message != msg {
			t.Errorf("%s: err = %v (want %s line %d)", name, err, col, line)
		}
	}
	check("arrival", pctBusFiles([3]string{"0.30", "0.40", "0.2"}, req, real), "soc_chegada_pct", 2)
	check("required", pctBusFiles(ok, [3]string{"0.9", "0.85", "0.9"}, real), "soc_saida_exigido_pct", 2)
	check("real", pctBusFiles(ok, req, [3]string{"", "0.7", "0.9"}), "soc_saida_real_pct", 3) // first non-empty cell
	// a value of 1 is still a fraction when nothing is above it
	check("ones", pctBusFiles(ok, [3]string{"1", "1", "0.5"}, real), "soc_saida_exigido_pct", 2)
	// accepted: one 0.5 among larger values; all zeros; real outcome absent
	for name, files := range map[string]map[string][]byte{
		"mixed":     pctBusFiles([3]string{"0.5", "40", "20"}, req, real),
		"all zeros": pctBusFiles([3]string{"0", "0", "0"}, req, real),
		"no real":   pctBusFiles(ok, req, [3]string{"", "", ""}),
		"ok":        pctBusFiles(ok, req, real),
	} {
		if _, err := Load(files); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLoadAcceptsPercentSignInPctColumns(t *testing.T) {
	d, err := Load(pctBusFiles([3]string{"30%", "40 %", "20"}, [3]string{"90%", "85", "90%"}, [3]string{"91%", "70", ""}))
	if err != nil {
		t.Fatal(err)
	}
	b := d.Buses
	if b[0].SoCArrivalPct != 30 || b[1].SoCArrivalPct != 40 || b[0].RequiredPct != 90 || b[2].RequiredPct != 90 ||
		b[0].RealSoCPct == nil || *b[0].RealSoCPct != 91 || b[2].RealSoCPct != nil {
		t.Errorf("buses = %+v", b)
	}
	// a lone "%" or "%%" is not a number
	for _, bad := range []string{"%", "30%%", "abc%"} {
		_, err := Load(pctBusFiles([3]string{bad, "40", "20"}, [3]string{"90", "85", "90"}, [3]string{"", "", ""}))
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Column != "soc_chegada_pct" {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestLoadMissingColumnListsTheHeadersFound(t *testing.T) {
	m := mutate(t, demoFiles(t), FileOnibus, "capacidade_kwh,", "capacidade,")
	_, err := Load(m)
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Column != "capacidade_kwh" || !strings.HasPrefix(fe.Message, "coluna obrigatória ausente") {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"cabeçalhos encontrados", "onibus_id", "capacidade,", "soc_saida_real_pct"} {
		if !strings.Contains(fe.Message, want) {
			t.Errorf("message lacks %q: %s", want, fe.Message)
		}
	}
	// a header with hundreds of columns is cut short, each name too
	long := strings.Repeat("x", 500)
	cols := []string{long}
	for i := 0; i < 40; i++ {
		cols = append(cols, fmt.Sprintf("c%d", i))
	}
	m = map[string][]byte{FileGaragem: []byte(strings.Join(cols, ",") + "\n1,2\n"), FileCarregadores: nil, FileOnibus: nil}
	_, err = Load(m)
	if !errors.As(err, &fe) || len(fe.Message) > 1500 || !strings.Contains(fe.Message, "… e mais") {
		t.Errorf("long header list not cut: %d bytes: %v", len(fe.Message), err)
	}
}

func TestLoadDateErrorExplainsTheAcceptedFormats(t *testing.T) {
	for _, bad := range []string{"2026-03-04T20:00Z", "04/03/26 20:00", "2026-03-04 20:00+00:00"} {
		m := mutate(t, demoFiles(t), FileOnibus, "2026-03-04 20:00,30", bad+",30")
		_, err := Load(m)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Column != "chegada" {
			t.Fatalf("%q: err = %v", bad, err)
		}
		for _, want := range []string{"AAAA-MM-DD HH:MM", "DD/MM/AAAA", "sem fuso", "Z", "2 dígitos"} {
			if !strings.Contains(fe.Message, want) {
				t.Errorf("%q: message lacks %q: %s", bad, want, fe.Message)
			}
		}
	}
}

// Text the user controls (a header, a file name, a bus id) never comes back unbounded.
func TestLoadEchoedUserTextIsBounded(t *testing.T) {
	long := strings.Repeat("é", 5000)
	m := demoFiles(t)
	m[long] = []byte("x")
	m = mutate(t, m, FileGaragem, "preco_fora_ponta\n", "preco_fora_ponta,"+long+"\n")
	m = mutate(t, m, FileGaragem, "0.90\n", "0.90,x\n")
	d, err := Load(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range d.Warnings {
		if n := len([]rune(w.Message)) + len([]rune(w.File)); n > 700 {
			t.Errorf("warning of %d runes: %.80q…", n, w.Message)
		}
	}
	// a duplicated header, and an unknown bus id in a session
	_, err = Load(mutate(t, demoFiles(t), FileGaragem, "limite_kw", long+","+long+",limite_kw"))
	if err == nil || len([]rune(err.Error())) > 700 {
		t.Errorf("duplicate-header error: %d runes", len([]rune(fmt.Sprint(err))))
	}
	_, err = Load(mutate(t, demoFiles(t), FileSessoes, "B01,C01", long+",C01"))
	if err == nil || len([]rune(err.Error())) > 700 {
		t.Errorf("unknown-bus error: %d runes", len([]rune(fmt.Sprint(err))))
	}
}
