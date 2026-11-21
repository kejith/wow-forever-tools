package sv_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kejith/wow-forever-tools/sim/internal/sv"
)

// orderScript prints, for every table of a file that load_sv turns into a dict without a string key, the
// order of its keys: that order is Lua's and the same in every run.
const orderScript = `
import json, sys
sys.path.insert(0, sys.argv[1])
from upgrades import load_sv
import re
src = open(sys.argv[2], encoding="utf-8", errors="replace").read()
out = {}
def walk(x, path):
    if isinstance(x, dict):
        if x and not any(isinstance(k, str) for k in x):
            out[path] = [str(k) for k in x]
        for k, v in x.items():
            walk(v, path + "/" + str(k))
    elif isinstance(x, list):
        for i, v in enumerate(x):
            walk(v, path + "/" + str(i + 1))
for n in re.findall(r"^([A-Za-z_][A-Za-z0-9_]*) = ", src, re.M):
    walk(load_sv(sys.argv[2], n), n)
print(json.dumps(out))
`

func walkOrder(x any, path string, out map[string][]string) {
	t, ok := x.(*sv.Table)
	if !ok {
		return
	}
	if t.IsList() {
		for i, v := range t.List() {
			walkOrder(v, path+"/"+sv.KeyString(int64(i+1)), out)
		}
		return
	}
	strKey := false
	var keys []string
	for _, k := range t.Keys() {
		if _, isStr := k.(string); isStr {
			strKey = true
		}
		keys = append(keys, sv.KeyString(k))
	}
	if len(keys) > 0 && !strKey {
		out[path] = keys
	}
	for i, k := range t.Keys() {
		walkOrder(t.Values()[i], path+"/"+sv.KeyString(k), out)
	}
}

// TestKeyOrderAgainstLupa checks that a table without string keys iterates in Lua's order (luaOrder): the
// scripts follow it, and ties in their sorts keep it. Runs with SV_LUPA=1 (needs uv); WOW_FOREVER_DIR adds a
// real game folder.
func TestKeyOrderAgainstLupa(t *testing.T) {
	if os.Getenv("SV_LUPA") == "" {
		t.Skip("set SV_LUPA=1 to compare with lupa (needs uv)")
	}
	scripts, _ := filepath.Abs(filepath.Join("..", "..", "..", "scripts"))
	roots := []string{filepath.Join(scripts, "fixtures", "forever")}
	if w := os.Getenv("WOW_FOREVER_DIR"); w != "" {
		roots = append(roots, w)
	}
	script := filepath.Join(t.TempDir(), "order.py")
	if err := os.WriteFile(script, []byte(orderScript), 0o644); err != nil {
		t.Fatal(err)
	}
	tables := 0
	for _, root := range roots {
		var paths []string
		for _, pat := range []string{"*/*/*/SavedVariables/*.lua", "*/SavedVariables/*.lua"} {
			m, _ := filepath.Glob(filepath.Join(root, "WTF", "Account", pat))
			paths = append(paths, m...)
		}
		for _, p := range paths {
			if filepath.Base(p) == "Auctionator.lua" || strings.HasSuffix(p, ".bak") {
				continue
			}
			raw, err := exec.Command("uv", "run", "--quiet", "--with", "lupa", "python", script, scripts, p).Output()
			if err != nil {
				t.Errorf("%s: lupa: %v", filepath.Base(p), err)
				continue
			}
			var want map[string][]string
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			f, err := sv.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if where := collision(f); where != "" {
				continue // [168] and ["168"] in one table: one path for two tables (see TestAgainstLupa)
			}
			got := map[string][]string{}
			for _, name := range f.Names() {
				walkOrder(f.Get(name), name, got)
			}
			bad := 0
			for path, keys := range want {
				tables++
				if strings.Join(got[path], ",") != strings.Join(keys, ",") {
					if bad < 3 {
						t.Errorf("%s: %s: Go %.80v, Lua %.80v", filepath.Base(p), path, got[path], keys)
					}
					bad++
				}
			}
			if bad > 3 {
				t.Errorf("%s: %d tables in another order", filepath.Base(p), bad)
			}
		}
	}
	t.Logf("%d tables compared", tables)
}
