package itemsets

import (
	"math"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/db2"
)

func mustTable(t *testing.T, name, csvData string) *db2.Table {
	t.Helper()
	tbl, err := db2.Read(name, strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return tbl
}

func sampleCatalog(t *testing.T) *Catalog {
	t.Helper()
	itemSet := mustTable(t, "ItemSet", strings.Join([]string{
		"ID,Name_lang,RequiredSkill,RequiredSkillRank,ItemID_0,ItemID_1,ItemID_2",
		"100,Dreadmist Raiment,0,0,1001,1002,1003",
		"200,Devilsaur Armor,0,0,2001,2002,0",
		"300,Bloodvine Garb,197,300,3001,3002,3003",
		"400,Necropile Raiment,0,0,4001,4002,0",
	}, "\n"))
	itemSetSpell := mustTable(t, "ItemSetSpell", strings.Join([]string{
		"ID,ChrSpecID,SpellID,TraitSubTreeID,Threshold,ItemSetID",
		"1,0,9001,0,2,100",
		"2,0,9002,0,3,100",
		"3,0,9003,0,2,200",
		"4,0,9004,0,3,300",
		"5,0,9005,0,2,400",
		"6,0,9006,0,2,400",
	}, "\n"))
	spellEffect := mustTable(t, "SpellEffect", strings.Join([]string{
		"DifficultyID,SpellID,EffectIndex,Effect,EffectAura,EffectBasePointsF,EffectMiscValue_0,EffectMiscValue_1",
		"0,9001,0,6,13,18,126,0",    // +18 spell damage
		"0,9001,1,6,135,18,126,0",   // +18 healing
		"0,9002,0,6,108,10,0,0",     // +10% spell mod (non-sheet)
		"0,9003,0,6,54,2,0,0",       // +2% melee hit
		"0,9003,1,6,55,2,0,0",       // +2% spell hit
		"0,9003,2,6,99,20,0,0",      // +20 melee AP
		"0,9003,3,6,124,20,0,0",     // +20 ranged AP
		"0,9004,0,6,57,2,0,0",       // +2% spell crit
		"0,9005,0,6,189,5,224,0",    // +5 hit rating -> 0.5% hit
		"0,9006,0,35,161,20,0,0",    // +20 hp5 (party aura effect 35)
		"0,9006,1,6,85,8,0,0",       // +8 mp5
	}, "\n"))
	classOpt := mustTable(t, "SpellClassOptions", strings.Join([]string{
		"SpellID,SpellClassSet",
		"9002,5",
	}, "\n"))
	spellName := mustTable(t, "SpellName", strings.Join([]string{
		"ID,Name_lang",
		"9001,Increase Spell Dam 18",
		"9002,Improved Shadow",
		"9003,Devilsaur Precision",
		"9004,Bloodvine Crit",
		"9005,Increased Hit 0.5%",
		"9006,Regeneration",
	}, "\n"))
	itemSparse := mustTable(t, "ItemSparse", strings.Join([]string{
		"ID,ItemSet",
		"1004,100", // linked only via ItemSparse.ItemSet
	}, "\n"))
	return Build(itemSet, itemSetSpell, spellEffect, classOpt, spellName, itemSparse)
}

func TestBuildAndDecode(t *testing.T) {
	cat := sampleCatalog(t)
	if got := cat.SetOf(1004); got == nil || got.ID != 100 {
		t.Fatalf("ItemSparse fallback failed: SetOf(1004) = %v", got)
	}
	s100 := cat.Sets[100]
	if len(s100.Bonuses) != 2 {
		t.Fatalf("expected 2 bonuses on set 100, got %d", len(s100.Bonuses))
	}
	if s100.Bonuses[0].Stats["spDamage"] != 18 || s100.Bonuses[0].Stats["healing"] != 18 {
		t.Errorf("unexpected 2pc stats: %+v", s100.Bonuses[0].Stats)
	}
	if !s100.Bonuses[1].HasNonSheetMods || s100.Bonuses[1].SpellClassSet != 5 {
		t.Errorf("expected non-sheet mod with classSet 5 on 3pc, got %+v", s100.Bonuses[1])
	}

	// Duplicate item IDs must count only once.
	active := cat.ActiveBonuses([]int64{1001, 1001}, nil)
	if len(active) != 0 {
		t.Errorf("duplicate item IDs should not activate 2pc bonus, got %+v", active)
	}

	// Two unique pieces activate 2pc; three activate both 2pc and 3pc.
	active2 := cat.ActiveBonuses([]int64{1001, 1002}, nil)
	if len(active2) != 1 || active2[0].SpellID != 9001 {
		t.Fatalf("expected 9001 active, got %+v", active2)
	}
	active3 := cat.ActiveBonuses([]int64{1001, 1002, 1004}, nil)
	if len(active3) != 2 {
		t.Fatalf("expected 2 bonuses active, got %+v", active3)
	}
	if ns := NonSheetSpellIDs(active3); len(ns) != 1 || ns[0] != 9002 {
		t.Errorf("NonSheetSpellIDs = %v, want [9002]", ns)
	}

	// Diff and DeltaStats from 2pc -> 0pc (losing 2pc).
	gained, lost := DiffBonuses(active2, nil)
	if len(gained) != 0 || len(lost) != 1 {
		t.Fatalf("DiffBonuses(2pc -> 0pc) = gained %d, lost %d", len(gained), len(lost))
	}
	delta := DeltaStats(gained, lost)
	if delta["spDamage"] != -18 || delta["healing"] != -18 {
		t.Errorf("DeltaStats = %+v, want -18 spDamage and -18 healing", delta)
	}
}

func TestSkillRequirementAndRatings(t *testing.T) {
	cat := sampleCatalog(t)
	// Bloodvine requires Tailoring (197) 300.
	if b := cat.ActiveBonuses([]int64{3001, 3002, 3003}, map[int]int{197: 250}); len(b) != 0 {
		t.Errorf("Bloodvine should not activate with Tailoring 250, got %+v", b)
	}
	b := cat.ActiveBonuses([]int64{3001, 3002, 3003}, map[int]int{197: 300})
	if len(b) != 1 || b[0].Stats["spellCrit"] != 2 {
		t.Fatalf("Bloodvine should activate +2 spellCrit at Tailoring 300, got %+v", b)
	}
	// ScaleStats should map spellCrit to crit for casters, but NOT for physical roles.
	if cs := ScaleStats(b, false, "shadow"); cs["crit"] != 2 {
		t.Errorf("caster ScaleStats = %+v, want crit=2", cs)
	}
	if ps := ScaleStats(b, true, ""); ps["crit"] != 0 {
		t.Errorf("physical ScaleStats should ignore spell-only crit, got %+v", ps)
	}

	// Necropile 2pc: Aura 189 hit rating 5 -> 0.5% meleeHit & spellHit, plus effect 35 hp5=20, mp5=8.
	necro := cat.ActiveBonuses([]int64{4001, 4002}, nil)
	d := DeltaStats(necro, nil)
	if math.Abs(d["spellHit"]-0.5) > 1e-9 || math.Abs(d["meleeHit"]-0.5) > 1e-9 {
		t.Errorf("Aura 189 hit rating = %+v, want 0.5", d)
	}
	if d["hp5"] != 20 || d["mp5"] != 8 {
		t.Errorf("hp5/mp5 = %+v, want hp5=20, mp5=8", d)
	}

	// Devilsaur 2pc: AP 20 + RAP 20 should not double-count.
	dev := cat.ActiveBonuses([]int64{2001, 2002}, nil)
	ds := ScaleStats(dev, true, "")
	if ds["ap"] != 20 || ds["rap"] != 20 || ds["hit"] != 2 {
		t.Errorf("Devilsaur ScaleStats = %+v, want ap=20, rap=20, hit=2", ds)
	}
}
