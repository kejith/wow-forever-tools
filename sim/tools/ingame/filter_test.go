package ingame

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

func TestIngameExportRespectsItemAndCategoryFilters(t *testing.T) {
	english(t)
	wow := t.TempDir()
	accSV := filepath.Join(wow, "WTF", "Account", "TEST", "SavedVariables")
	charSV := filepath.Join(wow, "WTF", "Account", "TEST", "Realm", "Tester", "SavedVariables")
	if err := os.MkdirAll(accSV, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(charSV, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wow, "WTF", "Config.wtf"), []byte(`SET textLocale "enUS"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	addon(t, wow, []string{"AppData.lua"}, "1.12.0")

	charLua := `WoWToolsForever_Char = {
  client = { locale = "enUS", build = "" },
  character = { name = "Tester", class = "Warlock", classFile = "WARLOCK", level = 60, faction = "Horde", money = 500000 },
  equipment = {
    { slot = 5, id = 1000, name = "Starter Robe", quality = 2, equipLoc = "INVTYPE_CHEST", classID = 4, subclassID = 1,
      stats = { ITEM_MOD_SPELL_POWER_SHORT = 5 }, tooltip = { "Starter Robe", "Chest", "+5 Spell Power" } },
  },
  skills = { skills = {} },
}
`
	itemsLua := `WoWToolsForever_Items = {
  updated = 1700000000,
  items = {
    [91001] = { id = 91001, name = "Warlord's Dreadweave Robe", quality = 4, reqLevel = 60, equipLoc = "INVTYPE_CHEST",
      classID = 4, subclassID = 1,
      stats = { ITEM_MOD_SPELL_POWER_SHORT = 40 },
      tooltip = { "Warlord's Dreadweave Robe", "Requires Warlord", "+40 Spell Power" } },
    [91002] = { id = 91002, name = "Robe of the Void", quality = 4, reqLevel = 60, equipLoc = "INVTYPE_CHEST",
      classID = 4, subclassID = 1,
      stats = { ITEM_MOD_SPELL_POWER_SHORT = 30 },
      tooltip = { "Robe of the Void", "Binds when equipped", "+30 Spell Power" } },
  },
}
`
	if err := os.WriteFile(filepath.Join(charSV, "WoWToolsForever.lua"), []byte(charLua), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(accSV, "WoWToolsForever.lua"), []byte(itemsLua), 0o644); err != nil {
		t.Fatal(err)
	}

	prices := sv.NewTable()
	for id, m := range map[string]int64{"91001": 50000, "91002": 60000} {
		a := sv.NewTable()
		a.Set("2000", int64(2))
		e := sv.NewTable()
		e.Set("m", m)
		e.Set("a", a)
		prices.Set(id, e)
	}

	// Keep engine off so the synthetic character does not need DB2 spell tables.
	savedUpg := computeUpgrades
	savedUpgF := computeUpgradesFiltered
	savedWrits := computeWrits
	savedRecipes := computeRecipes
	savedCons := computeConsumables
	savedPrices := loadPrices
	t.Cleanup(func() {
		computeUpgrades = savedUpg
		computeUpgradesFiltered = savedUpgF
		computeWrits = savedWrits
		computeRecipes = savedRecipes
		computeConsumables = savedCons
		loadPrices = savedPrices
	})
	loadPrices = func(string) (*sv.Table, error) { return prices, nil }
	runUpg := func(w, folder string, prices *sv.Table, f *upgrades.Filter) (any, error) {
		return upgrades.Compute(w, folder, prices, upgrades.Options{
			Top: 3, Sort: "gain", Engine: "off", Profile: "dungeon", Filter: f,
			PvPClassifier: upgrades.NewPvPClassifier(nil, nil),
		})
	}
	computeUpgrades = func(w, folder string, prices *sv.Table) (any, error) { return runUpg(w, folder, prices, nil) }
	computeUpgradesFiltered = runUpg
	computeWrits = func(string, string, *sv.Table) (any, error) { return decode(t, `{}`), nil }
	computeRecipes = func(string, string, *sv.Table) (any, error) { return decode(t, `{}`), nil }
	computeConsumables = func(string, string) (any, error) { return decode(t, `{}`), nil }

	// 1. Unfiltered AppData.lua includes PvP robe 91001.
	res, err := SyncWithOptions(wow, "", true, Options{})
	if err != nil {
		t.Fatalf("Sync unfiltered: %v", err)
	}
	if res.Get("result").(*pyjson.Obj).Get("written") != true {
		t.Fatal("expected initial AppData.lua write")
	}
	raw, err := os.ReadFile(filepath.Join(wow, "Interface", "AddOns", "WoWToolsForever", DataFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Warlord's Dreadweave Robe") {
		t.Fatalf("unfiltered AppData.lua missing PvP robe:\n%s", string(raw))
	}

	// 2. Excluding PvP removes 91001 from lists, shopping list, and tooltips, leaving 91002.
	pPvP := ComputeWithOptions(wow, "", Options{Filter: upgrades.NewFilter(nil, []string{upgrades.CategoryPvP})})
	if len(pPvP.Chars["Tester"].Tips.Lines(91001)) != 0 {
		t.Errorf("PvP item 91001 still has tooltip lines: %v", pPvP.Chars["Tester"].Tips.Lines(91001))
	}
	if len(pPvP.Chars["Tester"].Tips.Lines(91002)) == 0 {
		t.Errorf("non-PvP item 91002 missing tooltip lines after PvP filter")
	}
	res, err = SyncWithOptions(wow, "", true, Options{Filter: upgrades.NewFilter(nil, []string{upgrades.CategoryPvP})})
	if err != nil {
		t.Fatalf("Sync pvp filtered: %v", err)
	}
	if res.Get("result").(*pyjson.Obj).Get("written") != true {
		t.Fatal("expected AppData.lua rewrite when PvP filter enabled")
	}
	raw, err = os.ReadFile(filepath.Join(wow, "Interface", "AddOns", "WoWToolsForever", DataFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Warlord's Dreadweave Robe") {
		t.Fatalf("filtered AppData.lua still contains PvP robe:\n%s", string(raw))
	}
	if !strings.Contains(string(raw), "Robe of the Void") {
		t.Fatalf("filtered AppData.lua missing promoted non-PvP robe:\n%s", string(raw))
	}

	// 3. Excluding explicit item 91002 removes it from AppData.lua too.
	pBoth := ComputeWithOptions(wow, "", Options{Filter: upgrades.NewFilter([]int64{91002}, []string{upgrades.CategoryPvP})})
	if len(pBoth.Chars["Tester"].Tips.Lines(91002)) != 0 {
		t.Errorf("explicitly excluded item 91002 still has tooltip lines")
	}
}
