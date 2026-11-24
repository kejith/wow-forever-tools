package recipes

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// The same hand-made data as scripts/test_recipes.py.

func tbl(pairs ...any) *sv.Table {
	t := sv.NewTable()
	for i := 0; i+1 < len(pairs); i += 2 {
		t.Set(pairs[i], pairs[i+1])
	}
	return t
}

// days is {"<day>": value, …} as Auctionator's CBOR keys come.
func days(pairs ...int64) *sv.Table {
	t := sv.NewTable()
	for i := 0; i+1 < len(pairs); i += 2 {
		t.Set(itoa(pairs[i]), pairs[i+1])
	}
	return t
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// Auctionator format: m = lowest price, a = auctions per scan day, h = day price.
func testPrices() *sv.Table {
	return tbl(
		"100", tbl("m", int64(1000), "a", days(10, 50), "h", days(10, 1000)), // result A: 10s
		"101", tbl("m", int64(200), "a", days(10, 80), "h", days(10, 200)), // herb: 2s
		"3372", tbl("m", int64(60), "a", days(10, 900), "h", days(10, 60)), // Leaded Vial: more in the AH than at the vendor
		"200", tbl("m", int64(5000), "a", days(10, 20), "h", days(10, 5000)), // result B: 50s
		"900", tbl("m", int64(20000), "a", days(10, 2), "h", days(10, 20000)), // recipe item for B: 2g
		"300", tbl("m", int64(100), "a", days(8, 5), "h", days(8, 100, 10, 100)), // result C, price 2 days old
	)
}

func str(s string) *string { return &s }

func testCatalog() *Catalog {
	items := NewOrdered[*Item]()
	add := func(id string, f func(*Item)) {
		it := NewItem()
		f(it)
		items.Set(id, it)
	}
	add("3372", func(it *Item) { it.Name, it.Buy, it.BuyStack = str("Leaded Vial"), 200, 5 }) // 40c each
	add("100", func(it *Item) { it.Name = str("Potion A") })
	add("101", func(it *Item) { it.Name = str("Herb") })
	add("200", func(it *Item) { it.Name = str("Potion B") })
	add("300", func(it *Item) { it.Name, it.Bind, it.Sell = str("Bound C"), 1, 25 })
	add("900", func(it *Item) { it.Name, it.ReqSkill = str("Recipe: Potion B"), 150 })
	recs := NewOrdered[*Recipe]()
	recs.Set("1", &Recipe{Name: "Potion A", Output: 100, Qty: 1, Reagents: [][]int64{{101, 2}, {3372, 1}},
		Yellow: 100, Grey: 140})
	recs.Set("2", &Recipe{Name: "Potion B", Output: 200, Qty: 1, Reagents: [][]int64{{101, 5}}, Yellow: 160,
		Grey: 200, RecipeItems: []int64{900}})
	recs.Set("3", &Recipe{Name: "C", Output: 300, Qty: 2, Reagents: [][]int64{{101, 1}}, Yellow: 90, Grey: 120})
	recs.Set("4", &Recipe{Name: "Unobtainable", Output: 100, Qty: 1, Reagents: [][]int64{{101, 1}}, Yellow: 1,
		Grey: 2, Acquire: 3})
	lines := NewOrdered[*Ordered[*Recipe]]()
	lines.Set("171", recs)
	return &Catalog{Lines: lines, Items: items}
}

func rec(id string) *Recipe {
	r, _ := testCatalog().Lines.ByKey["171"].Get(id)
	return r
}

func market(vendor *sv.Table) *Market { return NewMarket(testPrices(), vendor, testCatalog()) }

func get(o any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			o = o.(*pyjson.Obj).Get(k)
		case int:
			o = o.([]any)[k]
		}
	}
	return o
}

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	g, _ := pyjson.Encode(got)
	w, _ := pyjson.Encode(want)
	if string(g) != string(w) {
		t.Errorf("%s = %s, want %s", what, g, w)
	}
}

func withLanguage(t *testing.T, lang string) {
	i18n.SetLanguage(lang, "")
	t.Cleanup(func() { i18n.SetLanguage("en", "") })
}

func anyNote(row *Row, sub string, prefix bool) bool {
	for _, n := range row.Obj.Get("notes").([]any) {
		s := n.(string)
		if prefix && strings.HasPrefix(s, sub) || !prefix && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestPyInt(t *testing.T) { // recipes._int, values from Python
	cases := map[string]int64{"1.9": 1, "-1.9": -1, "": 0, "abc": 0, " 7 ": 7, "1e3": 1000, "nan": 0, "2_5": 25,
		"0x10": 0, "+4": 4}
	for in, want := range cases {
		if got := pyInt(in); got != want {
			t.Errorf("pyInt(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestFmtCD(t *testing.T) { // recipes.fmt_cd, values from Python (".0f" rounds half to even)
	cases := map[int64]any{0: nil, 600: "10 min", 3599: "59 min", 3600: "1 h", 5400: "2 h", 9000: "2 h",
		12600: "4 h", 169200: "47 h", 1830: "30 min"}
	for in, want := range cases {
		s := in
		if got := FmtCD(&s); got != want {
			t.Errorf("FmtCD(%d) = %v, want %v", in, got, want)
		}
	}
	if FmtCD(nil) != nil {
		t.Error("FmtCD(nil) is not nil")
	}
}

func TestColor(t *testing.T) {
	for rank, want := range map[float64]int64{90: 0, 100: 1, 120: 2, 140: 3} {
		eq(t, "color", ColorOf(rank, 100, 140), want)
	}
	if ColorOf(10, 5, 0) != nil {
		t.Error("no grey threshold should be None")
	}
}

func TestProfitUsesCheapestReagentAndAHCut(t *testing.T) {
	row := Evaluate(1, rec("1"), nil, 130, market(nil), map[int64]int64{101: 5}, nil)
	// 2 x 2s herb + 40c vial from the vendor (AH: 60c) = 440c; sale 1000c - 5% = 950c
	eq(t, "cost", row.Obj.Get("cost"), 440)
	eq(t, "vial source", get(row.Obj, "reagents", 1, "source"), "vendor")
	eq(t, "vial vendor", get(row.Obj, "reagents", 1, "vendor"), nil)
	eq(t, "herb source", get(row.Obj, "reagents", 0, "source"), "ah")
	eq(t, "sell", row.Obj.Get("sell"), 950)
	eq(t, "profit", row.Obj.Get("profit"), 510)
	eq(t, "craftable", row.Obj.Get("craftable"), 0) // no vial owned
	eq(t, "difficulty", row.Obj.Get("difficulty"), 2)
}

func TestSeenVendorPriceWinsAndIngameReagentsOverride(t *testing.T) {
	vendor := tbl(int64(3372), tbl("price", int64(50), "npc", "Hamuul"))
	reagents := sv.NewTable()
	reagents.Set(int64(1), tbl("id", int64(101), "count", int64(1)))
	reagents.Set(int64(2), tbl("id", int64(3372), "count", int64(1)))
	scan := tbl("output", int64(100), "qtyMin", int64(2), "qtyMax", int64(4), "reagents", reagents)
	row := Evaluate(1, rec("1"), scan, 130, market(vendor), nil, nil)
	eq(t, "source", get(row.Obj, "reagents", 1, "source"), "vendor")
	eq(t, "vendor", get(row.Obj, "reagents", 1, "vendor"), "Hamuul")
	eq(t, "unit", get(row.Obj, "reagents", 1, "unit"), 50)
	eq(t, "cost", row.Obj.Get("cost"), 250)
	eq(t, "qty", row.Obj.Get("qty"), 3.0)
	eq(t, "sell", row.Obj.Get("sell"), 2850)
}

func TestBoundOutputFallsBackToVendorSell(t *testing.T) {
	row := Evaluate(3, rec("3"), nil, 130, market(nil), nil, nil)
	eq(t, "sellSource", row.Obj.Get("sellSource"), "vendor")
	eq(t, "sell", row.Obj.Get("sell"), 50)
	if !anyNote(row, "binds when picked up", false) || !anyNote(row, "2 days old", false) {
		t.Errorf("notes %v", row.Obj.Get("notes"))
	}
	withLanguage(t, "de")
	row = Evaluate(3, rec("3"), nil, 130, market(nil), nil, nil)
	eq(t, "sellSource", row.Obj.Get("sellSource"), "vendor") // keys stay, only the texts change
	if !anyNote(row, "gebunden", false) || !anyNote(row, "2 Tage alt", false) {
		t.Errorf("notes %v", row.Obj.Get("notes"))
	}
}

func TestSplitLearnedMissingAndAcquisition(t *testing.T) {
	scanned := sv.NewTable()
	scanned.Set(int64(1), tbl("learned", true, "output", int64(100)))
	scanned.Set(int64(2), tbl("learned", false))
	scan := tbl("rank", int64(130), "recipes", scanned)
	learned, missing := ProfessionRows(171, testCatalog(), scan, 130, market(nil), nil)
	if len(learned) != 1 || learned[0].Obj.Get("id") != int64(1) {
		t.Fatalf("learned %v", objs(learned))
	}
	ids := map[any]*Row{}
	for _, r := range missing {
		ids[r.Obj.Get("id")] = r
	}
	if len(ids) != 2 || ids[int64(2)] == nil || ids[int64(3)] == nil { // 4 cannot be obtained
		t.Fatalf("missing %v", objs(missing))
	}
	b := ids[int64(2)].Acquire
	eq(t, "learnable", b.Learnable, false)
	eq(t, "reqSkill", b.Obj.Get("reqSkill"), 150)
	eq(t, "best kind", b.Best.Get("kind"), "ah")
	eq(t, "best price", b.Best.Get("price"), 20000)
	c := ids[int64(3)].Acquire
	eq(t, "trainer", c.Obj.Get("trainer"), true)
	eq(t, "learnable", c.Learnable, true) // yellow (90) is below 130
}

func TestWithoutScanEverythingIsUnknown(t *testing.T) {
	learned, missing := ProfessionRows(171, testCatalog(), nil, 130, market(nil), nil)
	if len(learned) != 0 {
		t.Errorf("learned %v", objs(learned))
	}
	for _, r := range missing {
		eq(t, "learned", r.Obj.Get("learned"), nil)
		if r.Obj.Get("id") == int64(4) {
			t.Error("recipe 4 cannot be obtained")
		}
	}
}

// historyMarket: four full scan days (20-23) for Potion A (100) made of 2 herbs (101) and 1 vial (vendor
// 40c).
func historyMarket(out map[int64]int64, herb map[int64]int64, partialDay int64) *Market {
	dayList := []int64{20, 21, 22, 23}
	if herb == nil {
		herb = map[int64]int64{20: 200, 21: 200, 22: 200, 23: 200}
	}
	a, h := sv.NewTable(), sv.NewTable()
	for i, d := range dayList {
		if _, ok := out[d]; ok {
			a.Set(itoa(d), int64(10*(i+1)))
		}
	}
	for _, d := range dayList {
		if p, ok := out[d]; ok {
			h.Set(itoa(d), p)
		}
	}
	ha, hh := sv.NewTable(), sv.NewTable()
	for _, d := range dayList {
		if p, ok := herb[d]; ok {
			ha.Set(itoa(d), int64(80))
			hh.Set(itoa(d), p)
		}
	}
	prices := tbl("100", tbl("m", out[23], "a", a, "h", h), "101", tbl("m", herb[23], "a", ha, "h", hh))
	for i := int64(0); i < 8; i++ { // filler items, so the days count as full scans
		prices.Set(itoa(500+i), tbl("m", int64(1), "a", days(20, 1, 21, 1, 22, 1, 23, 1),
			"h", days(20, 1, 21, 1, 22, 1, 23, 1)))
	}
	if partialDay != 0 { // a single search on a day without a full scan
		a.Set(itoa(partialDay), int64(3))
		h.Set(itoa(partialDay), int64(9999))
	}
	return NewMarket(prices, nil, testCatalog())
}

func TestHistoryPerFullScanDay(t *testing.T) {
	m := historyMarket(map[int64]int64{20: 3000, 21: 2000, 22: 1000, 23: 1000}, nil, 19)
	eq(t, "scan days", m.ScanDays, []int64{20, 21, 22, 23}) // day 19 is not a full scan
	row := Evaluate(1, rec("1"), nil, 130, m, nil, nil)
	// cost 2 x 200 + 40 = 440; sale -5%
	var profits []any
	for _, h := range row.Obj.Get("history").([]any) {
		profits = append(profits, h.(*pyjson.Obj).Get("profit"))
	}
	eq(t, "profits", profits, []any{2410, 1460, 510, 510})
	eq(t, "last point", profits[3], row.Obj.Get("profit")) // last point = current profit
	eq(t, "first point", get(row.Obj, "history", 0),
		pyjson.New("date", "2020-01-21", "profit", 2410, "price", 3000, "listed", 10))
	eq(t, "trend", row.Obj.Get("trend"), "falling")
	if !anyNote(row, "profit is falling (on 01/22 still +0.15g)", true) {
		t.Errorf("notes %v", row.Obj.Get("notes"))
	}
	if anyNote(row, "cheap right now", false) { // 1000 < 60% of the median, but no outlier
		t.Errorf("notes %v", row.Obj.Get("notes"))
	}
	withLanguage(t, "de")
	row = Evaluate(1, rec("1"), nil, 130, m, nil, nil)
	eq(t, "trend", row.Obj.Get("trend"), "falling")
	if !anyNote(row, "Gewinn fällt (am 22.01. noch +0.15g)", true) {
		t.Errorf("notes %v", row.Obj.Get("notes"))
	}
}

func TestHistoryUsesDayMiddleAndSkipsDaysWithoutReagentPrice(t *testing.T) {
	m := historyMarket(map[int64]int64{20: 1000, 21: 1000, 22: 1000, 23: 1000},
		map[int64]int64{20: 200, 22: 300, 23: 200}, 0)
	m.Prices.Tab("100").Set("l", days(22, 800)) // two scans on day 22: 1000 and 800 -> middle 900
	m.Prices.Tab("101").Set("l", days(22, 100)) // herb 300 and 100 -> middle 200
	hist := ProfitHistory(m, 100, 1, []Pair{{101, 2}, {3372, 1}})
	var dates []string
	for _, h := range hist {
		dates = append(dates, h.Date[8:])
	}
	eq(t, "dates", dates, []string{"21", "23", "24"}) // day 21: no herb price
	eq(t, "price", hist[1].Price, 900)
	eq(t, "profit", hist[1].Profit, 415) // round(900 * 0.95 - 440)
}

func TestTrend(t *testing.T) {
	pts := func(profits ...int64) []HistoryPoint {
		var out []HistoryPoint
		for _, p := range profits {
			out = append(out, HistoryPoint{Profit: p})
		}
		return out
	}
	eq(t, "too few", TrendOf(pts(500, 400), 0), nil)
	eq(t, "stable", TrendOf(pts(1000, 1050, 950, 1000), 0), "stable")
	eq(t, "rising", TrendOf(pts(200, 300, 900, 1100), 0), "rising")
	eq(t, "falling", TrendOf(pts(4000, 3000, 700, 200), 0), "falling")
	eq(t, "collapsed", TrendOf(pts(200, 1200, 1100, 300), 0), "falling") // rose first, collapsed at the end
	eq(t, "no direction", TrendOf(pts(1800, -800, 270, 40, 280), 0), "stable")
	eq(t, "copper", TrendOf(pts(10, -10, 20, -20), 0), "stable") // copper amounts around zero are no trend
	row := Evaluate(3, rec("3"), nil, 130, market(nil), nil, nil)
	eq(t, "bound history", row.Obj.Get("history"), []any{}) // bound: no AH sale, no history
	eq(t, "bound trend", row.Obj.Get("trend"), nil)
}

// The catalog is stored per client locale, so a German and an English client do not read each other's
// names; a catalog is read in the file's order and written back byte for byte.
func TestCatalogIsCachedPerLocale(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("WOW_TOOLS_CACHE", cache)
	for _, c := range [][2]string{{"deDE", "Trank"}, {"enUS", "Potion"}} {
		text := `{"version": 2, "build": "1.0.0.5", "locale": "` + c[0] + `", "lines": {"171": {}}, ` +
			`"items": {"1": {"name": "` + c[1] + `"}}}`
		if err := os.WriteFile(filepath.Join(cache, "recipes-1.0.0.5-"+c[0]+".json"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct{ build, locale, want string }{
		{"1.0.0.5", "deDE", "Trank"}, {"1.0.0.5", "enUS", "Potion"}, {"", "deDE", "Trank"}} {
		cat, err := LoadCatalog(c.build, c.locale)
		if err != nil {
			t.Fatal(err)
		}
		if n := cat.Item(1).Name; n == nil || *n != c.want {
			t.Errorf("LoadCatalog(%q, %q): name %v, want %s", c.build, c.locale, n, c.want)
		}
	}
	// Order and format: what Python's json.dump wrote comes back the same.
	text := `{"version": 2, "build": "1.0.0.5", "locale": "deDE", "lines": {"185": {"9": {"name": "Brot", ` +
		`"output": 5, "qty": 1, "reagents": [[7, 2]], "yellow": 1, "grey": 5, "acquire": 0, "recipeItems": [], ` +
		`"cooldown": null}}, "171": {"3": {"name": "Tränk \"x\"", "output": 2, "qty": 2, "reagents": [], ` +
		`"yellow": 0, "grey": 0, "acquire": 3, "recipeItems": [8, 9], "cooldown": 3600}}}, "items": {"5": {"name": ` +
		`null, "quality": 1, "buy": 0, "buyStack": 1, "sell": 0, "bind": 0, "reqSkill": 0, "reqSkillLine": 0}}}`
	cat, err := DecodeCatalog([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "line order", cat.Lines.Keys, []string{"185", "171"})
	data, err := cat.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(text, `ä`, "ä", 1)
	if string(data) != want {
		t.Errorf("encoded\n%s\nwant\n%s", data, want)
	}
}

func TestOwnedCounts(t *testing.T) {
	list := func(items ...*sv.Table) *sv.Table {
		l := sv.NewTable()
		for i, it := range items {
			l.Set(int64(i+1), it)
		}
		return l
	}
	c := tbl("bags", list(tbl("items", list(tbl("id", int64(5), "count", int64(3)), tbl("id", int64(6))))),
		"bank", tbl("containers", list(tbl("items", list(tbl("id", int64(5), "count", int64(2)))))))
	got := OwnedCounts(c)
	if len(got) != 2 || got[5] != 5 || got[6] != 1 {
		t.Errorf("owned %v", got)
	}
}
