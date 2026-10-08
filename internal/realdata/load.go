package realdata

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/sim"
)

// Names of the five fixed spreadsheet files.
const (
	FileGaragem, FileCarregadores, FileOnibus = "garagem.csv", "carregadores.csv", "onibus.csv"
	FileSessoes, FilePotencia                 = "sessoes.csv", "potencia.csv"
)

// FileNames lists the accepted file names; anything else is ignored with a warning.
var FileNames = []string{FileGaragem, FileCarregadores, FileOnibus, FileSessoes, FilePotencia}

// Limits of the real-data importer (the simulator's own limits apply on top).
const (
	// maxRowsPerFile is applied after parsing; the real memory guard is
	// ReadTable's cap on columns x rows (maxCells).
	maxRowsPerFile   = 1_000_000
	maxBusesPerNight = 1000
	maxNights        = 366
	maxChargerKW     = 5000 // potencia_max_kw of a charger
	maxCapacityKWh   = 2000 // capacidade_kwh of a bus
	maxBatteryKW     = 5000 // potencia_max_bateria_kw

	// Sanity caps against typos (year 9999, 1e308) that would otherwise build
	// an enormous horizon or turn into Inf/NaN downstream.
	maxSessionKWh = 1e6            // energia_kwh of one session
	maxReadingKW  = 1e5            // potencia_kw of one reading (above 2x charger max is only a warning)
	maxStay       = 48 * time.Hour // saida_prevista/saida_real minus chegada; fim minus inicio of a session

	defaultMinKW      = 5.0
	defaultEfficiency = 0.94
	powerFactorWarn   = 2.0 // readings above this multiple of potencia_max_kw warn
)

// Garage holds the depot-wide data of garagem.csv.
type Garage struct {
	LimitKW                 float64
	PeakFromMin, PeakToMin  int     // minutes of the day; both 0 with HasTariff=false
	PeakPrice, OffPeakPrice float64 // R$/kWh
	HasTariff               bool
}

// Charger is one row of carregadores.csv (defaults already applied).
type Charger struct {
	ID                       string
	MaxKW, MinKW, Efficiency float64
}

// Bus is one bus-night of onibus.csv.
type Bus struct {
	Line               int // line in onibus.csv
	ID                 string
	CapacityKWh        float64
	Arrival, Departure time.Time // Departure is the planned departure
	SoCArrivalPct      float64
	RequiredPct        float64
	MaxBatteryKW       float64  // 0 = default (resolved in Nights)
	RealSoCPct         *float64 // nil = unknown
	RealDeparture      *time.Time
}

// Session is one charging session of sessoes.csv.
type Session struct {
	Line             int
	BusID, ChargerID string
	Start, End       time.Time
	EnergyKWh        float64
}

// PowerSample is one reading of potencia.csv.
type PowerSample struct {
	At        time.Time
	ChargerID string
	KW        float64
}

// Dataset is the validated content of the five spreadsheets.
type Dataset struct {
	Garage   Garage
	Chargers []Charger
	Buses    []Bus
	Sessions []Session // nil if sessoes.csv absent
	// Power is sorted by time within each charger but NOT globally: samples of
	// different chargers keep file order (a global sort happens only if some charger
	// was out of order), so consumers must not assume global time order.
	Power                 []PowerSample // nil if potencia.csv absent
	HasSessions, HasPower bool
	Warnings              []Warning
}

// NightKey names the night an instant belongs to: the date of (t - 12h).
func NightKey(t time.Time) string {
	return t.Add(-12 * time.Hour).Format("2006-01-02")
}

// Load validates the spreadsheets (file name -> bytes). The error, when not
// nil, is a *FieldError. Names outside FileNames are ignored with a warning.
func Load(files map[string][]byte) (*Dataset, error) {
	ds := &Dataset{}

	var extra []string
	for name := range files {
		if !isKnownFile(name) {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		ds.Warnings = append(ds.Warnings, Warning{File: name, Message: "arquivo não reconhecido, ignorado (nomes aceitos: " + strings.Join(FileNames, ", ") + ")"})
	}

	for _, name := range []string{FileGaragem, FileCarregadores, FileOnibus} {
		if _, ok := files[name]; !ok {
			return nil, &FieldError{File: name, Message: "arquivo obrigatório ausente"}
		}
	}

	read := func(name string) (*Table, error) {
		t, err := ReadTable(name, files[name])
		if err != nil {
			return nil, err
		}
		if len(t.Rows) > maxRowsPerFile {
			return nil, &FieldError{File: name, Message: "arquivo com linhas demais (máximo 1.000.000 de linhas de dados)"}
		}
		return t, nil
	}

	t, err := read(FileGaragem)
	if err != nil {
		return nil, err
	}
	if ds.Garage, err = loadGarage(t); err != nil {
		return nil, err
	}
	ds.Warnings = append(ds.Warnings, t.Warnings...)

	if t, err = read(FileCarregadores); err != nil {
		return nil, err
	}
	if ds.Chargers, err = loadChargers(t); err != nil {
		return nil, err
	}
	ds.Warnings = append(ds.Warnings, t.Warnings...)

	if t, err = read(FileOnibus); err != nil {
		return nil, err
	}
	if ds.Buses, err = loadBuses(t); err != nil {
		return nil, err
	}
	ds.Warnings = append(ds.Warnings, t.Warnings...)

	if _, ok := files[FileSessoes]; ok {
		if t, err = read(FileSessoes); err != nil {
			return nil, err
		}
		if ds.Sessions, err = loadSessions(t, ds); err != nil {
			return nil, err
		}
		if len(ds.Sessions) == 0 {
			ds.Sessions = nil
			// ReadTable already warned "sem linhas de dados"; treated as absent.
		}
		ds.HasSessions = ds.Sessions != nil
		ds.Warnings = append(ds.Warnings, t.Warnings...)
	}

	if _, ok := files[FilePotencia]; ok {
		if t, err = read(FilePotencia); err != nil {
			return nil, err
		}
		if ds.Power, err = loadPower(t, ds); err != nil {
			return nil, err
		}
		if len(ds.Power) == 0 {
			ds.Power = nil
			// ReadTable already warned "sem linhas de dados"; treated as absent.
		}
		ds.HasPower = ds.Power != nil
		ds.Warnings = append(ds.Warnings, t.Warnings...)
	}
	return ds, nil
}

// LoadDir reads only the five fixed file names inside dir and calls Load.
func LoadDir(dir string) (*Dataset, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, &FieldError{Message: "pasta não encontrada ou inacessível"}
	}
	files := map[string][]byte{}
	for _, name := range FileNames {
		b, err := readFixed(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			// no raw OS text: it carries the absolute path
			return nil, &FieldError{File: name, Message: "não foi possível ler " + name + " (confira se é um arquivo comum e se há permissão de leitura)"}
		}
		files[name] = b
	}
	return Load(files)
}

// readFixed reads a regular file, at most maxFileBytes+1 bytes (enough for
// ReadTable to reject an oversized file without loading all of it).
func readFixed(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxFileBytes+1))
}

func isKnownFile(name string) bool {
	for _, n := range FileNames {
		if n == name {
			return true
		}
	}
	return false
}

// --- helpers ---

// requireCols checks that every required column is in the header. It must run
// before any row is read: Float/Str/Time on an unknown column behave like an
// empty cell, so a typo in a header would otherwise look like missing data.
func requireCols(t *Table, cols ...string) *FieldError {
	for _, c := range cols {
		if t.Col(c) < 0 {
			return &FieldError{File: t.File, Line: t.headerLine, Column: c, Message: "coluna obrigatória ausente"}
		}
	}
	return nil
}

func reqStr(t *Table, r Row, col string) (string, *FieldError) {
	s := t.Str(r, col)
	if s == "" {
		return "", t.fieldErr(r, col, "valor obrigatório ausente")
	}
	return s, nil
}

func reqFloat(t *Table, r Row, col string) (float64, *FieldError) {
	v, ok, err := t.Float(r, col)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, t.fieldErr(r, col, "valor obrigatório ausente")
	}
	return v, nil
}

func reqTime(t *Table, r Row, col string) (time.Time, *FieldError) {
	v, ok, err := t.Time(r, col)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Time{}, t.fieldErr(r, col, "valor obrigatório ausente")
	}
	return v, nil
}

// between checks lo <= v <= hi (loOpen: lo < v). unit is appended to the message.
func between(t *Table, r Row, col string, v, lo, hi float64, loOpen bool, unit string) *FieldError {
	if v > hi || v < lo || (loOpen && v == lo) {
		if loOpen && lo == 0 {
			return t.fieldErr(r, col, fmt.Sprintf("%g%s fora da faixa: deve ser maior que 0 e no máximo %g%s", v, unit, hi, unit))
		}
		return t.fieldErr(r, col, fmt.Sprintf("%g%s fora da faixa: deve estar entre %g%s e %g%s", v, unit, lo, unit, hi, unit))
	}
	return nil
}

// parseClock reads HH:MM into minutes of the day.
func parseClock(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(h) < 1 || len(h) > 2 || len(m) != 2 {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || h[0] == '+' || h[0] == '-' || m[0] == '+' || m[0] == '-' || hh > 23 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// --- garagem.csv ---

func loadGarage(t *Table) (Garage, error) {
	var g Garage
	if ferr := requireCols(t, "limite_kw"); ferr != nil {
		return g, ferr
	}
	t.Known("limite_kw", "ponta_inicio", "ponta_fim", "preco_ponta", "preco_fora_ponta")
	if len(t.Rows) == 0 {
		return g, &FieldError{File: t.File, Message: "o arquivo precisa de uma linha de dados"}
	}
	if len(t.Rows) > 1 {
		t.warn(t.Rows[1].Line, fmt.Sprintf("o arquivo tem %d linhas de dados; só a primeira é usada", len(t.Rows)))
	}
	r := t.Rows[0]

	limit, ferr := reqFloat(t, r, "limite_kw")
	if ferr != nil {
		return g, ferr
	}
	if limit <= 0 || limit > sim.MaxLimitKW {
		return g, t.fieldErr(r, "limite_kw", fmt.Sprintf("%g kW fora da faixa: deve ser maior que 0 e no máximo %g kW", limit, float64(sim.MaxLimitKW)))
	}
	g.LimitKW = limit

	clock := func(col string) (int, bool, *FieldError) {
		s := t.Str(r, col)
		if s == "" {
			return 0, false, nil
		}
		m, ok := parseClock(s)
		if !ok {
			return 0, false, t.fieldErr(r, col, fmt.Sprintf("horário %s inválido: use HH:MM", shorten(s)))
		}
		return m, true, nil
	}
	price := func(col string) (float64, bool, *FieldError) {
		v, ok, err := t.Float(r, col)
		if err != nil {
			return 0, false, err
		}
		if ok && v < 0 {
			return 0, false, t.fieldErr(r, col, fmt.Sprintf("preço %g negativo: deve ser maior ou igual a 0", v))
		}
		return v, ok, nil
	}
	from, okFrom, ferr := clock("ponta_inicio")
	if ferr != nil {
		return g, ferr
	}
	to, okTo, ferr := clock("ponta_fim")
	if ferr != nil {
		return g, ferr
	}
	peak, okPeak, ferr := price("preco_ponta")
	if ferr != nil {
		return g, ferr
	}
	off, okOff, ferr := price("preco_fora_ponta")
	if ferr != nil {
		return g, ferr
	}
	if okFrom && okTo && okPeak && okOff {
		if from == to {
			return g, t.fieldErr(r, "ponta_fim", "ponta_fim igual a ponta_inicio: a janela de ponta ficaria vazia")
		}
		g.PeakFromMin, g.PeakToMin, g.PeakPrice, g.OffPeakPrice, g.HasTariff = from, to, peak, off, true
	} else {
		t.warn(r.Line, "tarifa incompleta: custo não será calculado (informe ponta_inicio, ponta_fim, preco_ponta e preco_fora_ponta)")
	}
	return g, nil
}

// --- carregadores.csv ---

func loadChargers(t *Table) ([]Charger, error) {
	if ferr := requireCols(t, "carregador_id", "potencia_max_kw"); ferr != nil {
		return nil, ferr
	}
	t.Known("carregador_id", "potencia_max_kw", "potencia_min_kw", "eficiencia")
	if len(t.Rows) == 0 {
		return nil, &FieldError{File: t.File, Message: "o arquivo precisa de pelo menos um carregador"}
	}
	if len(t.Rows) > sim.MaxChargers {
		return nil, &FieldError{File: t.File, Line: t.Rows[sim.MaxChargers].Line, Message: fmt.Sprintf("carregadores demais (máximo %d)", sim.MaxChargers)}
	}
	out := make([]Charger, 0, len(t.Rows))
	seen := make(map[string]bool, len(t.Rows))
	var defMin, defEff int
	for _, r := range t.Rows {
		id, ferr := reqStr(t, r, "carregador_id")
		if ferr != nil {
			return nil, ferr
		}
		if seen[id] {
			return nil, t.fieldErr(r, "carregador_id", fmt.Sprintf("carregador %s duplicado", shorten(id)))
		}
		seen[id] = true
		maxKW, ferr := reqFloat(t, r, "potencia_max_kw")
		if ferr != nil {
			return nil, ferr
		}
		if ferr = between(t, r, "potencia_max_kw", maxKW, 0, maxChargerKW, true, " kW"); ferr != nil {
			return nil, ferr
		}
		minKW, ok, err := t.Float(r, "potencia_min_kw")
		if err != nil {
			return nil, err
		}
		if !ok {
			minKW = min(defaultMinKW, maxKW)
			defMin++
		} else if minKW < 0 || minKW > maxKW {
			return nil, t.fieldErr(r, "potencia_min_kw", fmt.Sprintf("%g kW fora da faixa: deve estar entre 0 e a potência máxima (%g kW)", minKW, maxKW))
		}
		eff, ok, err := t.Float(r, "eficiencia")
		if err != nil {
			return nil, err
		}
		if !ok {
			eff = defaultEfficiency
			defEff++
		} else if eff <= 0 || eff > 1 {
			return nil, t.fieldErr(r, "eficiencia", fmt.Sprintf("eficiência %g fora da faixa: deve ser maior que 0 e no máximo 1 (ex.: 0.94)", eff))
		}
		out = append(out, Charger{ID: id, MaxKW: maxKW, MinKW: minKW, Efficiency: eff})
	}
	var parts []string
	if defMin > 0 {
		parts = append(parts, fmt.Sprintf("potencia_min_kw = 5 kW (ou a potência máxima do carregador, se for menor) em %s", plural(defMin, "carregador", "carregadores")))
	}
	if defEff > 0 {
		parts = append(parts, fmt.Sprintf("eficiencia = 0,94 em %s", plural(defEff, "carregador", "carregadores")))
	}
	if len(parts) > 0 {
		t.warn(0, "padrões usados: "+strings.Join(parts, "; "))
	}
	return out, nil
}

// --- onibus.csv ---

func loadBuses(t *Table) ([]Bus, error) {
	if ferr := requireCols(t, "onibus_id", "capacidade_kwh", "chegada", "soc_chegada_pct", "saida_prevista", "soc_saida_exigido_pct"); ferr != nil {
		return nil, ferr
	}
	t.Known("onibus_id", "capacidade_kwh", "chegada", "soc_chegada_pct", "saida_prevista", "soc_saida_exigido_pct",
		"potencia_max_bateria_kw", "soc_saida_real_pct", "saida_real")
	if len(t.Rows) == 0 {
		return nil, &FieldError{File: t.File, Message: "o arquivo precisa de pelo menos um ônibus"}
	}
	if len(t.Rows) > sim.MaxBuses {
		return nil, &FieldError{File: t.File, Line: t.Rows[sim.MaxBuses].Line, Message: fmt.Sprintf("ônibus demais no total (máximo %d)", sim.MaxBuses)}
	}
	out := make([]Bus, 0, len(t.Rows))
	seen := make(map[string]bool, len(t.Rows))
	perNight := map[string]int{}
	var defBat, noReal int
	for _, r := range t.Rows {
		var b Bus
		b.Line = r.Line
		var ferr *FieldError
		if b.ID, ferr = reqStr(t, r, "onibus_id"); ferr != nil {
			return nil, ferr
		}
		if b.CapacityKWh, ferr = reqFloat(t, r, "capacidade_kwh"); ferr != nil {
			return nil, ferr
		}
		if ferr = between(t, r, "capacidade_kwh", b.CapacityKWh, 1, maxCapacityKWh, false, " kWh"); ferr != nil {
			return nil, ferr
		}
		if b.Arrival, ferr = reqTime(t, r, "chegada"); ferr != nil {
			return nil, ferr
		}
		if b.SoCArrivalPct, ferr = reqFloat(t, r, "soc_chegada_pct"); ferr != nil {
			return nil, ferr
		}
		if ferr = between(t, r, "soc_chegada_pct", b.SoCArrivalPct, 0, 100, false, " %"); ferr != nil {
			return nil, ferr
		}
		if b.Departure, ferr = reqTime(t, r, "saida_prevista"); ferr != nil {
			return nil, ferr
		}
		if !b.Departure.After(b.Arrival) {
			return nil, t.fieldErr(r, "saida_prevista", "a saída prevista precisa ser depois da chegada")
		}
		if b.Departure.Sub(b.Arrival) > maxStay {
			return nil, t.fieldErr(r, "saida_prevista", "a permanência (saída prevista menos chegada) passa de 48 horas: confira a data")
		}
		if b.RequiredPct, ferr = reqFloat(t, r, "soc_saida_exigido_pct"); ferr != nil {
			return nil, ferr
		}
		if ferr = between(t, r, "soc_saida_exigido_pct", b.RequiredPct, 0, 100, true, " %"); ferr != nil {
			return nil, ferr
		}
		v, ok, err := t.Float(r, "potencia_max_bateria_kw")
		if err != nil {
			return nil, err
		}
		if ok {
			if ferr = between(t, r, "potencia_max_bateria_kw", v, 0, maxBatteryKW, true, " kW"); ferr != nil {
				return nil, ferr
			}
			b.MaxBatteryKW = v
		} else {
			defBat++
		}
		v, ok, err = t.Float(r, "soc_saida_real_pct")
		if err != nil {
			return nil, err
		}
		if ok {
			if ferr = between(t, r, "soc_saida_real_pct", v, 0, 100, false, " %"); ferr != nil {
				return nil, ferr
			}
			b.RealSoCPct = &v
		} else {
			noReal++
		}
		rd, ok, err := t.Time(r, "saida_real")
		if err != nil {
			return nil, err
		}
		if ok {
			if !rd.After(b.Arrival) {
				return nil, t.fieldErr(r, "saida_real", "a saída real precisa ser depois da chegada")
			}
			if rd.Sub(b.Arrival) > maxStay {
				return nil, t.fieldErr(r, "saida_real", "a permanência (saída real menos chegada) passa de 48 horas: confira a data")
			}
			b.RealDeparture = &rd
		}

		night := NightKey(b.Arrival)
		key := b.ID + "\x00" + night
		if seen[key] {
			return nil, t.fieldErr(r, "onibus_id", fmt.Sprintf("ônibus %s duplicado na noite %s", shorten(b.ID), night))
		}
		seen[key] = true
		if _, known := perNight[night]; !known && len(perNight) >= maxNights {
			return nil, &FieldError{File: t.File, Line: r.Line, Message: fmt.Sprintf("noites demais (máximo %d)", maxNights)}
		}
		if perNight[night]++; perNight[night] > maxBusesPerNight {
			return nil, &FieldError{File: t.File, Line: r.Line, Message: fmt.Sprintf("ônibus demais na noite %s (máximo %d por noite)", night, maxBusesPerNight)}
		}
		out = append(out, b)
	}
	if defBat > 0 {
		t.warn(0, fmt.Sprintf("padrões usados: potencia_max_bateria_kw = potência máxima do carregador mais comum em %s", plural(defBat, "ônibus", "ônibus")))
	}
	if noReal > 0 {
		t.warn(0, fmt.Sprintf("soc_saida_real_pct vazio em %s: o desfecho real 'pronto' não será calculado para eles", plural(noReal, "ônibus", "ônibus")))
	}
	return out, nil
}

// --- sessoes.csv ---

func loadSessions(t *Table, ds *Dataset) ([]Session, error) {
	if ferr := requireCols(t, "onibus_id", "carregador_id", "inicio", "fim", "energia_kwh"); ferr != nil {
		return nil, ferr
	}
	t.Known("onibus_id", "carregador_id", "inicio", "fim", "energia_kwh")
	chargers := make(map[string]bool, len(ds.Chargers))
	for _, c := range ds.Chargers {
		chargers[c.ID] = true
	}
	buses := make(map[string]bool, len(ds.Buses))
	for _, b := range ds.Buses {
		buses[b.ID+"\x00"+NightKey(b.Arrival)] = true
	}
	out := make([]Session, 0, len(t.Rows))
	for _, r := range t.Rows {
		s := Session{Line: r.Line}
		var ferr *FieldError
		if s.BusID, ferr = reqStr(t, r, "onibus_id"); ferr != nil {
			return nil, ferr
		}
		if s.ChargerID, ferr = reqStr(t, r, "carregador_id"); ferr != nil {
			return nil, ferr
		}
		if s.Start, ferr = reqTime(t, r, "inicio"); ferr != nil {
			return nil, ferr
		}
		if s.End, ferr = reqTime(t, r, "fim"); ferr != nil {
			return nil, ferr
		}
		if !s.End.After(s.Start) {
			return nil, t.fieldErr(r, "fim", "o fim da sessão precisa ser depois do início")
		}
		if s.End.Sub(s.Start) > maxStay {
			return nil, t.fieldErr(r, "fim", "a sessão dura mais de 48 horas: confira a data")
		}
		if s.EnergyKWh, ferr = reqFloat(t, r, "energia_kwh"); ferr != nil {
			return nil, ferr
		}
		if s.EnergyKWh < 0 {
			return nil, t.fieldErr(r, "energia_kwh", fmt.Sprintf("energia %g kWh negativa: deve ser maior ou igual a 0", s.EnergyKWh))
		}
		if s.EnergyKWh > maxSessionKWh {
			return nil, t.fieldErr(r, "energia_kwh", fmt.Sprintf("energia %g kWh acima do máximo aceito por sessão (%g kWh): confira a unidade", s.EnergyKWh, maxSessionKWh))
		}
		night := NightKey(s.Start)
		if !buses[s.BusID+"\x00"+night] {
			return nil, t.fieldErr(r, "onibus_id", fmt.Sprintf("ônibus %s não existe em onibus.csv na noite %s (noite = data de início menos 12 h)", shorten(s.BusID), night))
		}
		if !chargers[s.ChargerID] {
			return nil, t.fieldErr(r, "carregador_id", fmt.Sprintf("carregador %s não existe em carregadores.csv", shorten(s.ChargerID)))
		}
		out = append(out, s)
	}
	return out, nil
}

// --- potencia.csv ---

func loadPower(t *Table, ds *Dataset) ([]PowerSample, error) {
	if ferr := requireCols(t, "instante", "carregador_id", "potencia_kw"); ferr != nil {
		return nil, ferr
	}
	t.Known("instante", "carregador_id", "potencia_kw")
	maxKW := make(map[string]float64, len(ds.Chargers))
	for _, c := range ds.Chargers {
		maxKW[c.ID] = c.MaxKW
	}
	out := make([]PowerSample, 0, len(t.Rows))
	last := map[string]time.Time{}
	type readingKey struct {
		id string
		at int64
	}
	seen := make(map[readingKey]bool, len(t.Rows))
	dupFirst, dupN := 0, 0
	unsortedLine, unsorted := 0, false
	highFirst, highN := 0, 0
	for _, r := range t.Rows {
		var p PowerSample
		var ferr *FieldError
		if p.At, ferr = reqTime(t, r, "instante"); ferr != nil {
			return nil, ferr
		}
		if p.ChargerID, ferr = reqStr(t, r, "carregador_id"); ferr != nil {
			return nil, ferr
		}
		cmax, known := maxKW[p.ChargerID]
		if !known {
			return nil, t.fieldErr(r, "carregador_id", fmt.Sprintf("carregador %s não existe em carregadores.csv", shorten(p.ChargerID)))
		}
		if p.KW, ferr = reqFloat(t, r, "potencia_kw"); ferr != nil {
			return nil, ferr
		}
		if p.KW < 0 {
			return nil, t.fieldErr(r, "potencia_kw", fmt.Sprintf("potência %g kW negativa: deve ser maior ou igual a 0", p.KW))
		}
		if p.KW > maxReadingKW {
			return nil, t.fieldErr(r, "potencia_kw", fmt.Sprintf("potência %g kW acima do máximo aceito por leitura (%g kW): confira a unidade", p.KW, maxReadingKW))
		}
		if p.KW > powerFactorWarn*cmax {
			if highN == 0 {
				highFirst = r.Line
			}
			highN++
		}
		if prev, ok := last[p.ChargerID]; ok && p.At.Before(prev) && !unsorted {
			unsorted, unsortedLine = true, r.Line
		}
		last[p.ChargerID] = p.At
		k := readingKey{p.ChargerID, p.At.UnixNano()}
		if seen[k] {
			if dupN == 0 {
				dupFirst = r.Line
			}
			dupN++
		}
		seen[k] = true
		out = append(out, p)
	}
	if highN > 0 {
		t.warn(highFirst, fmt.Sprintf("%s acima de 2× a potência máxima do carregador (a primeira está nesta linha); confira as unidades", plural(highN, "leitura", "leituras")))
	}
	if dupN > 0 {
		t.warn(dupFirst, fmt.Sprintf("%s repetida(s) para o mesmo carregador e instante (a primeira repetição está nesta linha); somas por instante podem ficar duplicadas", plural(dupN, "leitura", "leituras")))
	}
	if unsorted {
		sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
		t.warn(unsortedLine, "leituras fora de ordem de horário; foram ordenadas por instante")
	}
	return out, nil
}
