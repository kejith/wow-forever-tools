package bridge

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
)

// TestGolden runs every bridge command the app uses, with the options the app passes, on a copy of the test
// folder (scripts/fixtures/forever) and compares the answer with the one the Python bridge gave for the same
// case. Python's answers were recorded in testdata/golden.json.gz before the scripts were removed (phase 5 of
// notes/go-migration.md); until then the parity check compared the two live. Numbers agree within a relative
// 1e-9, the key order of an object is not compared (Python's was random where its source was a Lua table),
// and paths are written as <wow> (the copy) and <cache> (WOW_TOOLS_CACHE). The recording also ran Python with
// its clock 40 days ahead; the paths that changed then ("time" per case) depend on the day and are left out.
// A case that says "recorded" is younger than the Python bridge (an option it never had): its answer is the
// one wowtools gave when the case was added.
func TestGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	cache := os.Getenv("WOW_TOOLS_CACHE")
	if cache == "" {
		cache = filepath.Join(root, "scripts", "cache")
		t.Setenv("WOW_TOOLS_CACHE", cache)
	}
	t.Setenv("WOW_FOREVER_DIR", "")
	t.Setenv("WOW_TOOLS_LANG", "")
	wow := filepath.Join(t.TempDir(), "_forever_")
	if err := copyFixture(filepath.Join(root, "scripts", "fixtures", "forever"), wow); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join("testdata", "golden.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Cases []struct {
			Name    string          `json:"name"`
			Command string          `json:"command"`
			Args    []string        `json:"args"`
			Lang    string          `json:"lang"`
			Time    []string        `json:"time"`
			Answer  json.RawMessage `json:"answer"`
		} `json:"cases"`
	}
	if err := json.NewDecoder(gz).Decode(&rec); err != nil {
		t.Fatal(err)
	}
	if len(rec.Cases) == 0 {
		t.Fatal("no recorded cases")
	}
	for _, c := range rec.Cases {
		t.Run(c.Name, func(t *testing.T) {
			args := append(append([]string{}, c.Args...), "--wow", wow, "--lang", c.Lang)
			r, err := Run(c.Command, args)
			if err != nil {
				t.Fatal(err)
			}
			data, err := pyjson.Encode(r)
			if err != nil {
				t.Fatal(err)
			}
			got, want := decodeJSON(t, data), decodeJSON(t, c.Answer)
			got = placeholders(got, wow, cache)
			skip := map[string]bool{"generated": true}
			for _, p := range c.Time {
				skip[p] = true
			}
			var diffs []string
			compareJSON("", want, got, skip, &diffs)
			for i, d := range diffs {
				if i == 8 {
					t.Errorf("… %d more", len(diffs)-i)
					break
				}
				t.Error(d)
			}
		})
	}
}

func decodeJSON(t *testing.T, data []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// placeholders writes the copy's folder and the cache as <wow> and <cache>, wherever a text names them, with
// forward slashes in that text, as the answers were recorded (under Linux).
func placeholders(v any, wow, cache string) any {
	switch x := v.(type) {
	case string:
		if !strings.Contains(x, wow) && !strings.Contains(x, cache) {
			return x
		}
		x = strings.ReplaceAll(strings.ReplaceAll(x, wow, "<wow>"), cache, "<cache>")
		return strings.ReplaceAll(x, string(filepath.Separator), "/") // an error text may name a path, too
	case map[string]any:
		for k, val := range x {
			x[k] = placeholders(val, wow, cache)
		}
	case []any:
		for i, val := range x {
			x[i] = placeholders(val, wow, cache)
		}
	}
	return v
}

func compareJSON(path string, want, got any, skip map[string]bool, out *[]string) {
	if skip[path] {
		return
	}
	at := func(k string) string {
		if path == "" {
			return k
		}
		return path + "." + k
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: python an object, go %T", path, got))
			return
		}
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if gv, ok := g[k]; ok {
				compareJSON(at(k), w[k], gv, skip, out)
			} else if !skip[at(k)] {
				*out = append(*out, fmt.Sprintf("%s: missing in go", at(k)))
			}
		}
		for k := range g {
			if _, ok := w[k]; !ok && !skip[at(k)] {
				*out = append(*out, fmt.Sprintf("%s: only in go", at(k)))
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: python a list, go %T", path, got))
			return
		}
		if len(w) != len(g) {
			*out = append(*out, fmt.Sprintf("%s: python %d items, go %d", path, len(w), len(g)))
		}
		for i := 0; i < len(w) && i < len(g); i++ {
			compareJSON(fmt.Sprintf("%s[%d]", path, i), w[i], g[i], skip, out)
		}
	case json.Number:
		g, ok := got.(json.Number)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: python %s, go %v", path, w, got))
			return
		}
		a, _ := w.Float64()
		b, _ := g.Float64()
		if math.Abs(a-b) > 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b))) {
			*out = append(*out, fmt.Sprintf("%s: python %s, go %s", path, w, g))
		}
	default:
		if want != got {
			*out = append(*out, fmt.Sprintf("%s: python %#v, go %#v", path, want, got))
		}
	}
}

// copyFixture copies the test folder and gives its files the times the recording gave them: the readers sort
// characters by the time their snapshot was written, and git does not keep file times. Character folders in
// alphabetical order get older and older, everything else is older than all of them.
func copyFixture(src, dst string) error {
	base := time.Unix(1795315076, 0) // the time the answers were recorded with; they carry file times
	var chars []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		if d.Name() == "WoWMCP.lua" && filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path)))) != "Account" {
			chars = append(chars, target)
		}
		return os.Chtimes(target, base, base)
	})
	if err != nil {
		return err
	}
	sort.Strings(chars)
	for i, path := range chars {
		t := base.Add(time.Duration(len(chars)-i) * 10 * time.Minute)
		if err := os.Chtimes(path, t, t); err != nil {
			return err
		}
	}
	return nil
}
