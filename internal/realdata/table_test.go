package realdata

import (
	"math/rand"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mustTable(t *testing.T, data string) *Table {
	t.Helper()
	tb, err := ReadTable("x.csv", []byte(data))
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	return tb
}

func TestReadTableSeparatorsEquivalent(t *testing.T) {
	comma := mustTable(t, "id,cap,nome\nb1,300,\"Ônibus, 1\"\nb2,250.5,x\n")
	semi := mustTable(t, "id;cap;nome\nb1;300;\"Ônibus, 1\"\nb2;250,5;x\n")
	if !reflect.DeepEqual(comma.Header, semi.Header) {
		t.Fatalf("headers differ: %v vs %v", comma.Header, semi.Header)
	}
	if len(comma.Rows) != 2 || len(semi.Rows) != 2 {
		t.Fatalf("rows: %d vs %d", len(comma.Rows), len(semi.Rows))
	}
	if got := comma.Str(comma.Rows[0], "nome"); got != "Ônibus, 1" {
		t.Errorf("quoted field = %q", got)
	}
	if got := semi.Str(semi.Rows[0], "nome"); got != "Ônibus, 1" {
		t.Errorf("quoted field (;) = %q", got)
	}
	v, ok, err := semi.Float(semi.Rows[1], "cap")
	if err != nil || !ok || v != 250.5 {
		t.Errorf("semi Float(12,5 style) = %v %v %v", v, ok, err)
	}
	v, ok, err = comma.Float(comma.Rows[1], "cap")
	if err != nil || !ok || v != 250.5 {
		t.Errorf("comma Float = %v %v %v", v, ok, err)
	}
}

func TestFloatCommaDecimalWithCommaSeparator(t *testing.T) {
	tb := mustTable(t, "id,cap\nb1,\"12,5\"\n")
	_, _, err := tb.Float(tb.Rows[0], "cap")
	if err == nil {
		t.Fatal("expected error for 12,5 with comma separator")
	}
	if !strings.Contains(err.Message, "use ponto decimal") {
		t.Errorf("message = %q", err.Message)
	}
	if err.Column != "cap" || err.Line != 2 || err.File != "x.csv" {
		t.Errorf("err = %+v", err)
	}
}

func TestReadTableEncodingAndLines(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantLines []int
		wantHdr   []string
	}{
		{"bom", "\xEF\xBB\xBFid,cap\nb1,1\n", []int{2}, []string{"id", "cap"}},
		{"crlf", "id,cap\r\nb1,1\r\nb2,2\r\n", []int{2, 3}, []string{"id", "cap"}},
		{"lf", "id,cap\nb1,1\nb2,2\n", []int{2, 3}, []string{"id", "cap"}},
		{"blank middle and end", "id,cap\n\nb1,1\n\n\nb2,2\n\n\n", []int{3, 6}, []string{"id", "cap"}},
		{"blank first line", "\n\nid,cap\nb1,1\n", []int{4}, []string{"id", "cap"}},
		{"separator-only rows", "id;cap\nb1;1\n;;\n  ;  \nb2;2\n", []int{2, 5}, []string{"id", "cap"}},
		{"trim and lower header", "  ID , Cap \n b1 , 1 \n", []int{2}, []string{"id", "cap"}},
		{"quoted newline shifts lines", "id,nome\nb1,\"a\nb\"\nb2,x\n", []int{2, 4}, []string{"id", "nome"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tb := mustTable(t, c.in)
			if !reflect.DeepEqual(tb.Header, c.wantHdr) {
				t.Errorf("header = %v, want %v", tb.Header, c.wantHdr)
			}
			var lines []int
			for _, r := range tb.Rows {
				lines = append(lines, r.Line)
				if len(r.Fields) != len(tb.Header) {
					t.Errorf("row %d has %d fields", r.Line, len(r.Fields))
				}
			}
			if !reflect.DeepEqual(lines, c.wantLines) {
				t.Errorf("lines = %v, want %v", lines, c.wantLines)
			}
		})
	}
	tb := mustTable(t, "  ID , Cap \n b1 , 1 \n")
	if got := tb.Str(tb.Rows[0], "id"); got != "b1" {
		t.Errorf("trim field = %q", got)
	}
	if got := tb.Str(tb.Rows[0], "cap"); got != "1" {
		t.Errorf("trim field = %q", got)
	}
}

func TestReadTableWindows1252(t *testing.T) {
	tb := mustTable(t, "id,descri\xE7\xE3o\nb1,\x80 \x93ok\x94\n")
	if tb.Header[1] != "descrição" {
		t.Errorf("header = %q", tb.Header[1])
	}
	if got := tb.Str(tb.Rows[0], "descrição"); got != "€ “ok”" {
		t.Errorf("field = %q", got)
	}
	want := Warning{File: "x.csv", Message: "arquivo não está em UTF-8; lido como Windows-1252"}
	found := false
	for _, w := range tb.Warnings {
		if w == want {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %+v, want %+v", tb.Warnings, want)
	}
	utf := mustTable(t, "id\nÔnibus\n")
	if len(utf.Warnings) != 0 {
		t.Errorf("valid UTF-8 should not warn: %+v", utf.Warnings)
	}
}

func TestReadTableFileErrors(t *testing.T) {
	big := "id\n" + strings.Repeat("a", maxFileBytes)
	cases := []struct {
		name, in, msg string
		line          int
	}{
		{"empty", "", "arquivo vazio", 0},
		{"only bom", "\xEF\xBB\xBF", "arquivo vazio", 0},
		{"only blanks", "\n\r\n  \n", "arquivo vazio", 0},
		{"too big", big, "8 MB", 0},
		{"unclosed quote", "id,nome\nb1,\"abc\nb2,x\n", "", 2},
		{"bare quote", "id,nome\nb1,ab\"c\n", "", 2},
		{"too many fields", "id,cap\nb1,1\nb2,2,3\n", "campos", 3},
		{"duplicate column", "id,id\nb1,b2\n", "duplicada", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tb, err := ReadTable("x.csv", []byte(c.in))
			if err == nil {
				t.Fatalf("expected error, got table %+v", tb)
			}
			fe, ok := err.(*FieldError)
			if !ok {
				t.Fatalf("error type %T", err)
			}
			if fe.File != "x.csv" || fe.Line != c.line {
				t.Errorf("err = %+v, want line %d", fe, c.line)
			}
			if c.msg != "" && !strings.Contains(fe.Message, c.msg) {
				t.Errorf("message = %q, want contain %q", fe.Message, c.msg)
			}
			if fe.Message == "" {
				t.Error("empty message")
			}
		})
	}
}

func TestReadTableHeaderOnly(t *testing.T) {
	tb := mustTable(t, "id,cap\n")
	if len(tb.Rows) != 0 {
		t.Fatalf("rows = %d", len(tb.Rows))
	}
	if len(tb.Warnings) != 1 || tb.Warnings[0].Message != "sem linhas de dados" || tb.Warnings[0].File != "x.csv" {
		t.Errorf("warnings = %+v", tb.Warnings)
	}
}

func TestReadTableShortRowPadded(t *testing.T) {
	tb := mustTable(t, "id,cap,nome\nb1,5\nb2\n")
	if len(tb.Rows) != 2 {
		t.Fatalf("rows = %d", len(tb.Rows))
	}
	if !reflect.DeepEqual(tb.Rows[0].Fields, []string{"b1", "5", ""}) {
		t.Errorf("fields = %q", tb.Rows[0].Fields)
	}
	if !reflect.DeepEqual(tb.Rows[1].Fields, []string{"b2", "", ""}) {
		t.Errorf("fields = %q", tb.Rows[1].Fields)
	}
}

func TestReadTableTrailingEmptyFieldsTolerated(t *testing.T) {
	tb := mustTable(t, "id;cap\nb1;1;;\n")
	if len(tb.Rows) != 1 || !reflect.DeepEqual(tb.Rows[0].Fields, []string{"b1", "1"}) {
		t.Errorf("rows = %+v", tb.Rows)
	}
}

func TestFloat(t *testing.T) {
	tb := mustTable(t, "id,v\na,\nb,abc\nc,NaN\nd,Inf\ne,1e999\nf,1.5\ng,-3\nh,1e2\ni,-inf\nj,0x10\nk, 7 \n")
	row := func(id string) Row {
		for _, r := range tb.Rows {
			if tb.Str(r, "id") == id {
				return r
			}
		}
		t.Fatalf("no row %s", id)
		return Row{}
	}
	if _, ok, err := tb.Float(row("a"), "v"); ok || err != nil {
		t.Errorf("empty: ok=%v err=%v", ok, err)
	}
	for _, id := range []string{"b", "c", "d", "e", "i", "j"} {
		_, ok, err := tb.Float(row(id), "v")
		if err == nil || ok {
			t.Errorf("row %s: expected error", id)
			continue
		}
		if err.Column != "v" || err.Line != row(id).Line || err.File != "x.csv" {
			t.Errorf("row %s: err = %+v", id, err)
		}
	}
	for id, want := range map[string]float64{"f": 1.5, "g": -3, "h": 100, "k": 7} {
		v, ok, err := tb.Float(row(id), "v")
		if err != nil || !ok || v != want {
			t.Errorf("row %s: %v %v %v", id, v, ok, err)
		}
	}
	// Unknown column behaves like an empty cell.
	if _, ok, err := tb.Float(row("f"), "nada"); ok || err != nil {
		t.Errorf("unknown col: ok=%v err=%v", ok, err)
	}
	if tb.Col("nada") != -1 || tb.Col("v") != 1 || tb.Col("V") != 1 {
		t.Errorf("Col: %d %d %d", tb.Col("nada"), tb.Col("v"), tb.Col("V"))
	}
	if tb.Str(row("f"), "nada") != "" {
		t.Error("Str of unknown column should be empty")
	}
}

func TestTime(t *testing.T) {
	tb := mustTable(t, strings.Join([]string{
		"id;t",
		"a;2026-03-04 21:10",
		"b;2026-03-04T21:10",
		"c;2026-03-04 21:10:00",
		"d;04/03/2026 21:10",
		"e;2026-13-40 25:61",
		"f;",
		"g;ontem",
		"h;2026-03-04T21:10:30",
		"i;04/03/2026 21:10:30",
		"j;2026-02-30 10:00",
	}, "\n"))
	want := time.Date(2026, 3, 4, 21, 10, 0, 0, time.UTC)
	byID := map[string]Row{}
	for _, r := range tb.Rows {
		byID[tb.Str(r, "id")] = r
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		tm, ok, err := tb.Time(byID[id], "t")
		if err != nil || !ok || !tm.Equal(want) || tm.Location() != time.UTC {
			t.Errorf("%s: %v %v %v", id, tm, ok, err)
		}
	}
	for _, id := range []string{"h", "i"} {
		tm, ok, err := tb.Time(byID[id], "t")
		if err != nil || !ok || !tm.Equal(want.Add(30*time.Second)) {
			t.Errorf("%s: %v %v %v", id, tm, ok, err)
		}
	}
	for _, id := range []string{"e", "g", "j"} {
		_, ok, err := tb.Time(byID[id], "t")
		if err == nil || ok {
			t.Errorf("%s: expected error", id)
			continue
		}
		if err.Column != "t" || err.Line != byID[id].Line {
			t.Errorf("%s: err = %+v", id, err)
		}
	}
	if _, ok, err := tb.Time(byID["f"], "t"); ok || err != nil {
		t.Errorf("empty: ok=%v err=%v", ok, err)
	}
}

func TestKnownWarnsOncePerColumn(t *testing.T) {
	tb := mustTable(t, "id,cap,xyz\nb1,1,9\nb2,2,8\n")
	tb.Known("id", "CAP")
	tb.Known("id", "cap")
	n := 0
	for _, w := range tb.Warnings {
		if w.Message == "coluna 'xyz' ignorada" {
			n++
			if w.File != "x.csv" || w.Line != 1 {
				t.Errorf("warning = %+v", w)
			}
		}
	}
	if n != 1 {
		t.Errorf("got %d warnings for xyz, want 1: %+v", n, tb.Warnings)
	}
	if len(tb.Warnings) != 1 {
		t.Errorf("extra warnings: %+v", tb.Warnings)
	}
}

func TestFieldErrorString(t *testing.T) {
	cases := []struct {
		e    FieldError
		want string
	}{
		{FieldError{File: "arquivo.csv", Line: 7, Column: "soc_chegada_pct", Message: "mensagem"}, "arquivo.csv, linha 7, coluna soc_chegada_pct: mensagem"},
		{FieldError{File: "arquivo.csv", Line: 7, Message: "mensagem"}, "arquivo.csv, linha 7: mensagem"},
		{FieldError{File: "arquivo.csv", Message: "mensagem"}, "arquivo.csv: mensagem"},
		{FieldError{Message: "mensagem"}, "mensagem"},
		{FieldError{File: "a.csv", Column: "c", Message: "m"}, "a.csv, coluna c: m"},
	}
	for _, c := range cases {
		e := c.e
		if got := e.Error(); got != c.want {
			t.Errorf("Error() = %q, want %q", got, c.want)
		}
	}
}

func TestReadTableRandomBytesNeverPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabet := []byte("abc,;\"\r\n \t0123456789.-eE:/T\xEF\xBB\xBF\xE7\xFF")
	for i := 0; i < 200; i++ {
		n := rng.Intn(200)
		buf := make([]byte, n)
		for j := range buf {
			if rng.Intn(4) == 0 {
				buf[j] = byte(rng.Intn(256))
			} else {
				buf[j] = alphabet[rng.Intn(len(alphabet))]
			}
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on input %q: %v", buf, r)
				}
			}()
			tb, err := ReadTable("x.csv", buf)
			if err != nil {
				if _, ok := err.(*FieldError); !ok {
					t.Fatalf("error type %T", err)
				}
				return
			}
			tb.Known("id")
			for _, r := range tb.Rows {
				for _, h := range tb.Header {
					tb.Float(r, h)
					tb.Time(r, h)
					tb.Str(r, h)
				}
			}
		}()
	}
}

func FuzzReadTable(f *testing.F) {
	f.Add([]byte("id,cap\nb1,1\n"))
	f.Add([]byte("id;cap\r\nb1;1,5\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		tb, err := ReadTable("x.csv", data)
		if err != nil {
			return
		}
		for _, r := range tb.Rows {
			for _, h := range tb.Header {
				tb.Float(r, h)
				tb.Time(r, h)
			}
		}
	})
}

func header64() string {
	cols := make([]string, 64)
	for i := range cols {
		cols[i] = "c" + strconv.Itoa(i)
	}
	return strings.Join(cols, ",") + "\n"
}

func allocDelta(f func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	f()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

func wantFieldError(t *testing.T, err error, msg string) *FieldError {
	t.Helper()
	fe, ok := err.(*FieldError)
	if !ok {
		t.Fatalf("expected *FieldError, got %T (%v)", err, err)
	}
	if !strings.Contains(fe.Message, msg) {
		t.Errorf("message = %q, want contain %q", fe.Message, msg)
	}
	return fe
}

func TestReadTableHostileShapesBoundedMemory(t *testing.T) {
	const limit = 50 << 20
	wide := strings.Repeat("c,", 19999) + "c\n" + strings.Repeat("x\n", 500)
	empties := strings.Repeat(",", maxFileBytes-16) + "\nx\n"
	wideRow := "a,b\n" + strings.Repeat(",", 100000) + "\n"
	many := header64() + strings.Repeat("x\n", 40000)
	cases := []struct {
		name, in, msg string
		line          int
	}{
		{"20000 columns", wide, "colunas demais (máximo 64)", 1},
		{"8 MB of empty columns", empties, "colunas demais (máximo 64)", 1},
		{"65 columns", strings.TrimSuffix(header64(), "\n") + ",extra\n", "colunas demais (máximo 64)", 1},
		{"data row too wide", wideRow, "campos demais (máximo 64)", 2},
		{"too many cells", many, "máximo de 2 milhões de células", 31252},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var err error
			start := time.Now()
			alloc := allocDelta(func() { _, err = ReadTable("x.csv", []byte(c.in)) })
			if err == nil {
				t.Fatal("expected error")
			}
			fe := wantFieldError(t, err, c.msg)
			if fe.Line != c.line || fe.File != "x.csv" {
				t.Errorf("err = %+v, want line %d", fe, c.line)
			}
			if alloc > limit {
				t.Errorf("allocated %d MB, want < 50", alloc>>20)
			}
			if d := time.Since(start); d > 5*time.Second {
				t.Errorf("took %v", d)
			}
		})
	}
}

func TestReadTableCellCapBoundary(t *testing.T) {
	header := header64()
	ok := header + strings.Repeat("x\n", 31250) // 64 x 31250 = 2,000,000 cells
	tb, err := ReadTable("x.csv", []byte(ok))
	if err != nil {
		t.Fatalf("under the cap: %v", err)
	}
	if len(tb.Rows) != 31250 || len(tb.Rows[0].Fields) != 64 {
		t.Fatalf("rows = %d, fields = %d", len(tb.Rows), len(tb.Rows[0].Fields))
	}
	_, err = ReadTable("x.csv", []byte(header+strings.Repeat("x\n", 31251)))
	wantFieldError(t, err, "células")
}

func TestTimeNoLeadingZeros(t *testing.T) {
	tb := mustTable(t, "id;t\na;4/3/2026 9:10\nb;2026-3-4 21:10\nc;2/1/2006 15:04\nd;2026-3-4T9:05:07\nf;31/2/2026 10:00\ng;2026-2-30 1:00\nh;2026-3-4 24:00\n")
	want := map[string]time.Time{
		"a": time.Date(2026, 3, 4, 9, 10, 0, 0, time.UTC),
		"b": time.Date(2026, 3, 4, 21, 10, 0, 0, time.UTC),
		"c": time.Date(2006, 1, 2, 15, 4, 0, 0, time.UTC),
		"d": time.Date(2026, 3, 4, 9, 5, 7, 0, time.UTC),
	}
	for _, r := range tb.Rows {
		id := tb.Str(r, "id")
		tm, ok, err := tb.Time(r, "t")
		if w, good := want[id]; good {
			if err != nil || !ok || !tm.Equal(w) {
				t.Errorf("%s: %v %v %v", id, tm, ok, err)
			}
			continue
		}
		if err == nil || ok {
			t.Errorf("%s: expected error, got %v", id, tm)
		}
	}
}

func TestFloatErrorQuotesOriginalCell(t *testing.T) {
	tb := mustTable(t, "id;v\na;\"12,5,3\"\nb;1,2x\n")
	_, _, err := tb.Float(tb.Rows[0], "v")
	if err == nil || !strings.Contains(err.Message, `"12,5,3"`) {
		t.Errorf("err = %+v", err)
	}
	_, _, err = tb.Float(tb.Rows[1], "v")
	if err == nil || !strings.Contains(err.Message, `"1,2x"`) {
		t.Errorf("err = %+v", err)
	}
}

func TestReadTableUTF16Rejected(t *testing.T) {
	for _, bom := range []string{"\xFF\xFE", "\xFE\xFF"} {
		_, err := ReadTable("x.csv", []byte(bom+"i\x00d\x00\n\x00"))
		fe := wantFieldError(t, err, "UTF-16 não é suportado")
		if fe.Line != 0 || !strings.Contains(fe.Message, "UTF-8") {
			t.Errorf("err = %+v", fe)
		}
	}
}

func TestExcelBrasilEqualsPlain(t *testing.T) {
	br := mustTable(t, "\xEF\xBB\xBFid;cap;nome\r\nb1;12,5;a\xE7\xE3o\r\nb2;3;x\r\n\r\n;;\r\n\r\n")
	plain := mustTable(t, "id,cap,nome\nb1,12.5,ação\nb2,3,x\n")
	if !reflect.DeepEqual(br.Header, plain.Header) {
		t.Fatalf("headers: %v vs %v", br.Header, plain.Header)
	}
	if len(br.Rows) != len(plain.Rows) {
		t.Fatalf("rows: %d vs %d", len(br.Rows), len(plain.Rows))
	}
	for i := range br.Rows {
		if br.Rows[i].Line != plain.Rows[i].Line {
			t.Errorf("row %d line %d vs %d", i, br.Rows[i].Line, plain.Rows[i].Line)
		}
		for _, c := range []string{"id", "nome"} {
			if br.Str(br.Rows[i], c) != plain.Str(plain.Rows[i], c) {
				t.Errorf("row %d %s: %q vs %q", i, c, br.Str(br.Rows[i], c), plain.Str(plain.Rows[i], c))
			}
		}
		a, ao, aerr := br.Float(br.Rows[i], "cap")
		b, bo, berr := plain.Float(plain.Rows[i], "cap")
		if a != b || ao != bo || aerr != nil || berr != nil {
			t.Errorf("row %d cap: %v %v %v vs %v %v %v", i, a, ao, aerr, b, bo, berr)
		}
	}
}

func TestReadTableOddShapes(t *testing.T) {
	// NUL bytes do not panic.
	tb := mustTable(t, "id,v\na,\x00b\n\x00,1\n")
	for _, r := range tb.Rows {
		tb.Float(r, "v")
		tb.Time(r, "v")
	}
	// ';' inside quotes with ',' delimiter stays in the field.
	tb = mustTable(t, "id,nome\nb1,\"a;b;c;d;e\"\n")
	if tb.Str(tb.Rows[0], "nome") != "a;b;c;d;e" || len(tb.Header) != 2 {
		t.Errorf("header %v rows %+v", tb.Header, tb.Rows)
	}
	// Quoted comma in the header.
	tb = mustTable(t, "\"id, x\";cap\nb1;2\n")
	if !reflect.DeepEqual(tb.Header, []string{"id, x", "cap"}) {
		t.Errorf("header = %q", tb.Header)
	}
	// Single column.
	tb = mustTable(t, "id\nb1\nb2\n")
	if len(tb.Header) != 1 || len(tb.Rows) != 2 || tb.Rows[1].Fields[0] != "b2" || tb.Rows[1].Line != 3 {
		t.Errorf("single column: %+v", tb)
	}
}
