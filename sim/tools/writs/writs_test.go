package writs

// The tests of scripts/test_writs.py (same data, same assertions), plus the readers that copy a Python
// quirk: the regular expressions over WoWToolsForever.lua, the shortcut through the ItemSparse CSV and Python's slice.

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/internal/tables"
)

func tab(pairs ...any) *sv.Table {
	t := sv.NewTable()
	for i := 0; i+1 < len(pairs); i += 2 {
		t.Set(pairs[i], pairs[i+1])
	}
	return t
}

func p(m, listed int64) *sv.Table {
	return tab("m", m, "a", tab("10", listed), "h", tab("10", m))
}

// testPrices: Journeyman Elixir of Wisdom (264012 -> 3383), Expert Mana Potion (264062 -> 3827); the
// Apprentice herb crate 248682: Peacebloom is cheap, Silverleaf cheaper but too few in the AH.
func testPrices() *sv.Table {
	return tab("264012", p(5000, 50), "3383", p(2000, 50), "264062", p(9000, 50), "3827", p(4000, 50),
		"248682", p(1000, 50), "2447", p(30, 100), "765", p(10, 5), "2449", p(500, 30))
}

func testQuests() *QuestOf {
	q := &QuestOf{}
	q.Add(264012, 94212)
	q.Add(264062, 94262)
	return q
}

func run(t *testing.T, e *Extras, level int64) *Result {
	t.Helper()
	if e == nil {
		e = &Extras{}
	}
	e.Level, e.Favor = level, int64(120)
	r, err := Calc(Options{Wow: t.TempDir(), Top: 3, Prices: testPrices(), Extras: e, Quests: testQuests()})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func writ(t *testing.T, r *Result, id int64) *Writ {
	t.Helper()
	for _, tier := range r.Tiers {
		for _, w := range tier.Best {
			if w.WritID == id {
				return w
			}
		}
	}
	t.Fatalf("writ %d not in the result", id)
	return nil
}

func crateTier(t *testing.T, r *Result, tier string) *CrateTier {
	t.Helper()
	for _, ct := range r.Crates.Tiers {
		if ct.Tier == tier {
			return ct
		}
	}
	t.Fatalf("crate tier %s missing", tier)
	return nil
}

func crate(t *testing.T, r *Result, id int64) *Crate {
	t.Helper()
	for _, ct := range r.Crates.Tiers {
		for _, c := range ct.Rows {
			if c.CrateID == id {
				return c
			}
		}
	}
	t.Fatalf("crate %d missing", id)
	return nil
}

func english(t *testing.T) {
	i18n.SetLanguage("en", "")
	t.Cleanup(func() { i18n.SetLanguage("en", "") })
}

func TestEstimateWithoutMeasurement(t *testing.T) {
	english(t)
	w := writ(t, run(t, nil, 46), 264012)
	if w.Cost != 7000 || w.Payout != PouchEstimate["J"] || w.PayoutMeasured {
		t.Fatalf("%+v", w)
	}
	if w.Net != 7000-PouchEstimate["J"] || w.Per != float64(w.Net)/75 {
		t.Fatalf("%+v", w)
	}
	if w.QuestID == nil || *w.QuestID != 94212 {
		t.Fatalf("quest %v", w.QuestID)
	}
}

func TestMeasuredPouchAndTierFallback(t *testing.T) {
	english(t)
	e := &Extras{
		WritQuests: tab(int64(94212), tab("pouch", int64(284583), "money", int64(0), "n", int64(1))),
		Pouches: tab(int64(284583), tab("n", int64(2), "sum", int64(8000), "min", int64(3000), "max", int64(5000)),
			int64(284584), tab("n", int64(1), "sum", int64(9000), "min", int64(9000), "max", int64(9000))),
	}
	r := run(t, e, 46)
	w := writ(t, r, 264012)
	if w.Payout != 4000 || !w.PayoutMeasured || !strings.Contains(w.PayoutSource, "2x") {
		t.Fatalf("%+v", w)
	}
	if w.PayoutSource != "measured: coin pouch opened 2x, 30s 00c to 50s 00c" {
		t.Fatalf("source %q", w.PayoutSource)
	}
	// No Expert writ handed in: the pouch is unknown, so the estimate stays (do not guess 284584).
	x := writ(t, r, 264062)
	if x.Payout != PouchEstimate["E"] || x.PayoutMeasured {
		t.Fatalf("%+v", x)
	}
	var net int64
	for _, tier := range r.Tiers {
		if len(tier.Best) > 0 {
			net += tier.Best[0].Net
		}
	}
	if r.DailyNet != net {
		t.Fatalf("daily net %d, want %d", r.DailyNet, net)
	}
}

func TestPouchOfTheTierWhenTheWritHasNone(t *testing.T) {
	english(t)
	// Two Journeyman writs handed in, both with pouch 284583: a third Journeyman writ without a turn-in
	// takes that pouch (the most votes, the first of equal ones), money of its own none.
	q := &QuestOf{}
	q.Add(264012, 94212)
	q.Add(264019, 94219)
	q.Add(264013, 94213)
	e := &Extras{
		WritQuests: tab(int64(94212), tab("pouch", int64(284583), "money", int64(100)),
			int64(94219), tab("pouch", int64(284583))),
		Pouches: tab(int64(284583), tab("n", int64(4), "sum", int64(10002), "min", int64(2000), "max", int64(3000))),
	}
	value := pouchValues(e, q)
	if v, _, ok := value(264012, "J"); !ok || v != 2500+100 { // round(2500.5) = 2500, half to even
		t.Fatalf("own pouch %d", v)
	}
	if v, _, ok := value(264013, "J"); !ok || v != 2500 {
		t.Fatalf("tier pouch %d", v)
	}
	if v, _, ok := value(264062, "E"); ok || v != PouchEstimate["E"] {
		t.Fatalf("estimate %d", v)
	}
}

func TestCrateCheapestBundleTheAHCanFill(t *testing.T) {
	english(t)
	r := run(t, nil, 46)
	herbs := crate(t, r, 248682)
	// Silverleaf costs 200c for 20, but only 5 are listed -> Peacebloom 20 x 30c = 600c
	if herbs.Best.ID != 2447 || *herbs.Best.Cost != 600 {
		t.Fatalf("%+v", herbs.Best)
	}
	if herbs.Cost != 1600 || *herbs.Per != 1600.0/5 || *herbs.PerGreen != 1600.0/10 {
		t.Fatalf("%+v", herbs)
	}
	if r.Crates.Best[0].CrateID != 248682 || r.Favor != int64(120) {
		t.Fatalf("best %d, favor %v", r.Crates.Best[0].CrateID, r.Favor)
	}
}

func TestCrateWithoutPriceIsIncompleteAndLevelGate(t *testing.T) {
	english(t)
	r := run(t, nil, 15)
	ore := crate(t, r, 248683)
	if ore.Complete || ore.Per != nil || !strings.Contains(ore.Note, "not in the AH") {
		t.Fatalf("%+v", ore)
	}
	if !crateTier(t, r, "E").TooLow {
		t.Fatal("Expert is not too low at level 15")
	}
	for _, b := range r.Crates.Best {
		if b.Tier == "E" {
			t.Fatal("an Expert crate in the best list")
		}
	}
}

func TestMeasuredFavorOverridesGuess(t *testing.T) {
	english(t)
	e := &Extras{Crates: tab(SealedCrate["A"][0], tab("n", int64(3), "sum", int64(75), "min", int64(25),
		"max", int64(25)))}
	ct := crateTier(t, run(t, e, 46), "A")
	if ct.Favor != 25.0 || ct.FavorGuess || !strings.Contains(ct.FavorSource, "3x") {
		t.Fatalf("%+v", ct)
	}
	ct = crateTier(t, run(t, nil, 46), "A")
	if !ct.FavorGuess || ct.Favor != CrateFavor["A"][0] {
		t.Fatalf("%+v", ct)
	}
}

func TestCrateDataMatchesGameAmounts(t *testing.T) {
	// Amounts from SpellEffect (spell "Fill Crate"), build 1.60.1.70938
	game := map[int64][]int64{248682: {20, 20, 10, 10, 10}, 248707: {8, 7, 4, 45}, 248712: {2, 10, 20},
		248696: {20, 20, 20, 20}}
	if len(CrateList) != 30 {
		t.Fatalf("%d crates", len(CrateList))
	}
	for _, c := range CrateList {
		want, ok := game[c.ID]
		if !ok {
			continue
		}
		var got []int64
		for _, b := range c.Bundles {
			got = append(got, b[1])
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%d: %v, want %v", c.ID, got, want)
		}
	}
	if len(WritList) != 150 {
		t.Fatalf("%d writs", len(WritList))
	}
}

func TestNamesFollowTheLanguage(t *testing.T) {
	english(t)
	r := run(t, nil, 46)
	w := writ(t, r, 264012)
	herbs := crate(t, r, 248682)
	if w.Name != "Elixir of Wisdom" || w.Prof != "Alchemy" || crateTier(t, r, "L").Name != "Apprentice" ||
		herbs.Kind != "Herbs" || herbs.Best.Name != "Peacebloom" {
		t.Fatalf("%q %q %q %q", w.Name, w.Prof, herbs.Kind, herbs.Best.Name)
	}
	i18n.SetLanguage("de", "")
	r = run(t, nil, 15)
	w = writ(t, r, 264012)
	herbs = crate(t, r, 248682)
	if w.Name != "Elixier der Weisheit" || w.Prof != "Alchimie" || crateTier(t, r, "L").Name != "Lehrling" ||
		herbs.Kind != "Kräuter" || herbs.Best.Name != "Friedensblume" {
		t.Fatalf("%q %q %q %q", w.Name, w.Prof, herbs.Kind, herbs.Best.Name)
	}
	if ore := crate(t, r, 248683); !strings.Contains(ore.Note, "nicht im AH") {
		t.Fatalf("note %q", ore.Note)
	}
}

func TestEveryTableItemHasANameInBothLanguages(t *testing.T) {
	english(t)
	ids := map[int64]bool{}
	for _, w := range WritList {
		ids[w.ID], ids[w.Item] = true, true
	}
	for _, c := range CrateList {
		ids[c.ID] = true
		for _, b := range c.Bundles {
			ids[b[0]] = true
		}
	}
	for _, lang := range i18n.Languages {
		i18n.SetLanguage(lang, "")
		for id := range ids {
			if ItemName(id) == "Item "+strconv.FormatInt(id, 10) {
				t.Errorf("%s: no name for %d", lang, id)
			}
		}
	}
	if ItemName(1) != "Item 1" {
		t.Fatalf("unknown item: %q", ItemName(1))
	}
}

func TestDataDirFindsTheNames(t *testing.T) {
	dir := DataDir()
	if dir == "" || filepath.Base(dir) != "data" {
		t.Fatalf("data folder %q", dir)
	}
}

// TestLoadOwned reads a snapshot with CRLF line ends; the expected values are what writs.load_char returns
// for the same text: an item counts only in an innermost table with a link (765 has a nested table, 2447 no
// link), counts of the same item add up, the writ faction's standing is the reputation.
func TestLoadOwned(t *testing.T) {
	text := strings.Join([]string{`WoWToolsForever_Char = {`, `["bags"] = {`,
		`{`, `["id"] = 3383,`, `["count"] = 2,`, `["link"] = "|cff1eff00|Hitem:3383|h[Elixir]|h|r",`, `},`,
		`{`, `["count"] = 3,`, `["id"] = 3383,`, `["link"] = "x",`, `},`,
		`{`, `["id"] = 2447,`, `["count"] = 5,`, `},`,
		`{`, `["id"] = 765,`, `["count"] = 7,`, `["link"] = "a",`, `["tip"] = { "nested" },`, `},`,
		`},`, `["reputation"] = {`,
		`{`, `["id"] = 2587,`, `["name"] = "Durotar Supply and Logistics",`, `["standing"] = "Friendly",`,
		`["value"] = 9125,`, `["max"] = 21000,`, `},`,
		`{`, `["id"] = 76,`, `["name"] = "Orgrimmar",`, `["standing"] = "Honored",`, `["value"] = 1,`,
		`["max"] = 2,`, `},`, `},`, `}`, ``}, "\r\n")
	path := filepath.Join(t.TempDir(), "WoWToolsForever.lua")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	owned, rep, err := LoadOwned(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(owned, map[int64]int64{3383: 5}) {
		t.Fatalf("owned %v", owned)
	}
	want := &Reputation{Name: "Durotar Supply and Logistics", Standing: "Friendly", Value: 9125, Max: 21000}
	if !reflect.DeepEqual(rep, want) {
		t.Fatalf("reputation %+v", rep)
	}
}

func TestSliceEnd(t *testing.T) {
	// len(xs[:top]) for xs of length 5, as Python computes it.
	for top, want := range map[int]int{-7: 0, -5: 0, -2: 3, 0: 0, 3: 3, 5: 5, 9: 5} {
		if got := sliceEnd(5, top); got != want {
			t.Errorf("sliceEnd(5, %d) = %d, want %d", top, got, want)
		}
	}
}

// questsByDictReader is writ_quests' loop over every row of tables.Parse (csv.DictReader).
func questsByDictReader(t *testing.T, text string, wanted map[int64]bool) *QuestOf {
	q := &QuestOf{Quest: map[int64]int64{}}
	for _, r := range tables.Parse(text).Rows() {
		id, err := strconv.ParseInt(strings.TrimSpace(r.Str("ID")), 10, 64)
		if err != nil {
			t.Fatalf("ID %q", r.Str("ID"))
		}
		s := r.Str("StartQuestID")
		if s == "" {
			s = "0"
		}
		if n, err := strconv.ParseInt(s, 10, 64); wanted[id] && isDigits(s) && err == nil && n != 0 {
			q.Add(id, n)
		}
	}
	return q
}

func TestQuestsFromCSV(t *testing.T) {
	wanted := map[int64]bool{264012: true, 264013: true, 264014: true, 264015: true, 264016: true}
	text := "Display_lang,ID,StartQuestID\r\n" +
		"\"Elixir\nof Wisdom\",264012,94212\r\n" + // a quoted field that goes on into the next line
		"\n" + // an empty row
		"plain,264013,94213\n" +
		"\"say \"\"hi\"\"\",264014,0\r" + // doubled quotes, a lone CR
		"short,264015\n" + // no StartQuestID: None or '0'
		"x,264016,94216,extra\n" +
		"x,99,7\n" +
		"\"last\",264017,1" // no line end
	got, err := questsFromCSV(text, wanted)
	if err != nil {
		t.Fatal(err)
	}
	want := questsByDictReader(t, text, wanted)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(got.Writs, []int64{264012, 264013, 264016}) {
		t.Fatalf("writs %v", got.Writs)
	}
	if _, err := questsFromCSV("ID,StartQuestID\nabc,1\n", wanted); err == nil ||
		err.Error() != "invalid literal for int() with base 10: 'abc'" {
		t.Fatalf("error %v", err)
	}
}

// TestQuestsFromCachedTable compares the shortcut with the full CSV reader on the cached ItemSparse tables,
// where there are any.
func TestQuestsFromCachedTable(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(tables.CacheDir(), "ItemSparse-*.csv"))
	if len(files) == 0 {
		t.Skip("no ItemSparse table in the cache")
	}
	wanted := map[int64]bool{}
	for _, w := range WritList {
		wanted[w.ID] = true
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		got, err := questsFromCSV(string(data), wanted)
		if err != nil {
			t.Fatal(err)
		}
		if want := questsByDictReader(t, string(data), wanted); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: %d writs, the csv reader %d", f, len(got.Writs), len(want.Writs))
		}
	}
}
