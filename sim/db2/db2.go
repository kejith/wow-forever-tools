// Package db2 reads the DB2 tables that the Python side downloads from
// wago.tools as CSV into the shared cache directory (scripts/cache or
// WOW_TOOLS_CACHE). The engine never touches the network: a missing file
// is an error that tells the caller which table to download first.
package db2

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Table is one loaded CSV table: a header index and the raw rows.
type Table struct {
	Name string
	cols map[string]int
	rows [][]string
}

// Localized tables are cached per client locale (same list as icons.py).
var localized = map[string]bool{"ItemSparse": true, "SpellName": true}

// Path returns the cache file for a table, build and locale. The build is
// the full client version string, e.g. "1.60.1.70959" (the same naming
// scripts/icons.py uses).
func Path(cacheDir, name, build, locale string) string {
	if localized[name] && locale != "" {
		return filepath.Join(cacheDir, fmt.Sprintf("%s-%s-%s.csv", name, build, locale))
	}
	return filepath.Join(cacheDir, fmt.Sprintf("%s-%s.csv", name, build))
}

// Load reads a table from the cache. The error names the missing file so the
// Python side can download it and try again.
//
// A process that runs the engine more than once (the Go bridge does it in-process) reads each file once:
// loaded tables are kept, keyed by the file with its size and time, and a table is never changed after
// it is read.
func Load(cacheDir, name, build, locale string) (*Table, error) {
	path := Path(cacheDir, name, build, locale)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("table %s not in cache (%s): %w", name, path, err)
	}
	defer f.Close()
	key := loaded{path: path}
	if st, err := f.Stat(); err == nil {
		key.size, key.mtime = st.Size(), st.ModTime()
		memoMu.Lock()
		t := memo[key]
		memoMu.Unlock()
		if t != nil {
			return t, nil
		}
	}
	t, err := Read(name, f)
	if err == nil && !key.mtime.IsZero() {
		memoMu.Lock()
		memo[key] = t
		memoMu.Unlock()
	}
	return t, err
}

type loaded struct {
	path  string
	size  int64
	mtime time.Time
}

var (
	memoMu sync.Mutex
	memo   = map[loaded]*Table{}
)

// Read parses CSV content into a Table (exported for tests).
func Read(name string, r io.Reader) (*Table, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("table %s: %w", name, err)
	}
	cols := make(map[string]int, len(header))
	for i, h := range header {
		cols[h] = i
	}
	t := &Table{Name: name, cols: cols}
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("table %s: %w", name, err)
		}
		t.rows = append(t.rows, row)
	}
	return t, nil
}

// Len returns the number of rows.
func (t *Table) Len() int { return len(t.rows) }

// Has reports whether the table has a column.
func (t *Table) Has(col string) bool { _, ok := t.cols[col]; return ok }

// Row is one record with typed accessors. Missing columns and empty cells
// read as zero, like the Python scripts treat them.
type Row struct {
	t *Table
	i int
}

// Row returns the i-th row.
func (t *Table) Row(i int) Row { return Row{t, i} }

// Str returns a cell as string ("" when the column is missing).
func (r Row) Str(col string) string {
	c, ok := r.t.cols[col]
	if !ok || c >= len(r.t.rows[r.i]) {
		return ""
	}
	return r.t.rows[r.i][c]
}

// Int returns a cell as int (0 when missing or not a number).
func (r Row) Int(col string) int {
	v, _ := strconv.Atoi(r.Str(col))
	return v
}

// Float returns a cell as float64 (0 when missing or not a number).
func (r Row) Float(col string) float64 {
	v, _ := strconv.ParseFloat(r.Str(col), 64)
	return v
}

// Uint32 returns a cell as uint32, accepting the signed spellings wago uses.
func (r Row) Uint32(col string) uint32 {
	s := r.Str(col)
	if s == "" {
		return 0
	}
	if u, err := strconv.ParseUint(s, 10, 64); err == nil {
		return uint32(u)
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return uint32(i)
	}
	return 0
}
