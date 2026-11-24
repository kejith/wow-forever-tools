package sv_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// dumpScript prints every global a file sets the way the Python scripts read it (upgrades.load_sv), as JSON.
const dumpScript = `
import json, sys
import re
# load_sv as scripts/upgrades.py had it: the file in a sandboxed Lua 5.4, one global as Python data.
def lua_to_py(x):
    from lupa.lua54 import lua_type
    if lua_type(x) != "table":
        return x
    keys = list(x.keys())
    if keys and all(isinstance(k, int) for k in keys) and sorted(keys) == list(range(1, len(keys) + 1)):
        return [lua_to_py(x[k]) for k in sorted(keys)]
    return {k: lua_to_py(x[k]) for k in keys}
def load_sv(path, name):
    from lupa.lua54 import LuaRuntime
    lua = LuaRuntime(register_eval=False, register_builtins=False)
    for g in ("os", "io", "package", "require", "dofile", "loadfile", "debug"):
        lua.execute(f"{g} = nil")
    lua.execute(open(path, encoding="utf-8", errors="replace").read())
    return lua_to_py(lua.globals()[name])
src = open(sys.argv[1], encoding="utf-8", errors="replace").read()
names = re.findall(r"^([A-Za-z_][A-Za-z0-9_]*) = ", src, re.M)
print(json.dumps({n: load_sv(sys.argv[1], n) for n in names}, ensure_ascii=False))
`

func collision(f *sv.File) string {
	var walk func(path string, t *sv.Table) string
	walk = func(path string, t *sv.Table) string {
		seen := map[string]bool{}
		for i, k := range t.Keys() {
			s := sv.KeyString(k)
			if seen[s] {
				return path
			}
			seen[s] = true
			if sub, ok := t.Values()[i].(*sv.Table); ok {
				if w := walk(path+"."+s, sub); w != "" {
					return w
				}
			}
		}
		return ""
	}
	for _, name := range f.Names() {
		if t := f.Tab(name); t != nil {
			if w := walk(name, t); w != "" {
				return w
			}
		}
	}
	return ""
}

// TestAgainstLupa reads every SavedVariables file of the test folder (and of WOW_FOREVER_DIR, if set) with
// this package and with lupa, and compares the data (not the key order, which lupa changes from run to
// run). It needs uv and runs only with SV_LUPA=1:
//
//	SV_LUPA=1 go test ./internal/sv/ -run Lupa -v
func TestAgainstLupa(t *testing.T) {
	if os.Getenv("SV_LUPA") == "" {
		t.Skip("set SV_LUPA=1 to compare with lupa (needs uv)")
	}
	fixtures, _ := filepath.Abs(filepath.Join("..", "..", "..", "scripts", "fixtures"))
	roots := []string{filepath.Join(fixtures, "forever")}
	if w := os.Getenv("WOW_FOREVER_DIR"); w != "" {
		roots = append(roots, w)
	}
	script := filepath.Join(t.TempDir(), "dump.py")
	if err := os.WriteFile(script, []byte(dumpScript), 0o644); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, root := range roots {
		var paths []string
		for _, pat := range []string{"*/*/*/SavedVariables/*.lua", "*/SavedVariables/*.lua"} {
			m, _ := filepath.Glob(filepath.Join(root, "WTF", "Account", pat))
			paths = append(paths, m...)
		}
		for _, p := range paths {
			if filepath.Base(p) == "Auctionator.lua" || strings.HasSuffix(p, ".bak") {
				continue // the price database is a string; Prices reads it
			}
			out, err := exec.Command("uv", "run", "--quiet", "--with", "lupa", "python", script, p).Output()
			if err != nil {
				t.Errorf("%s: lupa: %v", p, err)
				continue
			}
			var want any
			if err := json.Unmarshal(out, &want); err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			f, err := sv.ReadFile(p)
			if err != nil {
				t.Errorf("%s: %v", p, err)
				continue
			}
			if where := collision(f); where != "" {
				// [168] and ["168"] in one table: both are "168" in JSON, and which one survives a round trip
				// depends on the key order. EllesmereUI writes such tables.
				t.Logf("%s: not compared, an integer and a string key with the same text at %s", p, where)
				continue
			}
			globals := pyjson.New()
			for _, name := range f.Names() {
				globals.Set(name, f.Get(name))
			}
			data, err := pyjson.Encode(globals)
			if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			var got any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: the data differs from lupa's (SV_LUPA_OUT=<folder> keeps both)", p)
				if dir := os.Getenv("SV_LUPA_OUT"); dir != "" {
					base := filepath.Join(dir, filepath.Base(p))
					_ = os.WriteFile(base+".go.json", data, 0o644)
					_ = os.WriteFile(base+".lupa.json", out, 0o644)
				}
			}
			n++
		}
	}
	t.Logf("%d files agree with lupa", n)
}
