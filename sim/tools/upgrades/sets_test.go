package upgrades

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/db2"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/itemsets"
)

func buildTestSetCatalog(t *testing.T) *itemsets.Catalog {
	t.Helper()
	read := func(name, data string) *db2.Table {
		tbl, err := db2.Read(name, strings.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return tbl
	}
	itemSet := read("ItemSet", strings.Join([]string{
		"ID,Name_lang,RequiredSkill,RequiredSkillRank,ItemID_0,ItemID_1,ItemID_2",
		"10,Shadowweave Test,0,0,5001,5002,5003",
		"20,Synergy Pair,0,0,6001,6002,0",
	}, "\n"))
	itemSetSpell := read("ItemSetSpell", strings.Join([]string{
		"ID,ChrSpecID,SpellID,TraitSubTreeID,Threshold,ItemSetID",
		"1,0,9101,0,2,10",
		"2,0,9102,0,2,20",
	}, "\n"))
	spellEffect := read("SpellEffect", strings.Join([]string{
		"DifficultyID,SpellID,EffectIndex,Effect,EffectAura,EffectBasePointsF,EffectMiscValue_0,EffectMiscValue_1",
		"0,9101,0,6,13,30,126,0",  // +30 spell damage
		"0,9101,1,6,135,30,126,0", // +30 healing
		"0,9102,0,6,13,50,126,0",  // +50 spell damage
		"0,9102,1,6,135,50,126,0", // +50 healing
	}, "\n"))
	spellName := read("SpellName", strings.Join([]string{
		"ID,Name_lang",
		"9101,Spell Power 30",
		"9102,Spell Power 50",
	}, "\n"))
	return itemsets.Build(itemSet, itemSetSpell, spellEffect, nil, spellName, nil)
}

// writeUpgradeFixture creates a minimal fake WoW folder with Auctionator prices,
// WoWToolsForever item export, and a level-60 Warlock character snapshot.
func writeUpgradeFixture(t *testing.T, equippedLua, exportLua, pricesLua string) string {
	t.Helper()
	wow := t.TempDir()
	accSV := filepath.Join(wow, "WTF", "Account", "TESTACC", "SavedVariables")
	charSV := filepath.Join(wow, "WTF", "Account", "TESTACC", "TestRealm", "Testlock", "SavedVariables")
	if err := os.MkdirAll(accSV, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(charSV, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(accSV, "Auctionator.lua"), []byte(pricesLua), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(accSV, "WoWToolsForever.lua"), []byte(exportLua), 0o644); err != nil {
		t.Fatal(err)
	}
	charLua := `WoWToolsForever_Char = {
		["updated"] = 1700000000,
		["character"] = {
			["name"] = "Testlock",
			["classFile"] = "WARLOCK",
			["class"] = "Warlock",
			["level"] = 60,
			["money"] = 1000000,
			["stats"] = {
				["healthMax"] = 3000,
				["powerMax"] = 4000,
				["intellect"] = 200,
				["spirit"] = 100,
				["spellHit"] = 0,
				["spellDamage"] = { ["shadow"] = 200 },
				["spellCrit"] = { ["shadow"] = 10 },
			},
		},
		["equipment"] = {` + equippedLua + `},
		["bags"] = {},
		["bank"] = { ["containers"] = {} },
		["skills"] = { ["skills"] = {} },
		["talents"] = { ["class"] = {} },
	}`
	if err := os.WriteFile(filepath.Join(charSV, "WoWToolsForever.lua"), []byte(charLua), 0o644); err != nil {
		t.Fatal(err)
	}
	return wow
}

func TestSetBonusBreakActivateAndMultiPieceSynergy(t *testing.T) {
	cat := buildTestSetCatalog(t)

	// Equipped:
	//   Slot 1 (Head):  5001 (Shadowweave Test piece 1, +20 SP)
	//   Slot 3 (Shldr): 5002 (Shadowweave Test piece 2, +20 SP)  --> 2pc active (+30 SP)!
	//   Slot 7 (Legs):  7001 (non-set, +20 SP)
	//   Slot 10 (Hands):7002 (non-set, +20 SP)
	equippedLua := `
		{ ["slot"] = 1, ["id"] = 5001, ["name"] = "Shadowweave Hood", ["quality"] = 3, ["equipLoc"] = "INVTYPE_HEAD", ["stats"] = { ["ITEM_MOD_SPELL_POWER_SHORT"] = 20 } },
		{ ["slot"] = 3, ["id"] = 5002, ["name"] = "Shadowweave Mantle", ["quality"] = 3, ["equipLoc"] = "INVTYPE_SHOULDER", ["stats"] = { ["ITEM_MOD_SPELL_POWER_SHORT"] = 20 } },
		{ ["slot"] = 7, ["id"] = 7001, ["name"] = "Plain Pants", ["quality"] = 3, ["equipLoc"] = "INVTYPE_LEGS", ["stats"] = { ["ITEM_MOD_SPELL_POWER_SHORT"] = 20 } },
		{ ["slot"] = 10, ["id"] = 7002, ["name"] = "Plain Gloves", ["quality"] = 3, ["equipLoc"] = "INVTYPE_HAND", ["stats"] = { ["ITEM_MOD_SPELL_POWER_SHORT"] = 20 } },
	`
	// AH candidates:
	//   8001 (Head, non-set, +35 SP): +15 raw SP over 5001, but breaks 2pc (-30 SP) -> net -15! Must NOT be recommended.
	//   8002 (Head, non-set, +60 SP): +40 raw SP over 5001, breaks 2pc (-30 SP) -> net +10 with set loss badge.
	//   6001 (Legs, Synergy Pair 1, +15 SP): -5 raw SP vs 7001 alone.
	//   6002 (Hands, Synergy Pair 2, +15 SP): -5 raw SP vs 7002 alone.
	//   Together 6001 + 6002 unlock 2pc (+50 SP) -> net +40 SP! Package optimizer MUST pick both!
	exportLua := `WoWToolsForever_Items = {
		["updated"] = 1700000000,
		["items"] = {
			[8001] = { ["name"] = "Broken Set Trap Hood", ["quality"] = 3, ["reqLevel"] = 60, ["classID"] = 4, ["subclassID"] = 1, ["equipLoc"] = "INVTYPE_HEAD", ["stats"] = { ["ITEM_MOD_SPELL_POWER_SHORT"] = 35 } },
			[8002] = { ["name"] = "Strong Hood", ["quality"] = 3, ["reqLevel"] = 60, ["classID"] = 4, ["subclassID"] = 1, ["equipLoc"] = "INVTYPE_HEAD", ["stats"] = { ["ITEM_MOD_SPELL_POWER_SHORT"] = 60 } },
			[6001] = { ["name"] = "Synergy Leggings", ["quality"] = 3, ["reqLevel"] = 60, ["classID"] = 4, ["subclassID"] = 1, ["equipLoc"] = "INVTYPE_LEGS", ["stats"] = { ["ITEM_MOD_SPELL_POWER_SHORT"] = 15 } },
			[6002] = { ["name"] = "Synergy Gloves", ["quality"] = 3, ["reqLevel"] = 60, ["classID"] = 4, ["subclassID"] = 1, ["equipLoc"] = "INVTYPE_HAND", ["stats"] = { ["ITEM_MOD_SPELL_POWER_SHORT"] = 15 } },
		},
	}`
	prices := sv.NewTable()
	for _, p := range []struct {
		id    string
		price int64
	}{{"8001", 50000}, {"8002", 100000}, {"6001", 50000}, {"6002", 50000}} {
		entry := sv.NewTable()
		entry.Set("m", p.price)
		a := sv.NewTable()
		a.Set("2000", int64(1))
		entry.Set("a", a)
		prices.Set(p.id, entry)
	}

	wow := writeUpgradeFixture(t, equippedLua, exportLua, "")
	res, err := Compute(wow, "Testlock", prices, Options{
		Engine:     "off",
		Profile:    "dungeon",
		Top:        5,
		SetCatalog: cat,
	})
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}

	// Base gear value should include the +30 SP 2pc bonus (80 item SP + 30 set SP = 110).
	if got := res.Get("setValue").(float64); got != 110 {
		t.Errorf("setValue = %g, want 110 (80 gear + 30 set bonus)", got)
	}

	// Head slot: 8001 (-15 net) must NOT appear; 8002 (+10 net) MUST appear with a set loss change.
	found8001, found8002 := false, false
	for _, s := range res.Get("slots").([]any) {
		so := s.(*pyjson.Obj)
		if so.Get("slot").(int64) != 1 {
			continue
		}
		for _, c := range so.Get("candidates").([]any) {
			co := c.(*pyjson.Obj)
			id := co.Get("id").(int64)
			if id == 8001 {
				found8001 = true
			}
			if id == 8002 {
				found8002 = true
				if g := co.Get("gain").(float64); g != 10 {
					t.Errorf("8002 gain = %g, want 10 (+40 raw - 30 broken set)", g)
				}
				sc, ok := co.Get("setChanges").([]any)
				if !ok || len(sc) != 1 || sc[0].(*pyjson.Obj).Get("kind") != "loss" {
					t.Errorf("8002 setChanges = %v, want 1 loss entry", sc)
				}
			}
		}
	}
	if found8001 {
		t.Error("8001 should not be listed as an upgrade because it breaks the 2pc +30 SP bonus")
	}
	if !found8002 {
		t.Error("8002 should be listed as a +10 upgrade")
	}

	// Package optimizer MUST discover the 6001 + 6002 synergy pair (+40 SP) alongside 8002 (+10 SP) = +50 SP total!
	pkg := res.Get("package").(*pyjson.Obj)
	pkgIDs := map[int64]bool{}
	for _, it := range pkg.Get("items").([]any) {
		pkgIDs[it.(*pyjson.Obj).Get("id").(int64)] = true
	}
	if !pkgIDs[6001] || !pkgIDs[6002] {
		t.Errorf("package must include synergistic 2pc set pair 6001+6002, got IDs: %v", pkgIDs)
	}
	if g := pkg.Get("gain").(float64); g != 50 {
		t.Errorf("package gain = %g, want 50 (+10 from 8002, +40 from 6001+6002 2pc set)", g)
	}
}

func TestSetTrackerActivateSinglePiece(t *testing.T) {
	cat := buildTestSetCatalog(t)
	c := sv.NewTable()
	ch := sv.NewTable()
	ch.Set("classFile", "WARLOCK")
	c.Set("character", ch)
	eq := sv.NewTable()
	it := sv.NewTable()
	it.Set("slot", int64(1))
	it.Set("id", int64(5001))
	eq.Set(int64(1), it)
	c.Set("equipment", eq)

	tr := NewSetTracker(c, cat)
	if len(tr.BaseBonuses) != 0 {
		t.Fatalf("expected 0 base bonuses with 1 piece, got %d", len(tr.BaseBonuses))
	}
	sd := tr.ScaleDelta([]int64{3}, []int64{5002})
	if sd["sp"] != 30 {
		t.Errorf("activating 2pc should yield +30 sp ScaleDelta, got %+v", sd)
	}
	ed, _, hasMod := tr.EngineChange([]int64{3}, []int64{5002})
	if ed["spDamage"] != 30 || ed["healing"] != 30 || hasMod {
		t.Errorf("EngineChange = %+v, hasMod=%v, want spDamage=30, healing=30, hasMod=false", ed, hasMod)
	}
}
