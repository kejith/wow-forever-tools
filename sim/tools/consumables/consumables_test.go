package consumables

import (
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/internal/tables"
	"github.com/kejith/wow-tools-forever/sim/tools/recipes"
)

// world builds the catalog of one of the made-up DB2 worlds of scripts/test_consumables.py.
func world(t *testing.T, name string) *Catalog {
	t.Helper()
	for _, w := range worldCases {
		if w.name != name {
			continue
		}
		cat, err := buildCatalog(func(table string) ([]tables.Row, error) {
			text, ok := w.tables[table]
			if !ok {
				return nil, nil
			}
			return tables.Parse(text).Rows(), nil
		}, "1.0.0.1", "enUS")
		if err != nil {
			t.Fatal(err)
		}
		return cat
	}
	t.Fatalf("no world %q", name)
	return nil
}

// The catalog of each world is byte for byte what Python's json.dump writes, and reads back the same.
func TestCatalogAsPython(t *testing.T) {
	for _, w := range worldCases {
		cat := world(t, w.name)
		data, err := cat.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != w.catalog {
			t.Errorf("%s: catalog\n got %s\nwant %s", w.name, data, w.catalog)
		}
		back, err := DecodeCatalog([]byte(w.catalog))
		if err != nil {
			t.Fatal(err)
		}
		again, _ := back.Encode()
		if string(again) != w.catalog {
			t.Errorf("%s: read back\n got %s\nwant %s", w.name, again, w.catalog)
		}
	}
}

func TestCatalogDecodesEffects(t *testing.T) {
	items := world(t, "page").Items
	for _, id := range []string{"50", "51"} {
		if _, ok := items.Get(id); ok {
			t.Errorf("item %s: deprecated and conjured items are skipped", id)
		}
	}
	soup, _ := items.Get("21")
	if !soup.WellFed || soup.duration() != 3600 || len(soup.Buffs) != 1 || soup.Buffs[0] != (Buff{"sp", 22}) {
		t.Errorf("Nightfin Soup: %+v", soup)
	}
	bandage, _ := items.Get("30")
	if bandage.ReqSkill != 129 || bandage.Restore[0].Amount != 1104 {
		t.Errorf("bandage: %+v", bandage)
	}
	rune, _ := world(t, "fight").Items.Get("20520")
	if rune.Subclass != other || rune.HealthCost != 800 || rune.Cat != catRune {
		t.Errorf("Dark Rune: %+v", rune)
	}
}

func TestFloorDiv(t *testing.T) {
	for _, c := range floorDivCases {
		if got := floorDiv(c.x, c.y); got != c.want {
			t.Errorf("%v // %v = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestUsesIn(t *testing.T) {
	for _, c := range usesCases {
		if got := usesIn(c.seconds); got != c.want {
			t.Errorf("uses_in(%v) = %d, want %d", c.seconds, got, c.want)
		}
	}
}

func TestFightDelta(t *testing.T) {
	cats := map[string]*Catalog{"page": world(t, "page"), "fight": world(t, "fight")}
	for _, c := range deltaCases {
		it, _ := cats[c.world].Items.Get(c.id)
		data, err := pyjson.Encode(fightDelta(it, c.kind, c.seconds, c.hps).Obj())
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != c.want {
			t.Errorf("%s %s %s %vs: %s, want %s", c.world, c.id, c.kind, c.seconds, data, c.want)
		}
	}
}

func TestTexts(t *testing.T) {
	i18n.SetLanguage("en", "")
	cats := map[string]*Catalog{"page": world(t, "page"), "fight": world(t, "fight")}
	for _, c := range restoreCases {
		it, _ := cats[c.world].Items.Get(c.id)
		if got := restoreText(it.Restore[c.index]); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.world, c.id, got, c.want)
		}
	}
	for _, c := range durationCases {
		if got := fmtDuration(c.seconds); got != c.want {
			t.Errorf("fmt_duration(%d) = %v, want %v", c.seconds, got, c.want)
		}
	}
}

// The page of a level 21 warlock in the small world, as test_best_for_level_with_prices_bags_and_recipes
// checks it: strongest first, the mage potion left out, the AH price, a recipe nobody learned.
func TestBestForLevel(t *testing.T) {
	i18n.SetLanguage("en", "")
	cat := world(t, "page")
	prices := sv.NewTable()
	for id, m := range map[string]int64{"10": 80, "11": 500, "40": 1600, "42": 2000, "2287": 900} {
		e := sv.NewTable()
		e.Set("m", m)
		a := sv.NewTable()
		a.Set("10", int64(3))
		e.Set("a", a)
		prices.Set(id, e)
	}
	ch := sv.NewTable()
	ch.Set("name", "Test")
	ch.Set("level", int64(21))
	ch.Set("classFile", "WARLOCK")
	c := sv.NewTable()
	c.Set("character", ch)
	recipeCat := &recipes.Catalog{Lines: recipes.NewOrdered[*recipes.Ordered[*recipes.Recipe]]()}
	alchemy := recipes.NewOrdered[*recipes.Recipe]()
	alchemy.Set("1", &recipes.Recipe{Output: 11})
	alchemy.Set("2", &recipes.Recipe{Output: 12})
	recipeCat.Lines.Set("171", alchemy)
	out, _, err := evaluate(c, cat, recipeCat, prices, sv.NewTable(), 5, false, nil, 60, nil, Set{})
	if err != nil {
		t.Fatal(err)
	}
	heal := out.Get("categories").([]any)[0].(*pyjson.Obj)
	items := heal.Get("items").([]any)
	if len(items) != 2 {
		t.Fatalf("healing: %d items", len(items))
	}
	first := items[0].(*pyjson.Obj)
	if first.Get("id") != int64(11) || first.Get("best") != true || first.Get("price") != int64(500) ||
		first.Get("source") != "ah" {
		t.Errorf("best healing potion: %v", first.Keys())
	}
	craft := first.Get("craft").(*pyjson.Obj)
	if craft.Get("profession") != "Alchemy" || craft.Get("learned") != false {
		t.Errorf("craft: %v %v", craft.Get("profession"), craft.Get("learned"))
	}
	mana := out.Get("categories").([]any)[1].(*pyjson.Obj)
	if len(mana.Get("items").([]any)) != 0 || mana.Get("next").(*pyjson.Obj).Get("id") != int64(12) {
		t.Errorf("mana: the potion of level 22 is next")
	}
}

func TestActiveWeaponEnchantMarksOilActive(t *testing.T) {
	i18n.SetLanguage("en", "")
	cat := world(t, "fight")
	prices := sv.NewTable()
	for _, id := range []string{"60", "69"} {
		e := sv.NewTable()
		e.Set("m", int64(5000))
		a := sv.NewTable()
		a.Set("10", int64(3))
		e.Set("a", a)
		prices.Set(id, e)
	}
	ch := sv.NewTable()
	ch.Set("name", "Test")
	ch.Set("level", int64(55))
	ch.Set("classFile", "WARLOCK")
	we := sv.NewTable()
	mh := sv.NewTable()
	mh.Set("id", int64(2629)) // Wizard Oil in the "fight" world
	mh.Set("remainingSec", int64(1500))
	we.Set("mainHand", mh)
	ch.Set("weaponEnchants", we)
	c := sv.NewTable()
	c.Set("character", ch)

	x := &ctx{
		classFile: "WARLOCK",
		market:    recipes.NewMarket(prices, sv.NewTable(), nil),
		vendor:    sv.NewTable(),
		owned:     map[int64]int64{},
		skills:    map[int64]any{},
		learned:   map[int64]bool{},
		madeBy:    map[int64]int64{},
		profNames: map[int64]any{},
	}
	rows := fightCandidates(c, cat, x, 60)
	var oil *Row
	for _, r := range rows {
		if r.ID == 69 {
			oil = r
			break
		}
	}
	if oil == nil {
		t.Fatalf("Wizard Oil (69) missing from fight candidates")
	}
	if !oil.Active || oil.OnYou == "" || oil.Delta.Len() != 0 {
		t.Fatalf("Wizard Oil should be marked active with empty delta, got active=%v onYou=%q deltaLen=%d",
			oil.Active, oil.OnYou, oil.Delta.Len())
	}
}
