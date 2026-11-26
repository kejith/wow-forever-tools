package enchants

import (
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/db2"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

func readTable(t *testing.T, name, csvText string) *db2.Table {
	t.Helper()
	tbl, err := db2.Read(name, strings.NewReader(csvText))
	if err != nil {
		t.Fatalf("db2.Read(%s): %v", name, err)
	}
	return tbl
}

func TestParseEnchantID(t *testing.T) {
	// Explicit enchantId takes precedence.
	rec := sv.NewTable()
	rec.Set("enchantId", int64(2504))
	rec.Set("link", "|cff0070dd|Hitem:1234:1900:0:0|h[Staff]|h|r")
	if got := ParseEnchantID(rec); got != 2504 {
		t.Fatalf("ParseEnchantID explicit = %d, want 2504", got)
	}

	// Falls back to parsing item hyperlink when enchantId is absent.
	rec2 := sv.NewTable()
	rec2.Set("link", "|cffa335ee|Hitem:19356:2504:0:0:0:0:0:0|h[Staff of the Shadow Flame]|h|r")
	if got := ParseEnchantID(rec2); got != 2504 {
		t.Fatalf("ParseEnchantID link = %d, want 2504", got)
	}

	// Unenchanted item returns 0.
	rec3 := sv.NewTable()
	rec3.Set("link", "|cff1eff00|Hitem:19356:0:0:0|h[Staff]|h|r")
	if got := ParseEnchantID(rec3); got != 0 {
		t.Fatalf("ParseEnchantID unenchanted = %d, want 0", got)
	}
}

func TestBuildAndDecodeEnchants(t *testing.T) {
	sie := readTable(t, "SpellItemEnchantment", strings.Join([]string{
		"ID,Name_lang,Duration,Effect_0,Effect_1,Effect_2,EffectPointsMin_0,EffectPointsMin_1,EffectPointsMin_2,EffectArg_0,EffectArg_1,EffectArg_2,MinLevel,ItemLevelMin,RequiredSkillID,RequiredSkillRank",
		"2504,Spell Power +30,0,3,0,0,0,0,0,22747,0,0,0,0,0,0",
		"8211,Spell Power +$k1,0,5,0,0,55,0,0,45,0,0,0,0,0,0",
		"8212,Healing Spells +$k1 and Damage Spells +$k2,0,5,5,0,100,34,0,41,42,0,0,0,0,0",
		"1900,Crusader,0,1,0,0,0,0,0,20007,0,0,0,0,0,0",
		"664,Scope (+7 Damage),0,2,0,0,7,0,0,0,0,0,0,0,0,0",
		"783,Armor +$k1,0,4,0,0,70,0,0,0,0,0,0,0,0,0",
		"2628,Brilliant Wizard Oil,1800,3,0,0,0,0,0,25122,0,0,0,0,0,0",
	}, "\n"))

	spellEffect := readTable(t, "SpellEffect", strings.Join([]string{
		"DifficultyID,SpellID,Effect,EffectAura,EffectBasePointsF,EffectMiscValue_0",
		"0,22749,53,0,0,2504",
		"0,22747,6,13,30,126",
		"0,22747,6,135,30,0",
		"0,1248607,53,0,0,8211",
		"0,1248636,53,0,0,8212",
		"0,20034,53,0,0,1900",
		"0,12460,53,0,0,664",
		"0,20015,53,0,0,783",
		"0,23143,53,0,0,2504", // QA duplicate — must be filtered out
		"0,25122,6,13,36,126",
	}, "\n"))

	sei := readTable(t, "SpellEquippedItems", strings.Join([]string{
		"ID,SpellID,EquippedItemClass,EquippedItemInvTypes,EquippedItemSubclass",
		"1,22749,2,0,189939", // 1H + 2H weapons
		"2,1248607,2,0,1378", // 2H weapons only
		"3,1248636,2,0,1378", // 2H weapons only
		"4,20034,2,0,189939", // melee weapons
		"5,12460,2,0,262156", // Bow/Gun/Crossbow
		"6,20015,4,65536,31", // Cloak
		"7,23143,2,0,189939", // QA
	}, "\n"))

	sla := readTable(t, "SkillLineAbility", strings.Join([]string{
		"ID,SkillLine,Spell,TrivialSkillLineRankLow",
		"1,333,22749,300",
		"2,333,1248607,300",
		"3,333,1248636,300",
		"4,333,20034,300",
		"5,333,20015,255",
		"6,333,23143,1",
	}, "\n"))

	reagents := readTable(t, "SpellReagents", strings.Join([]string{
		"ID,SpellID,Reagent_0,Reagent_1,ReagentCount_0,ReagentCount_1",
		"1,22749,14344,16203,4,12",
	}, "\n"))

	itemEffect := readTable(t, "ItemEffect", "ID,SpellID\n10,12460\n")
	itemXEffect := readTable(t, "ItemXItemEffect", "ID,ItemID,ItemEffectID\n1,10548,10\n")

	spellName := readTable(t, "SpellName", strings.Join([]string{
		"ID,Name_lang",
		"22749,Enchant Weapon - Spell Power",
		"1248607,Enchant 2H Weapon - Mighty Spell Power",
		"1248636,Enchant 2H Weapon - Mighty Healing Power",
		"20034,Enchant Weapon - Crusader",
		"12460,Sniper Scope",
		"20015,Enchant Cloak - Superior Defense",
		"23143,QAEnchant Weapon Spell Power",
	}, "\n"))

	itemSparse := readTable(t, "ItemSparse", strings.Join([]string{
		"ID,Display_lang,OverallQualityID,RequiredLevel,RequiredSkill,RequiredSkillRank,AllowableClass,BuyPrice,VendorStackCount",
		"10548,Sniper Scope,2,40,0,0,-1,0,1",
		"14344,Large Brilliant Shard,3,0,0,0,-1,0,1",
		"16203,Greater Eternal Essence,2,0,0,0,-1,0,1",
	}, "\n"))

	cat := Build(sie, spellEffect, sei, sla, reagents, itemEffect, itemXEffect, spellName, spellName, itemSparse)

	// Verify $k1 substitution and Type 5 decoding.
	mightySP := cat.ByID[8211]
	if mightySP == nil || mightySP.Name != "Spell Power +55" || mightySP.Stats["sp"] != 55 {
		t.Fatalf("mightySP = %+v", mightySP)
	}
	mightyHeal := cat.ByID[8212]
	if mightyHeal == nil || mightyHeal.Stats["healing"] != 100 || mightyHeal.Stats["spDamage"] != 34 {
		t.Fatalf("mightyHeal = %+v", mightyHeal)
	}
	if crusader := cat.ByID[1900]; crusader == nil || !crusader.IsProc {
		t.Fatalf("crusader = %+v, want IsProc=true", crusader)
	}
	if scope := cat.ByID[664]; scope == nil || scope.Stats["weaponDmg"] != 7 {
		t.Fatalf("scope = %+v, want weaponDmg=7", scope)
	}
	if cloak := cat.ByID[783]; cloak == nil || cloak.Name != "Armor +70" || cloak.Stats["armor"] != 70 {
		t.Fatalf("cloak = %+v", cloak)
	}
	// Temporary enchant is in ByID (for active oil lookup) but NOT in permanent Options.
	if oil := cat.ByID[2628]; oil == nil || oil.Duration != 1800 || oil.Stats["spDamage"] != 36 {
		t.Fatalf("oil = %+v", oil)
	}
	for _, opt := range cat.Options {
		if opt.SpellID == 23143 {
			t.Fatalf("QA enchant 23143 should be filtered out")
		}
		if opt.EnchantID == 2628 {
			t.Fatalf("temporary enchant 2628 should not be in permanent options")
		}
	}

	// Verify slot/item matching rules.
	var opt2H, optScope, optSP *Option
	for _, opt := range cat.Options {
		switch opt.SpellID {
		case 1248607:
			opt2H = opt
		case 12460:
			optScope = opt
		case 22749:
			optSP = opt
		}
	}
	if opt2H == nil || !opt2H.TwoHandOnly {
		t.Fatalf("opt2H = %+v, want TwoHandOnly", opt2H)
	}
	if !MatchesItem(opt2H, 16, "INVTYPE_2HWEAPON", 2, 10) || MatchesItem(opt2H, 16, "INVTYPE_WEAPONMAINHAND", 2, 7) {
		t.Fatalf("2H enchant match failed")
	}
	if optScope == nil || !optScope.RangedOnly {
		t.Fatalf("optScope = %+v, want RangedOnly", optScope)
	}
	if !MatchesItem(optScope, 18, "INVTYPE_RANGED", 2, 2) || MatchesItem(optScope, 18, "INVTYPE_RANGEDRIGHT", 2, 19) {
		t.Fatalf("scope must match Bow (2) and not Wand (19)")
	}
	if optSP == nil || len(optSP.Reagents) != 2 {
		t.Fatalf("optSP = %+v, want 2 reagents", optSP)
	}
	if MatchesItem(optSP, 17, "INVTYPE_HOLDABLE", 4, 0) {
		t.Fatalf("weapon enchant must not match held-in-offhand")
	}
	if !MatchesItem(optSP, 17, "INVTYPE_WEAPONOFFHAND", 2, 15) {
		t.Fatalf("weapon enchant must match off-hand dagger")
	}

	// Verify StatsForSlot remapping.
	if oh := StatsForSlot(map[string]float64{"weaponDmg": 5}, 17); oh["offhandDmg"] != 5 || oh["weaponDmg"] != 0 {
		t.Fatalf("StatsForSlot(17) = %+v", oh)
	}
	if rng := StatsForSlot(map[string]float64{"weaponDmg": 7, "meleeHit": 3}, 18); rng["rangedDmg"] != 7 || rng["rangedHit"] != 3 {
		t.Fatalf("StatsForSlot(18) = %+v", rng)
	}
}
