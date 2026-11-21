package sv_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
)

func mustParse(t *testing.T, src string) *sv.File {
	t.Helper()
	f, err := sv.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	return f
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := pyjson.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGameFormat(t *testing.T) {
	// The way the game writes a file: one field per line, list entries without keys.
	f := mustParse(t, "\nWoWMCP_Char = {\n[\"name\"] = \"Brakk\",\n[\"level\"] = 53,\n[\"crit\"] = 0.0224,\n"+
		"[\"whole\"] = 2.0,\n[\"ok\"] = true,\n[\"list\"] = {\n10,\n45,\n},\n[\"empty\"] = {\n},\n"+
		"[\"map\"] = {\n[3402] = {\n[\"quantity\"] = 7,\n},\n},\n}\nOther = 5\n")
	c := f.Tab("WoWMCP_Char")
	if c.Str("name") != "Brakk" || c.Get("level") != int64(53) || c.Get("crit") != 0.0224 || c.Get("ok") != true {
		t.Fatalf("fields: %s", asJSON(t, c))
	}
	if _, isFloat := c.Get("whole").(float64); !isFloat {
		t.Error("2.0 is a float, as in Lua 5.4")
	}
	if got := asJSON(t, c); got != `{"name": "Brakk", "level": 53, "crit": 0.0224, "whole": 2.0, "ok": true, `+
		`"list": [10, 45], "empty": {}, "map": {"3402": {"quantity": 7}}}` {
		t.Errorf("JSON as lua_to_py makes it: %s", got)
	}
	if f.Get("Other") != int64(5) || f.Get("Missing") != nil {
		t.Error("other globals")
	}
	if len(f.Names()) != 2 {
		t.Errorf("names %v", f.Names())
	}
}

func TestLuaSyntax(t *testing.T) {
	f := mustParse(t, `-- a comment
--[[ a long
comment ]] X = { a = 1, b = 'two'; [ "c d" ] = -3.5e2, "first", "second", [10] = nil, x = { }, --[==[ ]==]
  esc = "q\"\\\n\t\065\066C\x44\u{FC}\z
       end", long = [[
line]], hex = 0x10, neg = - 7, big = 9223372036854775808, fkey = { [1.0] = "one", [2] = "two" } }`)
	x := f.Tab("X")
	checks := map[string]any{"a": int64(1), "b": "two", "c d": -350.0, "esc": "q\"\\\n\tABCDü" + "end",
		"long": "line", "hex": int64(16), "neg": int64(-7), "big": 9223372036854775808.0}
	for k, want := range checks {
		if got := x.Get(k); got != want {
			t.Errorf("%s: got %#v, want %#v", k, got, want)
		}
	}
	if x.Get(int64(1)) != "first" || x.Get(2) != "second" || x.Has(10) {
		t.Error("positional fields and a nil field")
	}
	if !x.Tab("fkey").IsList() || x.Tab("fkey").List()[0] != "one" {
		t.Error("a float key with an integer value is that integer")
	}
	if x.Tab("x").IsList() || asJSON(t, x.Tab("x")) != "{}" {
		t.Error("an empty table is an object")
	}
}

func TestListDetection(t *testing.T) {
	f := mustParse(t, `A = { [2] = "b", [1] = "a" } B = { [1] = "a", [3] = "c" } C = { [1] = "a", n = 1 }`)
	if got := asJSON(t, f.Tab("A")); got != `["a", "b"]` {
		t.Errorf("keys 1..n in any order are a list: %s", got)
	}
	if got := asJSON(t, f.Tab("B")); got != `{"1": "a", "3": "c"}` {
		t.Errorf("a gap makes an object: %s", got)
	}
	if got := asJSON(t, f.Tab("C")); got != `{"1": "a", "n": 1}` {
		t.Errorf("mixed keys make an object: %s", got)
	}
}

func TestInvalidUTF8(t *testing.T) {
	// Python reads the file with errors="replace": every invalid byte, or truncated sequence, is one U+FFFD.
	f := mustParse(t, "S = \"a\xffb\xe2\x82c\"")
	if got := f.Get("S"); got != "a�b�c" {
		t.Errorf("got %q", got)
	}
	if got := sv.DecodeReplace([]byte{0xf0, 0x9f, 0x98}); got != "�" {
		t.Errorf("truncated four-byte sequence: %q", got)
	}
}

func TestErrors(t *testing.T) {
	for _, src := range []string{`X = {`, `X = { a = }`, `X = "open`, `X = foo`, `= 1`, `X = { [nil] = 1 }`} {
		if _, err := sv.Parse([]byte(src)); err == nil {
			t.Errorf("no error for %q", src)
		}
	}
}

func TestPythonJSONNumbers(t *testing.T) {
	tab := sv.NewTable()
	tab.Set("f", 1795289733.114016)
	tab.Set("i", int64(3))
	tab.Set("w", 3.0)
	tab.Set("inf", math.Inf(1))
	if got := asJSON(t, tab); got != `{"f": 1795289733.114016, "i": 3, "w": 3.0, "inf": Infinity}` {
		t.Errorf("got %s", got)
	}
}

// cborDB builds the Lua text Auctionator writes: a CBOR blob inside a string, with the game's escapes.
func cborDB(realms map[string][]byte) []byte {
	out := []byte("\nAUCTIONATOR_CONFIG = {\n}\nAUCTIONATOR_PRICE_DATABASE = {\n[\"__dbversion\"] = 8,\n")
	for realm, blob := range realms {
		out = append(out, []byte("[\""+realm+"\"] = \"")...)
		for _, c := range blob {
			switch c {
			case '"':
				out = append(out, '\\', '"')
			case '\\':
				out = append(out, '\\', '\\')
			case '\n':
				out = append(out, '\\', 'n')
			case 0:
				out = append(out, '\\', '0', '0', '0')
			default:
				out = append(out, c)
			}
		}
		out = append(out, []byte("\",\n")...)
	}
	return append(out, []byte("}\n")...)
}

func TestPrices(t *testing.T) {
	// {"2447": {"a": {2510: 3, 2516: 1}, "m": 120, "l": [], "h": {"2516": 1.5}}, 12: true}
	big := []byte{0xa2,
		0x64, '2', '4', '4', '7', 0xa4,
		0x61, 'a', 0xa2, 0x19, 0x09, 0xce, 0x03, 0x19, 0x09, 0xd4, 0x01,
		0x61, 'm', 0x18, 120,
		0x41, 'l', 0x80, // a byte-string key and an empty array
		0x61, 'h', 0xa1, 0x64, '2', '5', '1', '6', 0xf9, 0x3e, 0x00, // half float 1.5
		0x0c, 0xf5}
	small := []byte{0xa1, 0x61, 'x', 0x0a}
	dir := t.TempDir()
	path := filepath.Join(dir, "Auctionator.lua")
	if err := os.WriteFile(path, cborDB(map[string][]byte{"Small Realm": small, "Big Realm": big}), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := sv.Prices(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := asJSON(t, db); got != `{"2447": {"a": {"2510": 3, "2516": 1}, "m": 120, "l": [], "h": {"2516": 1.5}}, "12": true}` {
		t.Errorf("the realm with the most entries, keys as str(): %s", got)
	}
	day, ok := sv.ScanDate(db)
	if !ok || day.Format("2006-01-02") != "2026-11-21" { // 2020-01-01 + 2516 days
		t.Errorf("scan date %v %v", day, ok)
	}
	if err := os.WriteFile(path, []byte("AUCTIONATOR_CONFIG = {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sv.Prices(path); err == nil || err.Error() != "subsection not found" {
		t.Errorf("no database: %v", err)
	}
	if err := os.WriteFile(path, cborDB(map[string][]byte{"R": {0x83, 1, 2, 3}}), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sv.Prices(path); err == nil {
		t.Error("an array is no database")
	}
}

// TestFixture reads every SavedVariables file of the test folder.
func TestFixture(t *testing.T) {
	root := filepath.Join("..", "..", "..", "scripts", "fixtures", "forever", "WTF")
	paths, _ := filepath.Glob(filepath.Join(root, "Account", "*", "*", "*", "SavedVariables", "WoWMCP.lua"))
	acct, _ := filepath.Glob(filepath.Join(root, "Account", "*", "SavedVariables", "*.lua"))
	paths = append(paths, acct...)
	if len(paths) < 4 {
		t.Fatalf("fixture files: %v", paths)
	}
	for _, p := range paths {
		if filepath.Base(p) == "Auctionator.lua" {
			db, err := sv.Prices(p)
			if err != nil || db.Len() < 1000 {
				t.Errorf("%s: %v", p, err)
			}
			continue
		}
		f, err := sv.ReadFile(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if len(f.Names()) == 0 {
			t.Errorf("%s: no globals", p)
		}
	}
}
