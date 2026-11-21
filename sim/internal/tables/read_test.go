package tables

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseLikeDictReader(t *testing.T) {
	text := "ID,Name,Name,Extra\r\n1,a,b\r\n\r\n2,\"x,\"\"y\"\"\",c,d,e\r\n"
	tab := Parse(text)
	rows := tab.Rows()
	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2", len(rows))
	}
	if v := rows[0].Str("Name"); v != "b" {
		t.Errorf("the second Name of a short row: got %q", v)
	}
	if v, ok := rows[0].Get("Extra"); ok || v != "" {
		t.Errorf("a short row has no value for the missing columns: got %q %v", v, ok)
	}
	if v := rows[1].Str("Name"); v != "c" {
		t.Errorf("the last column of a doubled name wins: got %q", v)
	}
	if v := rows[1].Str("ID"); v != "2" {
		t.Errorf("ID = %q", v)
	}
	if _, ok := rows[0].Get("Missing"); ok {
		t.Error("an unknown column has no value")
	}
}

// TestTablesLikePython compares Parse with csv.DictReader on every table of the cache (TABLES_PYTHON=1, needs
// python3 and the cache of the repo or WOW_TOOLS_CACHE).
func TestTablesLikePython(t *testing.T) {
	if os.Getenv("TABLES_PYTHON") == "" {
		t.Skip("set TABLES_PYTHON=1")
	}
	cache := os.Getenv("WOW_TOOLS_CACHE")
	if cache == "" {
		cache = filepath.Join("..", "..", "..", "scripts", "cache")
	}
	files, _ := filepath.Glob(filepath.Join(cache, "*.csv"))
	script := `import csv, hashlib, sys
for path in sys.argv[1:]:
    h = hashlib.sha1()
    n = 0
    with open(path, encoding="utf-8", newline="") as f:
        for r in csv.DictReader(f):
            n += 1
            for k, v in r.items():
                if k is not None:
                    h.update(("%s=%s\x00" % (k, "" if v is None else v)).encode())
            h.update(b"\x01")
    print(n, h.hexdigest())`
	out, err := exec.Command("python3", append([]string{"-c", script}, files...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		tab := Parse(string(data))
		h := sha1.New()
		for _, r := range tab.Rows() {
			seen := map[string]bool{}
			for _, k := range tab.Header {
				if seen[k] {
					continue
				}
				seen[k] = true
				v, _ := r.Get(k)
				h.Write([]byte(k + "=" + v + "\x00"))
			}
			h.Write([]byte{1})
		}
		got := strconv.Itoa(tab.Len()) + " " + hex.EncodeToString(h.Sum(nil))
		if got != lines[i] {
			t.Errorf("%s: Go %s, Python %s", filepath.Base(path), got, lines[i])
		}
	}
	t.Logf("%d tables compared", len(files))
}
