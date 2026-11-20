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
	if got := cat.Chains[686]; len(got) != 2 || got[0] != 686 || got[1] != 695 {
		t.Errorf("Shadow Bolt chain = %v", got)
	}
	if cat.BaseOf[695] != 686 || cat.Spells[695].BaseID != 686 {
		t.Errorf("base of 695 = %d", cat.BaseOf[695])
	}
	if cat.RankNumber(686) != 1 || cat.RankNumber(695) != 2 {
		t.Errorf("ranks = %d, %d", cat.RankNumber(686), cat.RankNumber(695))
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

// Forever leaves SupercedesSpell at 0 for the ranks of most class spells
// (the fixture mirrors that for the priest heals): the chain then comes
// from the shared name and spell family mask, in the order the ranks are
// learned. Without it the engine rated every character with rank 1 only.
func TestRankChainsWithoutSupersedeLinks(t *testing.T) {
	cat, err := Build("../testdata", "9.9.9.9", "xxXX", "PRIEST")
	if err != nil {
		t.Fatal(err)
	}
	if got := cat.Chains[2050]; len(got) != 2 || got[0] != 2050 || got[1] != 2052 {
		t.Errorf("Lesser Heal chain = %v", got)
	}
	if cat.RankNumber(2052) != 2 || cat.BaseOf[2052] != 2050 {
		t.Errorf("Lesser Heal rank 2: rank %d, base %d", cat.RankNumber(2052), cat.BaseOf[2052])
	}
	// Heal shares the fixture's family mask with Lesser Heal but not the
	// name: its own chain, rank 1.
	if cat.BaseOf[2054] != 2054 || cat.RankNumber(2054) != 1 {
		t.Errorf("Heal: base %d, rank %d", cat.BaseOf[2054], cat.RankNumber(2054))
	}
	book := map[int]bool{2050: true, 2052: true}
	if ranks := cat.KnownRanks(2050, book, 60); len(ranks) != 2 || ranks[1].ID != 2052 {
		t.Errorf("known ranks = %v", ranks)
	}
	if sp := cat.BestRank(2052, book, 60); sp == nil || sp.ID != 2052 {
		t.Errorf("best rank asked through rank 2 = %v", sp)
	}
}

// A translation may name the ranks of one spell differently (deDE: Smite
// rank 1 "Heilige Pein", rank 2 "Goettliche Pein"); the English names in
// the cache keep the chain together, the labels stay in the client's language.
func TestRankChainsUseTheEnglishNames(t *testing.T) {
	cat, err := Build("../testdata", "9.9.9.9", "zzZZ", "PRIEST")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Spells[2050].Name != "Kleines Heilen" || cat.Spells[2052].Name != "Geringes Heilen" {
		t.Fatalf("labels = %q, %q", cat.Spells[2050].Name, cat.Spells[2052].Name)
	}
	if got := cat.Chains[2050]; len(got) != 2 || got[1] != 2052 || cat.RankNumber(2052) != 2 {
		t.Errorf("chain = %v, rank %d", got, cat.RankNumber(2052))
	}
}

// A granted alternative version of a rank (AcquireMethod 3, same name, mask
// and level as Renew rank 1) shares that rank's number, never becomes the
// chain's first rank, and is not part of the assumed spellbook.
func TestGrantedVariantOfARank(t *testing.T) {
	cat, err := Build("../testdata", "9.9.9.9", "xxXX", "PRIEST")
	if err != nil {
		t.Fatal(err)
	}
	if got := cat.Chains[139]; len(got) != 3 || got[0] != 139 || got[1] != 425268 || got[2] != 6074 {
		t.Errorf("Renew chain = %v", got)
	}
	if cat.RankNumber(139) != 1 || cat.RankNumber(425268) != 1 || cat.RankNumber(6074) != 2 {
		t.Errorf("ranks = %d, %d, %d", cat.RankNumber(139), cat.RankNumber(425268), cat.RankNumber(6074))
	}
	if cat.BaseOf[425268] != 139 || cat.BaseOf[6074] != 139 {
		t.Errorf("bases = %d, %d", cat.BaseOf[425268], cat.BaseOf[6074])
	}
	known := cat.AssumedSpellbook(30, nil)
	if known[425268] {
		t.Error("the granted version is in the assumed spellbook")
	}
	if sp := cat.BestRank(139, known, 30); sp == nil || sp.ID != 6074 {
		t.Errorf("best Renew at 30 = %v", sp)
	}
	if sp := cat.BestRank(139, cat.AssumedSpellbook(20, nil), 20); sp == nil || sp.ID != 139 {
		t.Errorf("best Renew at 20 = %v", sp)
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

// The per-rank talent values come from the trait tables; a talent without
// a row falls back to the number the caller has.
func TestTalentValues(t *testing.T) {
	cat, err := Build("../testdata", "9.9.9.9", "xxXX", "PRIEST")
	if err != nil {
		t.Fatal(err)
	}
	if !cat.HasTraits {
		t.Fatal("trait tables not loaded")
	}
	for rank, want := range map[int]float64{1: 5, 2: 10, 3: 15} {
		if v, exact := cat.TalentValue(14521, 0, rank, 99); !exact || v != want {
			t.Errorf("Meditation rank %d = %g (exact %v)", rank, v, exact)
		}
	}
	if v, exact := cat.TalentValue(14521, 0, 4, 99); exact || v != 99 {
		t.Errorf("a rank the curve does not have = %g (exact %v)", v, exact)
	}
	if v, exact := cat.TalentValue(15473, 0, 1, 7); exact || v != 7 {
		t.Errorf("a talent without a trait row = %g (exact %v)", v, exact)
	}
	if cat.Name(14521) != "Meditation" {
		t.Errorf("name = %q", cat.Name(14521))
	}
}

// Leech effects (aura 53) carry how much of the damage returns as health,
// and the auras of a talent keep their effect index.
func TestLeechAndAuraIndex(t *testing.T) {
	cat, err := Build("../testdata", "9.9.9.9", "xxXX", "WARLOCK")
	if err != nil {
		t.Fatal(err)
	}
	drain := cat.Spells[689]
	if drain == nil || drain.Periodic == nil || drain.Periodic.Leech != 1 || !drain.Channeled {
		t.Fatalf("Drain Life = %+v", drain)
	}
	if bolt := cat.Spells[695]; bolt.Direct.Leech != 0 {
		t.Errorf("Shadow Bolt leech = %g", bolt.Direct.Leech)
	}
	auras := cat.Auras[7777]
	if len(auras) != 2 || auras[0].Index != 0 || auras[1].Index != 1 || auras[0].Misc0 != 22 || auras[1].Mask[0] != 1 {
		t.Errorf("auras = %+v", auras)
	}
	// Wrack is known through its talent only.
	known := cat.AssumedSpellbook(40, map[int]int{1316697: 1})
	if !known[1316697] || !known[689] {
		t.Errorf("assumed book = %v", known)
	}
	if cat.AssumedSpellbook(40, nil)[1316697] {
		t.Error("Wrack needs its talent")
	}
}
