package talents

import (
	"testing"

	"github.com/kejith/wow-forever-tools/sim/catalog"
	"github.com/kejith/wow-forever-tools/sim/engine"
)

func decode(t *testing.T, ranks map[int]int) engine.Mods {
	t.Helper()
	cat, err := catalog.Build("../testdata", "9.9.9.9", "xxXX", "WARLOCK")
	if err != nil {
		t.Fatal(err)
	}
	mods, err := Decode(cat, ranks)
	if err != nil {
		t.Fatal(err)
	}
	return mods
}

func TestGenericSchoolDamage(t *testing.T) {
	mods := decode(t, map[int]int{18271: 1})
	if mods.SchoolDamagePct[engine.Shadow] != 10 {
		t.Errorf("shadow pct = %g", mods.SchoolDamagePct[engine.Shadow])
	}
	if mods.SchoolDamagePct[engine.Fire] != 0 {
		t.Error("fire should be untouched")
	}
}

func TestGenericSpellMod(t *testing.T) {
	mods := decode(t, map[int]int{9999: 1})
	if len(mods.SpellMods) != 1 {
		t.Fatalf("mods = %+v", mods.SpellMods)
	}
	m := mods.SpellMods[0]
	if m.Op != engine.OpCastTime || m.Pct || m.Value != -500 || m.Mask[0] != 1 {
		t.Errorf("mod = %+v", m)
	}
}

// The overrides win over the generic decode and scale with the rank.
func TestOverrides(t *testing.T) {
	mods := decode(t, map[int]int{18175: 2, 18182: 1, 427712: 1})
	if mods.HitFlatByBase[172] != 4 || mods.HitFlatByBase[980] != 4 {
		t.Errorf("suppression = %v", mods.HitFlatByBase)
	}
	if mods.LifeTapPct != 10 {
		t.Errorf("life tap = %g", mods.LifeTapPct)
	}
	if mods.DotCritSchools&(1<<uint(engine.Shadow)) == 0 {
		t.Error("pandemic missing")
	}
}

func TestZeroRankIgnored(t *testing.T) {
	mods := decode(t, map[int]int{18271: 0})
	if mods.SchoolDamagePct[engine.Shadow] != 0 {
		t.Error("rank 0 must not count")
	}
}
