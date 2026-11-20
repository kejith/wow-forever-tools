package catalog

import (
	"testing"

	"github.com/kejith/wow-forever-tools/sim/engine"
)

func build(t *testing.T) *Catalog {
	t.Helper()
	cat, err := Build("../testdata", "9.9.9.9", "xxXX", "WARLOCK")
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func TestBuildDecodesSpells(t *testing.T) {
	cat := build(t)
	sb := cat.Spells[686]
	if sb == nil {
		t.Fatal("Shadow Bolt missing")
	}
	if sb.CastMs != 2500 || sb.School != engine.Shadow || sb.Cost.Mana != 100 {
		t.Errorf("Shadow Bolt decoded wrong: %+v", sb)
	}
	if sb.Direct == nil || sb.Direct.BP != 50 || sb.Direct.Coeff != 0.5 {
		t.Errorf("Shadow Bolt effect wrong: %+v", sb.Direct)
	}
	if sb.Levels.Base != 8 || sb.GCDMs != 1500 {
		t.Errorf("Shadow Bolt levels/gcd wrong: %+v", sb)
	}
	co := cat.Spells[172]
	if co == nil || co.Periodic == nil {
		t.Fatal("Corruption periodic missing")
	}
	if co.Periodic.PeriodMs != 3000 || co.Periodic.BP != 20 || co.DurationMs != 18000 {
		t.Errorf("Corruption decoded wrong: %+v dur %d", co.Periodic, co.DurationMs)
	}
	if co.Name != "Corruption" {
		t.Errorf("name = %q", co.Name)
	}
}

func TestRankChains(t *testing.T) {
	cat := build(t)
	if cat.NextRank[686] != 695 {
		t.Errorf("next rank of 686 = %d", cat.NextRank[686])
	}
	if cat.BaseOf[695] != 686 || cat.Spells[695].BaseID != 686 {
		t.Errorf("base of 695 = %d", cat.BaseOf[695])
	}
	known := map[int]bool{686: true, 695: true}
	if sp := cat.BestRank(686, known, 12); sp == nil || sp.ID != 686 {
		t.Errorf("best rank at 12 = %v", sp)
	}
	if sp := cat.BestRank(686, known, 20); sp == nil || sp.ID != 695 {
		t.Errorf("best rank at 20 = %v", sp)
	}
	if sp := cat.BestRank(686, map[int]bool{686: true}, 60); sp == nil || sp.ID != 686 {
		t.Errorf("best KNOWN rank = %v", sp)
	}
}

func TestAssumedSpellbook(t *testing.T) {
	cat := build(t)
	known := cat.AssumedSpellbook(12, nil)
	if !known[686] || !known[172] || !known[980] {
		t.Errorf("low ranks missing: %v", known)
	}
	if known[695] {
		t.Error("rank above the level is known")
	}
	known = cat.AssumedSpellbook(20, map[int]int{18271: 1})
	if !known[695] || !known[18271] {
		t.Errorf("level 20 book wrong: %v", known)
	}
}

func TestAurasKeptForTalents(t *testing.T) {
	cat := build(t)
	auras := cat.Auras[18271]
	if len(auras) != 1 || auras[0].Aura != 79 || auras[0].BP != 10 || auras[0].Misc0 != 32 {
		t.Errorf("talent auras wrong: %+v", auras)
	}
}

func TestMissingTableError(t *testing.T) {
	if _, err := Build("../testdata", "1.2.3.4", "xxXX", "WARLOCK"); err == nil {
		t.Error("missing tables should error")
	}
	if _, err := Build("../testdata", "9.9.9.9", "xxXX", "GNOME"); err == nil {
		t.Error("unknown class should error")
	}
}
