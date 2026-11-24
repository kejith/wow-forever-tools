package run

import (
	"testing"

	"github.com/kejith/wow-tools-forever/sim/engine"
)

func healRow(t *testing.T, tab *HealTable, id int) HealRow {
	t.Helper()
	for _, r := range tab.Rows {
		if r.SpellID == id {
			return r
		}
	}
	t.Fatalf("no row for spell %d in %+v", id, tab.Rows)
	return HealRow{}
}

// The heal table of the fixture priest of shield_test.go with the whole
// book (level 22, 100 healing, 5 % crit, Improved Power Word: Shield 2/3,
// Soul Warding). Hand-computed:
//
//	Lesser Heal 1  50 + 0.4*100 = 90, crits add 2.5 %: 92.25 per 2.5 s and 30 mana
//	Renew          5 ticks of 20 + 0.2*100 = 200 for 30 mana, a global cooldown to cast
//	Shield         100*1.2 + 0.1*100 = 130 absorbed for 85 mana (15 % cheaper)
//	regeneration   5 mp5 = 1 mana/s; spirit (13 + 60/4)/2 = 14 mana/s, 15 % of it while casting
func TestHealTable(t *testing.T) {
	in := shieldInput()
	in.Character.Spellbook = nil
	in.HealTable = true
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	tab := out.HealTable
	if tab == nil {
		t.Fatal("no heal table")
	}
	// Chains in the order they are learned, ranks from low to high; Dark
	// Sacrifice heals nothing and is not in it.
	order := []int{2050, 2052, 139, 2054, 2061, 17}
	if len(tab.Rows) != len(order) {
		t.Fatalf("rows: %+v", tab.Rows)
	}
	for i, id := range order {
		if tab.Rows[i].SpellID != id {
			t.Errorf("row %d is %d, want %d", i, tab.Rows[i].SpellID, id)
		}
	}

	lesser := healRow(t, tab, 2050)
	if lesser.Kind != "direct" || lesser.Rank != 1 || lesser.Level != 4 || lesser.BaseID != 2050 {
		t.Errorf("lesser heal: %+v", lesser)
	}
	almost(t, "lesser min", lesser.Min, 90, 1e-9)
	almost(t, "lesser max", lesser.Max, 90, 1e-9)
	almost(t, "lesser crit", lesser.CritPct, 5, 1e-9)
	almost(t, "lesser per cast", lesser.PerCast, 92.25, 1e-9)
	almost(t, "lesser hps", lesser.HPS, 36.9, 1e-9)
	almost(t, "lesser hpm", lesser.HPM, 3.075, 1e-9)
	almost(t, "lesser bonus", lesser.Bonus, 41, 1e-9)
	almost(t, "lesser coefficient", lesser.CoefPct, 41, 1e-9)
	if second := healRow(t, tab, 2052); second.Rank != 2 || second.BaseID != 2050 {
		t.Errorf("lesser heal rank 2: %+v", second)
	}

	renew := healRow(t, tab, 139)
	if renew.Kind != "hot" || renew.Min != 0 || renew.CritPct != 0 || renew.CastSec != 0 {
		t.Errorf("renew: %+v", renew)
	}
	almost(t, "renew tick", renew.PerTick, 40, 1e-9)
	almost(t, "renew ticks", renew.Ticks, 5, 1e-9)
	almost(t, "renew duration", renew.DurationSec, 15, 1e-9)
	almost(t, "renew per cast", renew.PerCast, 200, 1e-9)
	almost(t, "renew execute", renew.ExecuteSec, 1.5, 1e-9)
	almost(t, "renew hpm", renew.HPM, 200.0/30, 1e-9)
	almost(t, "renew coefficient", renew.CoefPct, 100, 1e-9)
	almost(t, "renew on the target", renew.OnTargetSec, 15, 1e-9)
	if lesser.OnTargetSec != 0 {
		t.Errorf("a heal is cast back to back: %g", lesser.OnTargetSec)
	}

	shield := healRow(t, tab, 17)
	if shield.Kind != "shield" || shield.CritPct != 0 || shield.Min != 0 {
		t.Errorf("shield: %+v", shield)
	}
	almost(t, "shield absorb", shield.Absorb, 130, 1e-9)
	almost(t, "shield mana", shield.Mana, 85, 1e-9)
	almost(t, "shield hpm", shield.HPM, 130.0/85, 1e-9)
	almost(t, "shield bonus", shield.Bonus, 10, 1e-9)
	// Soul Warding removes the cooldown, Weakened Soul stays.
	almost(t, "shield cooldown", shield.CooldownSec, 0, 1e-9)
	almost(t, "shield on the target", shield.OnTargetSec, 15, 1e-9)

	almost(t, "healing", tab.Healing, 100, 1e-9)
	almost(t, "crit", tab.CritPct, 5, 1e-9)
	almost(t, "mana", tab.ManaMax, 2000, 1e-9)
	almost(t, "gear mp5", tab.GearMP5, 5, 1e-9)
	almost(t, "regen casting", tab.RegenCastingPS, 1+14*0.15, 1e-9)
	almost(t, "regen idle", tab.RegenIdlePS, 15, 1e-9)
	almost(t, "casting share", tab.CastingRegenPct, 15, 1e-9)

	// Not asked for, not sent; a class without heals has none.
	in.HealTable = false
	if out := Run(in); out.HealTable != nil {
		t.Error("heal table without being asked")
	}
	lock := input()
	lock.HealTable = true
	if out := Run(lock); out.Error != "" || out.HealTable != nil {
		t.Errorf("warlock: %q %+v", out.Error, out.HealTable)
	}
}

// Renew is a choice like the shield: with the whole book the rotations are
// (no shield | shield) x (no Renew | Renew) x the four heal ranks.
func TestRenewIsOptional(t *testing.T) {
	in := shieldInput()
	in.Character.Spellbook = nil
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if out.Coverage.Candidates != 16 {
		t.Errorf("candidates = %d", out.Coverage.Candidates)
	}
	// A book without Renew still heals.
	in.Character.Spellbook = []int{2050}
	out = Run(in)
	if out.Error != "" || out.Baseline.Value <= 0 || rowOf(out.Baseline, 139) != nil {
		t.Errorf("without Renew: %q %+v", out.Error, out.Baseline)
	}
	almost(t, "lesser heal alone", out.Baseline.Value, 36.9, 1e-6)
}

// The character sheet's own regeneration (addon 1.12) replaces the class
// formula and the gear's mp5.
//
//	spirit regeneration 20 mana/s on the sheet, Meditation lets 15 % through:
//	3 mana/s while casting. The sheet says 4.5 while casting, so 1.5 mana/s
//	(7.5 mp5) come from gear and buffs. Outside the five-second rule 21.5.
//	Eight more spirit add 8 * 0.125 = 1 mana/s to the spirit regeneration.
func TestSheetRegeneration(t *testing.T) {
	in := shieldInput()
	in.Situations = false
	in.HealTable = true
	casting := 4.5
	in.Character.Stats.SpiritRegen = 20
	in.Character.Stats.CastingRegen = &casting
	tab := Run(in).HealTable
	if tab == nil {
		t.Fatal("no heal table")
	}
	almost(t, "mp5 from the sheet", tab.GearMP5, 7.5, 1e-9)
	almost(t, "regen casting", tab.RegenCastingPS, 4.5, 1e-9)
	almost(t, "regen idle", tab.RegenIdlePS, 21.5, 1e-9)

	st := buildState(in.Character, engine.Mods{CastingRegenPct: 15}).ApplyDelta(map[string]float64{"spi": 8, "mp5": 5})
	c, idle := st.Regen(in.Fight)
	almost(t, "with more spirit, casting", c, 4.5+1+1*0.15, 1e-9)
	almost(t, "with more spirit, idle", idle, 21.5+1+1, 1e-9)

	// Without the regeneration while casting the gear's mp5 stays; a sheet
	// that shows less than the spirit share has no mp5.
	in.Character.Stats.CastingRegen = nil
	almost(t, "only the spirit part", Run(in).HealTable.GearMP5, 5, 1e-9)
	low := 2.0
	in.Character.Stats.CastingRegen = &low
	almost(t, "never below zero", Run(in).HealTable.GearMP5, 0, 1e-9)
	// A snapshot without the sheet's numbers keeps the class formula.
	in.Character.Stats.SpiritRegen = 0
	almost(t, "class formula", Run(in).HealTable.RegenIdlePS, 15, 1e-9)
}
