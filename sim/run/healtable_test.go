package run

import "testing"

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
