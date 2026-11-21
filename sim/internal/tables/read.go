package tables

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"sync"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/pycsv"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
)

// Table is a DB2 table as icons.table returns it: the rows of csv.DictReader. A row shorter than the
// header has no value for the missing columns (Python's None); a header named twice keeps the last column.
type Table struct {
	Header []string
	index  map[string]int
	rows   [][]string
}

// Row is one row of a table.
type Row struct {
	t *Table
	f []string
}

// Len is the number of rows.
func (t *Table) Len() int {
	if t == nil {
		return 0
	}
	return len(t.rows)
}

// Rows are the rows in file order.
func (t *Table) Rows() []Row {
	if t == nil {
		return nil
	}
	out := make([]Row, len(t.rows))
	for i, f := range t.rows {
		out[i] = Row{t, f}
	}
	return out
}

// Get is r.get(col): the value and whether the row has one (false for an unknown column or a short row).
func (r Row) Get(col string) (string, bool) {
	i, ok := r.t.index[col]
	if !ok || i >= len(r.f) {
		return "", false
	}
	return r.f[i], true
}

// Str is r.get(col) or "".
func (r Row) Str(col string) string { s, _ := r.Get(col); return s }

// Has tells whether the table has a column.
func (t *Table) Has(col string) bool {
	if t == nil {
		return false
	}
	_, ok := t.index[col]
	return ok
}

var buildNumber = regexp.MustCompile(`\.(\d+)(?:-[A-Za-z]{4})?\.csv$`)

// BuildNumber is icons._build_number: the build of a cached table's file name, 0 without one.
func BuildNumber(path string) int64 {
	m := buildNumber.FindStringSubmatch(path)
	if m == nil {
		return 0
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	return n
}

// Parse reads CSV text as csv.DictReader does.
func Parse(text string) *Table {
	all := pycsv.ReadAll(text)
	t := &Table{index: map[string]int{}}
	if len(all) == 0 {
		return t
	}
	// DictReader takes the first row as the header, even an empty one, and skips empty rows after it.
	start := 0
	t.Header = all[start]
	for i, h := range t.Header {
		t.index[h] = i
	}
	for _, row := range all[start+1:] {
		if len(row) > 0 {
			t.rows = append(t.rows, row)
		}
	}
	return t
}

var (
	readMu sync.Mutex
	read   = map[string]*Table{}
)

// Read is icons.table: a DB2 table of a build, downloaded into the cache unless it is there; without a build,
// or when the download fails, the newest cached copy of any build. Tables with text are per locale. Nil when
// there is none at all. A table read once is kept for the rest of the process.
func Read(cache, name, build, locale string) (*Table, error) {
	suffix := ""
	if Localized[name] {
		if locale == "" {
			locale = DefaultLocale
		}
		suffix = "-" + locale
	}
	_ = os.MkdirAll(cache, 0o755)
	path := ""
	if build != "" {
		path = Path(cache, name, build, locale)
		Ensure(cache, name, build, locale)
	}
	if _, err := os.Stat(path); path == "" || err != nil {
		cached := game.Glob(cache, name+"-*"+suffix+".csv")
		if len(cached) == 0 {
			return nil, nil
		}
		sort.SliceStable(cached, func(i, j int) bool { return BuildNumber(cached[i]) < BuildNumber(cached[j]) })
		path = cached[len(cached)-1]
	}
	readMu.Lock()
	t := read[path]
	readMu.Unlock()
	if t != nil {
		return t, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t = Parse(string(data))
	readMu.Lock()
	read[path] = t
	readMu.Unlock()
	return t, nil
}

// ClientBuild is icons.client_build: the newest client build of the character snapshots, e.g.
// "1.60.1.70938"; "" without one.
func ClientBuild(wow string) string {
	best, bestNum := "", int64(0)
	for _, path := range game.FindChars(wow, "") {
		c, err := sv.LoadTable(path, "WoWMCP_Char")
		if err != nil {
			continue
		}
		cl := c.Tab("client")
		if !sv.Truthy(cl.Get("version")) || !sv.Truthy(cl.Get("build")) {
			continue
		}
		n, ok := sv.Num(cl.Get("build"))
		if text, isText := cl.Get("build").(string); isText {
			i, parsed := pyfmt.ParseInt(text)
			n, ok = float64(i), parsed
		}
		if !ok {
			continue
		}
		b := strconv.FormatInt(int64(n), 10)
		if best == "" || int64(n) > bestNum {
			best, bestNum = sv.KeyString(cl.Get("version"))+"."+b, int64(n)
		}
	}
	return best
}
