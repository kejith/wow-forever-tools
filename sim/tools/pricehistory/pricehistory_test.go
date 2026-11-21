package pricehistory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
)

const build = "1.60.1.70938"

func TestLowerLikePython(t *testing.T) {
	for _, c := range lowerCases {
		if got := Lower(c.text); got != c.want {
			t.Errorf("Lower(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func parseTable(t *testing.T, lua string) *sv.Table {
	t.Helper()
	f, err := sv.Parse([]byte("X = " + lua))
	if err != nil {
		t.Fatal(err)
	}
	return f.Tab("X")
}

func TestDaysLikePython(t *testing.T) {
	for _, c := range daysCases {
		var rows [][]any
		for _, d := range DaysOf(parseTable(t, c.lua)) {
			rows = append(rows, d.json())
		}
		got, _ := pyjson.Encode(rows)
		if rows == nil {
			got = []byte("[]")
		}
		if string(got) != c.want {
			t.Errorf("days of %s:\n got %s\nwant %s", c.lua, got, c.want)
		}
	}
}

// prices is Auctionator's format: h = highest minimum price of the day, l = lowest (only if below h),
// a = offers, m = last minimum price.
const prices = `{
  ["2447"] = { ["m"] = 158, ["h"] = { ["2512"] = 35, ["2510"] = 40, ["2513"] = 158 }, ["l"] = { ["2512"] = 30, ["2513"] = 999 }, ["a"] = { ["2510"] = 50, ["2513"] = 12 } },
  ["500"] = { ["m"] = 0, ["h"] = { ["2513"] = 90000 }, ["l"] = {}, ["a"] = { ["2513"] = 2 } },
  ["600"] = { ["m"] = 12, ["h"] = { ["2511"] = 12 }, ["l"] = {}, ["a"] = {} },
  ["700"] = { ["m"] = 5, ["h"] = { ["2509"] = 5 } },
  ["800"] = { ["m"] = 5, ["h"] = {} },
  ["p:123"] = { ["m"] = 5, ["h"] = { ["2513"] = 5 } },
  ["version"] = 2,
}`

// 500: equipment only the item export names; 600: only in a bag; 700: nobody knows its name; 800: no day left.
const names = `{"2447": ["Friedensblume", 1, 7], "500": [null, null, 4], "600": [null, null, 7], "700": [null, null, null]}`

const char = `WoWMCP_Char = {
  ["client"] = { ["version"] = "1.60.1", ["build"] = "70938", ["locale"] = "deDE" },
  ["bags"] = { { ["items"] = { { ["id"] = 600, ["name"] = "Leinenstoff", ["count"] = 3, ["quality"] = 1 } } } },
}
`

const account = `WoWMCP_Items = { ["locale"] = "deDE", ["items"] = {
  [500] = { ["name"] = "Robe der Tests", ["quality"] = 3, ["classID"] = 4 },
} }
`

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// wowFolder is a WoW folder with the client language, one character with a bag and the account's item export.
func wowFolder(t *testing.T, acct string) string {
	wow := t.TempDir()
	write(t, filepath.Join(wow, "WTF", "Config.wtf"), "SET textLocale \"deDE\"\n")
	write(t, filepath.Join(wow, "WTF", "Account", "TEST", "SavedVariables", "WoWMCP.lua"), acct)
	write(t, filepath.Join(wow, "WTF", "Account", "TEST", "93", "Tester-Realm", "SavedVariables", "WoWMCP.lua"), char)
	return wow
}

// withCache makes a cache folder with the names of the build and points the tables at it.
func withCache(t *testing.T) {
	cache := t.TempDir()
	write(t, filepath.Join(cache, "itemnames-"+build+"-deDE.json"), `{"version": 1, "items": `+names+`}`)
	t.Setenv("WOW_TOOLS_CACHE", cache)
	i18n.SetLanguage("en", "")
}

func run(t *testing.T, wow, lua string) map[string]any {
	t.Helper()
	out, err := Compute(wow, parseTable(t, lua))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := pyjson.Encode(out)
	var r map[string]any
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func byID(r map[string]any) map[float64]map[string]any {
	out := map[float64]map[string]any{}
	for _, v := range r["items"].([]any) {
		it := v.(map[string]any)
		out[it["id"].(float64)] = it
	}
	return out
}

func TestDaysAndNames(t *testing.T) {
	withCache(t)
	r := run(t, wowFolder(t, account), prices)
	items := byID(r)
	if len(items) != 4 || r["count"] != 4.0 || r["unnamed"] != 1.0 || r["locale"] != "deDE" {
		t.Fatalf("count %v unnamed %v locale %v items %v", r["count"], r["unnamed"], r["locale"], items)
	}
	peace := items[2447]
	// Oldest day first; a lowest price above the day's highest is no lowest price.
	days := []any{[]any{2510.0, 40.0, 40.0, 50.0}, []any{2512.0, 30.0, 35.0, nil}, []any{2513.0, 158.0, 158.0, 12.0}}
	if !reflect.DeepEqual(peace["days"], days) {
		t.Errorf("days %v", peace["days"])
	}
	check := func(id float64, name, q, cls, m any) {
		it := items[id]
		if it["name"] != name || it["q"] != q || it["cls"] != cls || (m != nil && it["m"] != m) {
			t.Errorf("item %v: %v", id, it)
		}
	}
	check(2447, "Friedensblume", 1.0, 7.0, 158.0)
	// Names the game tables lack: the item export, then the bags; quality and class from there too.
	check(500, "Robe der Tests", 3.0, 4.0, 90000.0)
	check(600, "Leinenstoff", 1.0, 7.0, nil)
	check(700, nil, nil, nil, nil)
	// Sorted by name, items without a name last.
	var order []float64
	for _, v := range r["items"].([]any) {
		order = append(order, v.(map[string]any)["id"].(float64))
	}
	if !reflect.DeepEqual(order, []float64{2447, 600, 500, 700}) {
		t.Errorf("order %v", order)
	}
	if r["lastDay"] != 2513.0 || r["lastDate"] != "2026-11-18" || r["firstDay"] != 2509.0 {
		t.Errorf("lastDay %v lastDate %v firstDay %v", r["lastDay"], r["lastDate"], r["firstDay"])
	}
	// Day 2513 has 2 of the 4 items: still a scan.
	if r["lastScanDay"] != 2513.0 || r["lastScanDate"] != "2026-11-18" || r["modified"] != nil {
		t.Errorf("lastScanDay %v lastScanDate %v", r["lastScanDay"], r["lastScanDate"])
	}
}

// A day with less than 30 % of the items of the biggest day is a single search, not a scan.
func TestLastScanIgnoresSingleSearches(t *testing.T) {
	withCache(t)
	var b strings.Builder
	b.WriteString("{")
	for i := 1; i <= 10; i++ {
		extra := ""
		if i == 1 {
			extra = `, ["2513"] = 12` // one search the next day
		}
		b.WriteString(`["` + strconv.Itoa(i) + `"] = { ["m"] = 10, ["h"] = { ["2512"] = 10` + extra + ` } }, `)
	}
	b.WriteString("}")
	r := run(t, wowFolder(t, account), b.String())
	if r["lastDay"] != 2513.0 || r["lastScanDay"] != 2512.0 {
		t.Errorf("lastDay %v lastScanDay %v", r["lastDay"], r["lastScanDay"])
	}
}

func TestExportOfAnotherLanguageIsIgnored(t *testing.T) {
	withCache(t)
	r := run(t, wowFolder(t, strings.Replace(account, `"deDE"`, `"enUS"`, 1)), prices)
	if byID(r)[500]["name"] != nil || r["unnamed"] != 2.0 {
		t.Errorf("item 500 %v unnamed %v", byID(r)[500], r["unnamed"])
	}
}

func TestNamesAreBuiltFromTheTablesAndCached(t *testing.T) {
	cache := t.TempDir()
	write(t, filepath.Join(cache, "Item-"+build+".csv"), "ID,ClassID\n2447,7\n500,4\n")
	write(t, filepath.Join(cache, "ItemSparse-"+build+"-enUS.csv"), "ID,Display_lang,OverallQualityID\n2447,Peacebloom,1\n")
	n, err := LoadNames(cache, build, "enUS")
	if err != nil {
		t.Fatal(err)
	}
	if n.Len() != 2 || n.Get("2447") != [3]any{"Peacebloom", int64(1), int64(7)} || n.Get("500") != [3]any{nil, nil, int64(4)} {
		t.Fatalf("names %v %v", n.Get("2447"), n.Get("500"))
	}
	data, err := os.ReadFile(filepath.Join(cache, "itemnames-"+build+"-enUS.json"))
	if err != nil {
		t.Fatal(err)
	}
	// json.dump(..., ensure_ascii=False): Python's separators, the order of the tables.
	if want := `{"version": 1, "items": {"2447": ["Peacebloom", 1, 7], "500": [null, null, 4]}}`; string(data) != want {
		t.Errorf("cache file %s", data)
	}
	// The second time comes from the cache, also without a build.
	_ = os.Remove(filepath.Join(cache, "Item-"+build+".csv"))
	_ = os.Remove(filepath.Join(cache, "ItemSparse-"+build+"-enUS.csv"))
	for _, b := range []string{build, ""} {
		n, err := LoadNames(cache, b, "enUS")
		if err != nil || n.Len() != 2 || n.Get("2447") != [3]any{"Peacebloom", int64(1), int64(7)} {
			t.Errorf("from the cache (build %q): %v %v", b, n, err)
		}
	}
}

func TestWithoutPrices(t *testing.T) {
	withCache(t)
	_, err := Compute(wowFolder(t, account), parseTable(t, `{ ["version"] = 2 }`))
	if err == nil || !strings.Contains(err.Error(), "Auctionator.lua") {
		t.Errorf("no items should be an error, got %v", err)
	}
}
