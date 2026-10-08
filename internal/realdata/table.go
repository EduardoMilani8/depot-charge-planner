// Package realdata imports real depot spreadsheets (CSV) and replays them
// through the simulator. This file holds the tolerant CSV reader.
package realdata

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxFileBytes = 8 << 20
	maxColumns   = 64
	maxCells     = 2_000_000 // columns x rows; rows are padded, so bytes alone do not bound memory
)

// FieldError is a data error pointing at a file, line and column.
type FieldError struct {
	File    string
	Line    int    // 1-based physical line in the file; 0 = whole file
	Column  string // header name, "" = whole line
	Message string
}

func (e *FieldError) Error() string {
	var where []string
	if e.File != "" {
		where = append(where, e.File)
	}
	if e.Line > 0 {
		where = append(where, fmt.Sprintf("linha %d", e.Line))
	}
	if e.Column != "" {
		where = append(where, "coluna "+e.Column)
	}
	if len(where) == 0 {
		return e.Message
	}
	return strings.Join(where, ", ") + ": " + e.Message
}

// Warning is a non-fatal remark about a file (Line 0 = general).
type Warning struct {
	File    string
	Line    int
	Message string
}

// Table is a parsed CSV: Header (normalized names) and Rows with their source line numbers.
type Table struct {
	File     string
	Header   []string
	Rows     []Row
	Warnings []Warning

	headerLine int
	semicolon  bool // decimal comma allowed
}

// Row is one data record; Line is its physical line in the file.
type Row struct {
	Line   int
	Fields []string // same length as Header (short rows padded with "")
}

// ReadTable parses CSV bytes tolerantly (BOM, CRLF, ';' or ',', Windows-1252,
// blank lines). The error, when not nil, is a *FieldError.
func ReadTable(file string, data []byte) (*Table, error) {
	fail := func(line int, msg string) (*Table, error) {
		return nil, &FieldError{File: file, Line: line, Message: msg}
	}
	if len(data) > maxFileBytes {
		return fail(0, "arquivo maior que 8 MB")
	}
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE}) || bytes.HasPrefix(data, []byte{0xFE, 0xFF}) {
		return fail(0, "arquivo em UTF-16 não é suportado: salve como CSV UTF-8")
	}
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	t := &Table{File: file}
	if !utf8.Valid(data) {
		data = decodeWindows1252(data)
		t.warn(0, "arquivo não está em UTF-8; lido como Windows-1252")
	}
	comma := detectComma(data)
	t.semicolon = comma == ';'
	if ferr := checkShape(file, data, byte(comma)); ferr != nil {
		return nil, ferr
	}

	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = comma
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				line := pe.StartLine
				if line == 0 {
					line = pe.Line
				}
				return fail(line, "aspas mal fechadas ou fora de lugar no campo")
			}
			return fail(0, "não foi possível ler o arquivo: "+err.Error())
		}
		line, _ := r.FieldPos(0)
		for i := range rec {
			rec[i] = strings.TrimSpace(rec[i])
		}
		if blankRecord(rec) {
			continue
		}
		if t.Header == nil {
			for i := range rec {
				rec[i] = strings.ToLower(rec[i])
			}
			seen := map[string]bool{}
			for _, h := range rec {
				if h != "" && seen[h] {
					return fail(line, fmt.Sprintf("coluna '%s' duplicada no cabeçalho", h))
				}
				seen[h] = true
			}
			t.Header = rec
			t.headerLine = line
			continue
		}
		if len(rec) > len(t.Header) {
			if !blankRecord(rec[len(t.Header):]) {
				return fail(line, fmt.Sprintf("a linha tem %d campos, mas o cabeçalho tem %d", len(rec), len(t.Header)))
			}
			rec = rec[:len(t.Header)]
		}
		if len(t.Rows)+1 > maxCells/len(t.Header) {
			return fail(line, "o arquivo é grande demais (máximo de 2 milhões de células)")
		}
		if len(rec) < len(t.Header) {
			padded := make([]string, len(t.Header)) // one allocation, no append growth
			copy(padded, rec)
			rec = padded
		}
		t.Rows = append(t.Rows, Row{Line: line, Fields: rec})
	}
	if t.Header == nil {
		return fail(0, "arquivo vazio")
	}
	if len(t.Rows) == 0 {
		t.warn(0, "sem linhas de dados")
	}
	return t, nil
}

func (t *Table) warn(line int, msg string) {
	t.Warnings = append(t.Warnings, Warning{File: t.File, Line: line, Message: msg})
}

func blankRecord(rec []string) bool {
	for _, f := range rec {
		if f != "" {
			return false
		}
	}
	return true
}

// detectComma picks ';' or ',' by counting them outside quotes on the first
// non-blank line; a tie means ','.
func detectComma(data []byte) rune {
	var commas, semis int
	inQuote, content := false, false
	for _, b := range data {
		if b == '"' {
			inQuote = !inQuote
			content = true
			continue
		}
		if inQuote {
			continue
		}
		switch b {
		case ',':
			commas++
		case ';':
			semis++
		case '\n':
			if content {
				goto done
			}
			commas, semis = 0, 0
		case ' ', '\t', '\r':
		default:
			content = true
		}
	}
done:
	if semis > commas {
		return ';'
	}
	return ','
}

// checkShape rejects records with more than maxColumns fields before
// encoding/csv allocates them (a line of millions of separators would
// otherwise cost memory far beyond the file size). Quotes are tracked the same
// way csv does for well-formed input; malformed input fails later in the reader.
func checkShape(file string, data []byte, comma byte) *FieldError {
	line, fields := 1, 1
	inQuote, content, headerSeen := false, false, false
	for _, b := range data {
		switch {
		case b == '"':
			inQuote = !inQuote
			content = true
		case inQuote:
			if b == '\n' {
				line++
			}
		case b == comma:
			fields++
			if fields > maxColumns {
				if headerSeen {
					return &FieldError{File: file, Line: line, Message: "a linha tem campos demais (máximo 64)"}
				}
				return &FieldError{File: file, Line: line, Message: "o arquivo tem colunas demais (máximo 64)"}
			}
		case b == '\n':
			if content {
				headerSeen = true
			}
			line++
			fields, content = 1, false
		case b != ' ' && b != '\t' && b != '\r':
			content = true
		}
	}
	return nil
}

// cp1252 maps bytes 0x80-0x9F; undefined slots keep their C1 control code point.
var cp1252 = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

func decodeWindows1252(b []byte) []byte {
	out := make([]byte, 0, len(b)+len(b)/4)
	for _, c := range b {
		switch {
		case c < 0x80:
			out = append(out, c)
		case c < 0xA0:
			out = utf8.AppendRune(out, cp1252[c-0x80])
		default:
			out = utf8.AppendRune(out, rune(c))
		}
	}
	return out
}

// Known records one warning per header column that is not in cols
// (case-insensitive). Calling it again does not repeat warnings.
func (t *Table) Known(cols ...string) {
	known := make(map[string]bool, len(cols))
	for _, c := range cols {
		known[strings.ToLower(strings.TrimSpace(c))] = true
	}
	for _, h := range t.Header {
		if h == "" || known[h] {
			continue
		}
		msg := fmt.Sprintf("coluna '%s' ignorada", h)
		dup := false
		for _, w := range t.Warnings {
			if w.Message == msg {
				dup = true
				break
			}
		}
		if !dup {
			t.warn(t.headerLine, msg)
		}
	}
}

// Col returns the index of a header column (case-insensitive) or -1.
func (t *Table) Col(name string) int {
	name = strings.ToLower(strings.TrimSpace(name))
	for i, h := range t.Header {
		if h == name {
			return i
		}
	}
	return -1
}

// Str returns the trimmed cell, or "" when the column does not exist.
func (t *Table) Str(r Row, col string) string {
	i := t.Col(col)
	if i < 0 || i >= len(r.Fields) {
		return ""
	}
	return strings.TrimSpace(r.Fields[i])
}

func (t *Table) fieldErr(r Row, col, msg string) *FieldError {
	return &FieldError{File: t.File, Line: r.Line, Column: strings.ToLower(strings.TrimSpace(col)), Message: msg}
}

// Float parses a finite number. An empty cell (or unknown column) is not
// present. With ',' as separator a decimal comma is an error; with ';' it is
// accepted.
func (t *Table) Float(r Row, col string) (v float64, present bool, err *FieldError) {
	s := t.Str(r, col)
	if s == "" {
		return 0, false, nil
	}
	orig := s
	if strings.Contains(s, ",") {
		if !t.semicolon {
			return 0, false, t.fieldErr(r, col, fmt.Sprintf("número %s inválido: use ponto decimal", shorten(orig)))
		}
		if strings.Contains(s, ".") {
			return 0, false, t.fieldErr(r, col, fmt.Sprintf("número %s inválido: não use separador de milhar", shorten(orig)))
		}
		s = strings.Replace(s, ",", ".", 1)
	}
	if strings.IndexFunc(s, func(c rune) bool { return !strings.ContainsRune("0123456789+-.eE", c) }) >= 0 {
		return 0, false, t.fieldErr(r, col, fmt.Sprintf("%s não é um número", shorten(orig)))
	}
	f, perr := strconv.ParseFloat(s, 64)
	if perr != nil {
		if errors.Is(perr, strconv.ErrRange) {
			return 0, false, t.fieldErr(r, col, fmt.Sprintf("número %s fora do intervalo", shorten(orig)))
		}
		return 0, false, t.fieldErr(r, col, fmt.Sprintf("%s não é um número", shorten(orig)))
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false, t.fieldErr(r, col, fmt.Sprintf("número %s fora do intervalo", shorten(orig)))
	}
	return f, true, nil
}

var timeLayouts = []string{
	"2006-1-2 15:04",
	"2006-1-2T15:04",
	"2006-1-2 15:04:05",
	"2006-1-2T15:04:05",
	"2/1/2006 15:04",
	"2/1/2006 15:04:05",
}

// Time parses a local date-time without zone; it is read as UTC with the
// digits as written (only differences matter). An empty cell is not present.
func (t *Table) Time(r Row, col string) (tm time.Time, present bool, err *FieldError) {
	s := t.Str(r, col)
	if s == "" {
		return time.Time{}, false, nil
	}
	for _, layout := range timeLayouts {
		if p, perr := time.ParseInLocation(layout, s, time.UTC); perr == nil {
			return p, true, nil
		}
	}
	return time.Time{}, false, t.fieldErr(r, col, fmt.Sprintf("data/hora %s inválida: use AAAA-MM-DD HH:MM", shorten(s)))
}

// shorten quotes a cell value for messages, cutting very long ones.
func shorten(s string) string {
	if rs := []rune(s); len(rs) > 40 {
		s = string(rs[:40]) + "…"
	}
	return strconv.Quote(s)
}
