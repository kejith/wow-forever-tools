package ingame

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// The cases of scripts/test_ingame.py: the four calculations are replaced by hand-made results.

const upgradesJSON = `{
  "budget": 150000, "profileLabel": "Leveling",
  "package": {"gain": 12.5, "cost": 90000, "items": [
    {"id": 9001, "name": "Robe of Tests", "quality": 2, "slotName": "Chest", "gain": 12.5, "price": 90000}]},
  "slots": [{"slotName": "Chest", "current": {"name": "Old Robe"}, "candidates": [
      {"id": 9001, "name": "Robe of Tests", "quality": 2, "gain": 12.5, "price": 90000},
      {"id": 9002, "name": "Robe | of Pipes", "quality": 3, "gain": 4.0, "price": 500000, "soon": true, "reqLevel": 50}]},
    {"slotName": "Head", "current": null, "candidates": [
      {"id": 9003, "name": "Hat", "quality": 2, "gain": 1.0, "price": 100}]}],
  "review": [{"id": 9004, "name": "Odd Trinket", "quality": 3, "slot": "Trinket", "price": 7000}]}`

const writsJSON = `{
  "reputation": {"name": "Durotar Supply and Logistics", "standing": "Friendly", "value": 100, "max": 6000},
  "favor": 25,
  "tiers": [{"name": "Journeyman", "best": [
    {"writId": 264012, "itemId": 3383, "name": "Elixir of Wisdom", "qty": 4, "prof": "Alchemy",
     "wcost": 5000, "icost": 8000, "net": 9700, "per": 129.3},
    {"writId": 264013, "itemId": 3384, "name": "Owned Thing", "qty": 2, "prof": "Alchemy",
     "wcost": 0, "icost": 0, "net": -3300, "per": -44.0}]}],
  "crates": {
    "best": [{"crateId": 248682, "name": "Herb Crate", "crateCost": 1000, "per": 500, "haveCrate": false,
              "best": {"id": 2447, "name": "Peacebloom", "qty": 50, "cost": 1500}}],
    "tiers": [{"name": "Apprentice", "favor": 5, "level": 10, "tooLow": false, "rows": [
      {"crateId": 248682, "name": "Herb Crate", "crateCost": 1000, "per": 500, "complete": true,
       "best": {"id": 2447, "name": "Peacebloom", "qty": 50, "cost": 1500},
       "options": [{"id": 2447, "qty": 50}, {"id": 765, "qty": 40}]},
      {"crateId": 248683, "name": "Ore Crate", "complete": false, "best": null, "options": []}]}]}}`

const recipesJSON = `{"professions": [{
  "name": "Alchemy",
  "learned": [
    {"name": "Mana Potion", "output": 3827, "outputName": "Mana Potion", "quality": 1, "qty": 1, "profit": 1200,
     "trend": "falling", "reagents": [
       {"id": 785, "name": "Mageroyal", "quality": 1, "count": 2, "owned": 0, "cost": 400, "source": "ah"},
       {"id": 3371, "name": "Empty Vial", "quality": 1, "count": 1, "owned": 0, "cost": 4, "source": "vendor"},
       {"id": 2450, "name": "Briarthorn", "quality": 1, "count": 1, "owned": 5, "cost": 100, "source": "ah"}]},
    {"name": "Loss Potion", "output": 3828, "outputName": "Loss Potion", "quality": 1, "qty": 1, "profit": -50,
     "reagents": [{"id": 785, "name": "Mageroyal", "count": 9, "owned": 0, "cost": 1800, "source": "ah"}]}],
  "missing": [
    {"name": "Elixir of Gain", "profit": 3000, "payback": 2.0, "better": true, "acquire": {
      "learnable": true, "best": {"id": 7001, "name": "Recipe: Elixir of Gain", "quality": 2, "price": 6000,
                                  "kind": "ah"},
      "options": [{"id": 7001}]}},
    {"name": "Elixir of Patience", "profit": 100, "payback": 400.0, "better": true, "acquire": {
      "learnable": true, "best": {"id": 7002, "name": "Recipe: Elixir of Patience", "quality": 2, "price": 40000,
                                  "kind": "ah"},
      "options": [{"id": 7002}]}},
    {"name": "Elixir of Later", "profit": 900, "payback": 5.0, "better": false, "acquire": {
      "learnable": false, "reqSkill": 250, "best": {"id": 7003, "name": "Recipe: Elixir of Later", "price": 4500,
                                                    "kind": "vendor"},
      "options": [{"id": 7003}]}}]}]}`

const consumablesJSON = `{"level": 46, "categories": [{"label": "Healing potions", "items": [
    {"id": 3928, "name": "Superior Healing Potion", "quality": 1, "effect": "restores 700-900 health", "price": 2500,
     "source": "ah", "best": true, "available": true, "owned": 0},
    {"id": 1710, "name": "Greater Healing Potion", "quality": 1, "effect": "restores 455-585 health", "price": 900,
     "source": "ah", "best": false, "available": true, "owned": 3},
    {"id": 929, "name": "Unobtainable", "effect": "x", "available": false, "price": null}]}],
  "buffs": [
    {"label": "Intellect", "relevant": true, "items": [
      {"id": 9179, "name": "Elixir of Greater Intellect", "effect": "+25 intellect", "duration": "1 h",
       "price": 3000, "source": "ah", "best": true, "available": true, "owned": 2}]},
    {"label": "Strength", "relevant": false, "items": [
      {"id": 9206, "name": "Elixir of Giants", "effect": "+25 strength", "price": 1, "source": "ah",
       "best": true, "available": true}]}]}`

// decode reads JSON as Python's json.loads does: ints stay ints, floats floats.
func decode(t *testing.T, text string) any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return numbers(v)
}

func numbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil && !strings.ContainsAny(string(x), ".eE") {
			return i
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, val := range x {
			x[k] = numbers(val)
		}
	case []any:
		for i, val := range x {
			x[i] = numbers(val)
		}
	}
	return v
}

// with returns a copy of a decoded object with some keys replaced.
func with(t *testing.T, base string, pairs ...any) any {
	m := decode(t, base).(map[string]any)
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i].(string)] = pairs[i+1]
	}
	return m
}

// plain is a value as JSON, for comparisons.
func plain(t *testing.T, v any) string {
	t.Helper()
	b, err := pyjson.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sectionsOf(one *pyjson.Obj) map[string]*pyjson.Obj {
	out := map[string]*pyjson.Obj{}
	for _, s := range items(one.Get("sections")) {
		out[s.(*pyjson.Obj).Get("title").(string)] = s.(*pyjson.Obj)
	}
	return out
}

func rowsOf(s *pyjson.Obj) []*pyjson.Obj {
	var out []*pyjson.Obj
	for _, r := range items(s.Get("rows")) {
		out = append(out, r.(*pyjson.Obj))
	}
	return out
}

func english(t *testing.T) {
	t.Helper()
	prev := i18n.Language()
	i18n.SetLanguage("en", "")
	t.Cleanup(func() { i18n.SetLanguage(prev, "") })
}

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", what, got, want)
	}
}

func TestUpgradeList(t *testing.T) {
	english(t)
	tips := newIntDict()
	one := upgradeList(decode(t, upgradesJSON), tips)
	note := one.Get("note").(string)
	if one.Get("key") != "upgrades" || !strings.Contains(note, "{m:150000}") || !strings.Contains(note, "Leveling") {
		t.Errorf("note %q", note)
	}
	s := sectionsOf(one)
	pkg := s["Best package for the budget"]
	eq(t, "package note", pkg.Get("note"), "+12.5 points for {m:90000}")
	eq(t, "package rows", plain(t, pkg.Get("rows")),
		`[{"id": 9001, "name": "Robe of Tests", "q": 2, "info": "Chest +12.5", "price": 90000, "shop": true}]`)
	chest := s["Chest"]
	eq(t, "chest note", chest.Get("note"), "now: Old Robe")
	for _, r := range rowsOf(chest) {
		if r.Get("shop") != nil {
			t.Error("a slot row is shopped")
		}
	}
	eq(t, "escaped name", rowsOf(chest)[1].Get("name"), "Robe || of Pipes")
	eq(t, "later", rowsOf(chest)[1].Get("info"), "+4.0 · from level 50")
	eq(t, "head", s["Head"].Get("note"), "empty")
	eq(t, "review", rowsOf(s["With a special effect"])[0].Get("info"), "Trinket")
	eq(t, "tips 9001", tips.Lines(9001), []string{"Upgrade for Chest: +12.5 points, no. 1 in the AH scan ({m:90000})",
		"Part of the best package for your budget"})
	if !strings.Contains(tips.Lines(9002)[0], "no. 2") || tips.vals[9004] != nil {
		t.Error("tips 9002/9004", tips.Lines(9002))
	}
	if upgradeList(decode(t, `{"slots": [], "review": []}`), newIntDict()) != nil {
		t.Error("an empty result made a list")
	}

	// With the combat engine the same lists speak of DPS instead of points.
	tips = newIntDict()
	one = upgradeList(with(t, upgradesJSON, "engine", decode(t, `{"metric": "dps", "value": 98.7}`)), tips)
	if n := one.Get("note").(string); !strings.Contains(n, "combat model") || !strings.Contains(n, "98.7 DPS") {
		t.Error("engine note", n)
	}
	eq(t, "engine package", sectionsOf(one)["Best package for the budget"].Get("note"), "+12.5 DPS for {m:90000}")
	eq(t, "engine tip", tips.Lines(9001)[0], "Upgrade for Chest: +12.5 DPS, no. 1 in the AH scan ({m:90000})")

	// Gear the account already has: where it is instead of a price, never on the shopping list.
	ownedItem := `{"id": 9005, "name": "Bank Robe", "quality": 2, "slotName": "Chest", "gain": 20.0, "price": 0,
		"source": "bank", "owner": null, "where": "in your bank"}`
	base := decode(t, upgradesJSON).(map[string]any)
	slot := base["slots"].([]any)[0].(map[string]any)
	slot["candidates"] = append([]any{decode(t, ownedItem)}, slot["candidates"].([]any)...)
	pkgItems := base["package"].(map[string]any)["items"].([]any)
	base["package"] = map[string]any{"gain": 32.5, "cost": int64(90000),
		"items": append([]any{decode(t, ownedItem)}, pkgItems...)}
	base["slots"] = []any{slot}
	tips = newIntDict()
	one = upgradeList(base, tips)
	rows := rowsOf(sectionsOf(one)["Best package for the budget"])
	eq(t, "owned row", plain(t, rows[0]), `{"id": 9005, "name": "Bank Robe", "q": 2, "info": "Chest +20.0 · in your bank"}`)
	eq(t, "bought row", rows[1].Get("shop"), true)
	eq(t, "owned slot row", rowsOf(sectionsOf(one)["Chest"])[0].Get("info"), "+20.0 · in your bank")
	eq(t, "owned tips", tips.Lines(9005), []string{"You own this upgrade for Chest: +20.0 points (in your bank)",
		"Part of the best package for your budget"})
	if !strings.Contains(tips.Lines(9001)[0], "no. 1 in the AH scan") {
		t.Error("the rank counts owned gear", tips.Lines(9001))
	}
	base["engine"] = decode(t, `{"metric": "hps", "value": 50.0}`)
	tips = newIntDict()
	upgradeList(base, tips)
	eq(t, "healer owned tip", tips.Lines(9005)[0], "You own this upgrade for Chest: +20.0 HPS (in your bank)")

	// A healer's note says HPS and carries the downrank recommendation.
	one = upgradeList(with(t, upgradesJSON, "engine",
		decode(t, `{"metric": "hps", "value": 50.0, "downrank": {"spell": "Heal (Rank 1)"}}`)), newIntDict())
	if n := one.Get("note").(string); !strings.Contains(n, "50.0 HPS") || !strings.Contains(n, "heal with Heal (Rank 1)") {
		t.Error("healer note", n)
	}
}

func TestWritList(t *testing.T) {
	english(t)
	tips := newIntDict()
	one := writList(decode(t, writsJSON), tips)
	eq(t, "note", one.Get("note"), "Durotar Supply and Logistics: Friendly 100/6000")
	rows := rowsOf(sectionsOf(one)["Journeyman"])
	writ, item, ownedWrit, ownedItem := rows[0], rows[1], rows[2], rows[3]
	eq(t, "writ", []any{writ.Get("shop"), writ.Get("price"), writ.Get("info")},
		[]any{true, int64(5000), "net {m:9700} · {m:129} per reputation"})
	eq(t, "item", plain(t, item),
		`{"id": 3383, "name": "Elixir of Wisdom", "n": 4, "info": "Alchemy", "price": 8000, "shop": true, "sub": true}`)
	if ownedWrit.Get("shop") != nil || ownedItem.Get("shop") != nil || ownedWrit.Get("price") != nil {
		t.Error("an owned row is shopped")
	}
	if !strings.HasPrefix(ownedWrit.Get("info").(string), "you have it · profit {m:3300}") {
		t.Error("owned writ", ownedWrit.Get("info"))
	}
	eq(t, "owned item", ownedItem.Get("info"), "you have it · Alchemy")
	eq(t, "item tip", tips.Lines(3383), []string{"For a writ (Journeyman): hand in 4x, net {m:9700}"})
	if !strings.Contains(tips.Lines(264012)[0], "needs 4x Elixir of Wisdom") {
		t.Error("writ tip", tips.Lines(264012))
	}
}

func TestCrateList(t *testing.T) {
	english(t)
	tips := newIntDict()
	one := crateList(decode(t, writsJSON), tips)
	eq(t, "note", one.Get("note"), "Merchant's Favor: 25")
	best := rowsOf(sectionsOf(one)["Cheapest Merchant's Favor"])
	eq(t, "best ids", []any{best[0].Get("id"), best[1].Get("id")}, []any{int64(248682), int64(2447)})
	eq(t, "best shop", []any{best[0].Get("shop"), best[1].Get("shop")}, []any{true, true})
	eq(t, "best info", best[0].Get("info"), "{m:500} per favor")
	eq(t, "bundle", []any{best[1].Get("n"), best[1].Get("sub")}, []any{int64(50), true})
	tier := sectionsOf(one)["Apprentice"]
	eq(t, "tier note", tier.Get("note"), "5 favor each")
	for _, r := range rowsOf(tier) {
		if r.Get("shop") != nil {
			t.Error("a tier row is shopped")
		}
	}
	eq(t, "tier rows", len(rowsOf(tier)), 2)
	eq(t, "option tip", tips.Lines(765), []string{"Fills Herb Crate: 40x"})
	eq(t, "crate tip", tips.Lines(248682), []string{"Fill with 50x Peacebloom ({m:1500}): {m:500} per Merchant's Favor"})
	if crateList(decode(t, `{"crates": {}}`), newIntDict()) != nil {
		t.Error("no crates made a list")
	}
}

func TestRecipeList(t *testing.T) {
	english(t)
	tips, vendor := newIntDict(), newIntDict()
	one := recipeList(decode(t, recipesJSON), tips, vendor)
	crafts := rowsOf(sectionsOf(one)["Alchemy: best crafts"])
	var ids []any
	for _, r := range crafts {
		ids = append(ids, r.Get("id"))
	}
	eq(t, "crafts", ids, []any{int64(3827), int64(785), int64(3371), int64(2450)})
	eq(t, "craft info", crafts[0].Get("info"), "profit {m:1200} · falling")
	eq(t, "reagent", []any{crafts[1].Get("shop"), crafts[1].Get("n"), crafts[1].Get("sub")}, []any{true, int64(2), true})
	eq(t, "vendor reagent", []any{crafts[2].Get("shop"), crafts[2].Get("info")}, []any{nil, "vendor"})
	eq(t, "owned reagent", []any{crafts[3].Get("shop"), crafts[3].Get("info")}, []any{nil, "you have it"})
	missing := rowsOf(sectionsOf(one)["Alchemy: worth getting"])
	eq(t, "missing", len(missing), 2)
	eq(t, "worth it", []any{missing[0].Get("shop"), missing[0].Get("info")},
		[]any{true, "profit {m:3000} · paid off after 2 crafts"})
	eq(t, "400 crafts", missing[1].Get("shop"), nil)
	eq(t, "output tip", tips.Lines(3827), []string{"Alchemy: profit {m:1200} per craft · falling"})
	eq(t, "reagent tip", tips.Lines(785), []string{"Reagent for Mana Potion: profit {m:1200} per craft"})
	eq(t, "recipe tip", tips.Lines(7001), []string{"Teaches Elixir of Gain: profit {m:3000} per craft, paid off after 2 crafts",
		"Pays more than your best recipe"})
	l := tips.Lines(7003)
	eq(t, "skill tip", l[len(l)-1], "Needs Alchemy 250")
	eq(t, "vendor keys", vendor.keys, []int64{7001, 7002, 7003})
	if !strings.HasPrefix(vendor.vals[7003].(string), "Teaches Elixir of Later") {
		t.Error("vendor line", vendor.vals[7003])
	}
}

func TestConsumableList(t *testing.T) {
	english(t)
	tips := newIntDict()
	one := consumableList(decode(t, consumablesJSON), tips)
	eq(t, "note", one.Get("note"), "Level 46")
	s := sectionsOf(one)
	eq(t, "sections", len(s), 2)
	potions := rowsOf(s["Healing potions"])
	eq(t, "potions", []any{potions[0].Get("id"), potions[1].Get("id")}, []any{int64(3928), int64(1710)})
	eq(t, "best potion", []any{potions[0].Get("shop"), potions[0].Get("info")}, []any{true, "restores 700-900 health"})
	if !strings.HasPrefix(potions[1].Get("info").(string), "3 in bags · ") || potions[1].Get("shop") != nil {
		t.Error("owned potion", plain(t, potions[1]))
	}
	elixir := rowsOf(s["Intellect"])[0]
	eq(t, "elixir", []any{elixir.Get("info"), elixir.Get("shop")}, []any{"2 in bags · +25 intellect (1 h)", nil})
	eq(t, "tip", tips.Lines(3928), []string{"Best now (Healing potions): restores 700-900 health"})
	if tips.vals[9206] != nil {
		t.Error("a buff the class does not need has a tip")
	}
}

func TestTipLimitAndDuplicates(t *testing.T) {
	tips := newIntDict()
	for i := 0; i < 10; i++ {
		addTip(tips, "5", "line "+string(rune('0'+i%6)))
	}
	eq(t, "keys", tips.keys, []int64{5})
	eq(t, "lines", tips.Lines(5), []string{"line 0", "line 1", "line 2", "line 3"})
}

func TestLuaValue(t *testing.T) {
	nasty := "Günthers \"Trank\" \\ ]] --[[ \n|cffff0000  \x00"
	tips := newIntDict()
	tips.set(4711, []string{"one", "two"})
	v := pyjson.New("version", int64(1), "generated", int64(5), "hash", "abc", "none", nil, "flag", true,
		"ratio", 1.5, "whole", 2.0, "chars", pyjson.New("Günther", pyjson.New("lists", []any{
			pyjson.New("title", nasty, "rows", []any{pyjson.New("id", int64(1), "name", "A"), pyjson.New("id", int64(2))}),
		}, "tips", tips, "vendor", newIntDict())), "end", pyjson.New("while", int64(1)))
	text, err := luaValue(v, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range text {
		if c >= 127 {
			t.Fatal("not plain ASCII")
		}
	}
	want := `{
  version = 1,
  generated = 5,
  hash = "abc",
  flag = true,
  ratio = 1.5,
  whole = 2,
  chars = {
    ["G\195\188nther"] = {
      lists = {
        {
          title = "G\195\188nthers \"Trank\" \\ ]] --[[ \010|cffff0000  \000",
          rows = {
            { id = 1, name = "A" },
            { id = 2 },
          },
        },
      },
      tips = {
        [4711] = { "one", "two" },
      },
      vendor = {},
    },
  },
  ["end"] = { ["while"] = 1 },
}`
	if text != want {
		t.Errorf("got\n%s\nwant\n%s", text, want)
	}
	for _, bad := range []float64{nan(), inf()} {
		if _, err := luaValue(pyjson.New("x", bad), 0); err == nil {
			t.Error("a number Lua cannot read was written")
		}
	}
}

func nan() float64 { var z float64; return z / z }
func inf() float64 { var z float64; return 1 / z }

func addon(t *testing.T, wow string, files []string, version string) string {
	folder := addonDir(wow)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	toc := "## Interface: 16001\n## Version: " + version + "\n\n" + strings.Join(files, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(folder, "WoWToolsForever.toc"), []byte(toc), 0o644); err != nil {
		t.Fatal(err)
	}
	return folder
}

func payloadOf(text string) *Payload {
	tips := newIntDict()
	list := pyjson.New("key", "k", "title", text, "sections", []any{
		pyjson.New("title", "S", "rows", []any{pyjson.New("id", int64(1), "name", "A", "shop", true)})})
	p := &Payload{Generated: 100, Locale: "enUS", Names: []string{"Tester"}, Chars: map[string]*Char{
		"Tester": {Level: int64(1), Lists: []*pyjson.Obj{list}, Tips: tips, Vendor: newIntDict()}}}
	p.Hash = Digest(p)
	return p
}

func TestDigestIsPythons(t *testing.T) {
	// ingame.digest of payload_of("A") in scripts/test_ingame.py.
	if got := payloadOf("A").Hash; got != "8dd3f475365162c7" {
		t.Errorf("hash %s", got)
	}
}

func TestWriteRules(t *testing.T) {
	english(t)
	wow := t.TempDir()
	first := payloadOf("A")
	r, _ := Write(wow, first)
	if r.Get("written") != false || !strings.Contains(r.Get("reason").(string), "not installed") {
		t.Error("no addon", plain(t, r))
	}
	folder := addon(t, wow, []string{"AppRequest.lua", "Locales.lua", "WoWToolsForever.lua"}, "1.6.0")
	eq(t, "1.6", plain(t, AddonState(wow)), `{"installed": true, "ready": false, "version": "1.6.0"}`)
	r, _ = Write(wow, first)
	if r.Get("written") != false || !strings.Contains(r.Get("reason").(string), "1.6.0") {
		t.Error("old addon", plain(t, r))
	}
	if _, err := os.Stat(filepath.Join(folder, DataFile)); err == nil {
		t.Error("written into an addon that cannot read it")
	}
	addon(t, wow, []string{"AppRequest.lua", "AppData.lua", "Locales.lua", "WoWToolsForever.lua", "Lists.lua"}, "1.7.0")
	if AddonState(wow).Get("ready") != true || FileState(wow) != nil {
		t.Fatal("ready addon")
	}
	r, _ = Write(wow, first)
	eq(t, "written", r.Get("written"), true)
	eq(t, "state", plain(t, FileState(wow)), `{"generated": 100, "hash": "`+first.Hash+`"}`)
	entries, _ := os.ReadDir(folder)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Error("a temporary file is left", e.Name())
		}
	}
	data, _ := os.ReadFile(r.Get("file").(string))
	if !strings.Contains(string(data), "level = 1") {
		t.Error(string(data))
	}

	// The same content a minute later: the file stays, or the game would be asked to /reload forever.
	again := *first
	again.Generated = 160
	eq(t, "same hash", Digest(&again), first.Hash)
	r, _ = Write(wow, &again)
	eq(t, "unchanged", []any{r.Get("written"), r.Get("unchanged"), FileState(wow).Get("generated")},
		[]any{false, true, int64(100)})

	changed := payloadOf("B")
	changed.Generated = 200
	if changed.Hash == first.Hash {
		t.Fatal("same hash for other content")
	}
	r, _ = Write(wow, changed)
	eq(t, "changed", r.Get("written"), true)
	eq(t, "changed state", plain(t, FileState(wow)), `{"generated": 200, "hash": "`+changed.Hash+`"}`)
}

// fakeWow is a game folder with one priest and one warlock export and nothing else.
func fakeWow(t *testing.T, locale string) string {
	wow := t.TempDir()
	os.MkdirAll(filepath.Join(wow, "WTF"), 0o755)
	os.WriteFile(filepath.Join(wow, "WTF", "Config.wtf"), []byte(`SET textLocale "`+locale+`"`+"\n"), 0o644)
	for _, c := range [][2]string{{"Tester", "WARLOCK"}, {"Healer", "PRIEST"}} {
		folder := filepath.Join(wow, "WTF", "Account", "TEST", "93", c[0]+"-"+c[0], "SavedVariables")
		os.MkdirAll(folder, 0o755)
		os.WriteFile(filepath.Join(folder, "WoWToolsForever.lua"), []byte(`WoWToolsForever_Char = { character = { name = "`+c[0]+
			`", classFile = "`+c[1]+`", level = 46 } }`+"\n"), 0o644)
	}
	return wow
}

// withFakes runs fn with the four calculations replaced by the hand-made results.
func withFakes(t *testing.T, fn func()) {
	saved := []any{computeUpgrades, computeWrits, computeRecipes, computeConsumables, loadPrices}
	defer func() {
		computeUpgrades = saved[0].(func(string, string, *sv.Table) (any, error))
		computeWrits = saved[1].(func(string, string, *sv.Table) (any, error))
		computeRecipes = saved[2].(func(string, string, *sv.Table) (any, error))
		computeConsumables = saved[3].(func(string, string) (any, error))
		loadPrices = saved[4].(func(string) (*sv.Table, error))
	}()
	computeUpgrades = func(string, string, *sv.Table) (any, error) { return decode(t, upgradesJSON), nil }
	computeWrits = func(string, string, *sv.Table) (any, error) { return decode(t, writsJSON), nil }
	computeRecipes = func(string, string, *sv.Table) (any, error) { return nil, errors.New("no recipes exported") }
	computeConsumables = func(string, string) (any, error) { return decode(t, consumablesJSON), nil }
	loadPrices = func(string) (*sv.Table, error) { return nil, errors.New("no scan") }
	fn()
}

func keysOf(c *Char) []any {
	var out []any
	for _, l := range c.Lists {
		out = append(out, l.Get("key"))
	}
	return out
}

func TestPayloadPerCharacter(t *testing.T) {
	english(t)
	wow := fakeWow(t, "enUS")
	withFakes(t, func() {
		p := Compute(wow, "")
		eq(t, "chars", len(p.Chars), 2)
		eq(t, "locale", p.Locale, "enUS")
		lock, priest := p.Chars["Tester"], p.Chars["Healer"]
		all := []any{"upgrades", "writs", "crates", "consumables"}
		eq(t, "warlock lists", keysOf(lock), all)
		eq(t, "priest lists", keysOf(priest), all)
		eq(t, "errors", plain(t, lock.Errors), `{"recipes": "no recipes exported"}`)
		eq(t, "level", lock.Level, int64(46))
		if lock.Tips.vals[9001] == nil || priest.Tips.vals[9001] == nil {
			t.Error("tips")
		}
		eq(t, "hash", p.Hash, Digest(p))
		eq(t, "hash again", Compute(wow, "").Hash, p.Hash)
		eq(t, "one char", Compute(wow, "Healer").Names, []string{"Healer"})

		s, err := Sync(wow, "", false)
		if err != nil {
			t.Fatal(err)
		}
		if s.Get("result") != nil || s.Get("file") != nil || s.Get("loaded") != nil ||
			s.Get("addon").(*pyjson.Obj).Get("installed") != false {
			t.Error("sync", plain(t, s))
		}
		for _, c := range items(s.Get("chars")) {
			c := c.(*pyjson.Obj)
			if c.Get("name") == "Tester" {
				eq(t, "summary", plain(t, items(c.Get("lists"))[0]),
					`{"key": "upgrades", "title": "AH upgrades", "rows": 5, "shop": 1}`)
				eq(t, "summary tips", c.Get("tips"), lock.Tips.Len())
			}
		}
	})
}

func TestTextsFollowTheClientNotTheApp(t *testing.T) {
	english(t)
	wow := fakeWow(t, "deDE")
	withFakes(t, func() {
		p := Compute(wow, "")
		eq(t, "language restored", i18n.Language(), "en")
		lock := p.Chars["Tester"]
		eq(t, "title", lock.Lists[0].Get("title"), "AH-Upgrades")
		eq(t, "locale", p.Locale, "deDE")
		eq(t, "note", items(lock.Lists[0].Get("sections"))[0].(*pyjson.Obj).Get("note"), "+12,5 Punkte für {m:90000}")
		eq(t, "tip", lock.Tips.Lines(9001)[1], "Teil des besten Pakets für dein Budget")
	})
}

func TestSyncWritesAndReadsWhatTheGameLoaded(t *testing.T) {
	english(t)
	wow := fakeWow(t, "enUS")
	addon(t, wow, []string{"AppRequest.lua", "AppData.lua", "Locales.lua", "WoWToolsForever.lua", "Lists.lua"}, "1.7.0")
	withFakes(t, func() {
		s, _ := Sync(wow, "", true)
		file := s.Get("file").(*pyjson.Obj)
		if s.Get("result").(*pyjson.Obj).Get("written") != true || file.Get("hash") != s.Get("hash") ||
			s.Get("loaded") != nil || s.Get("saved") != nil || file.Get("modified").(float64) <= 0 {
			t.Error("first sync", plain(t, s))
		}
		s, _ = Sync(wow, "", true)
		eq(t, "unchanged", s.Get("result").(*pyjson.Obj).Get("unchanged"), true)
		// The game saves what it had loaded; the newest account file counts.
		for _, a := range [][2]string{{"TEST", "50"}, {"OTHER", "70"}} {
			folder := filepath.Join(wow, "WTF", "Account", a[0], "SavedVariables")
			os.MkdirAll(folder, 0o755)
			os.WriteFile(filepath.Join(folder, "WoWToolsForever.lua"), []byte(`WoWToolsForever_Account = { appData = { generated = 1, hash = "`+
				a[0]+`", loaded = `+a[1]+` } }`+"\n"), 0o644)
		}
		eq(t, "loaded", plain(t, loadedState(wow)), `{"generated": 1, "hash": "OTHER", "loaded": 70}`)
		s, _ = Sync(wow, "", false)
		if s.Get("saved").(float64) < s.Get("file").(*pyjson.Obj).Get("modified").(float64) {
			t.Error("saved before the file was written")
		}
	})
}

// TestDumpLua writes the data file of a real game folder for a byte comparison with Python's
// (INGAME_WOW=<_forever_> INGAME_DUMP=<file>; generated is 1 on both sides, see notes/go-migration.md).
func TestDumpLua(t *testing.T) {
	wow, out := os.Getenv("INGAME_WOW"), os.Getenv("INGAME_DUMP")
	if wow == "" || out == "" {
		t.Skip("INGAME_WOW and INGAME_DUMP not set")
	}
	if os.Getenv("WOW_TOOLS_CACHE") == "" { // the repo's cache, as the Python side uses, not one below this folder
		cache, _ := filepath.Abs(filepath.Join("..", "..", "..", "scripts", "cache"))
		t.Setenv("WOW_TOOLS_CACHE", cache)
	}
	p := Compute(wow, "")
	p.Generated = 1
	text, err := ToLua(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
