package run

import "testing"

func damageRow(t *testing.T, tab *DamageTable, id int) DamageRow {
	t.Helper()
	for _, r := range tab.Rows {
		if r.SpellID == id {
			return r
		}
	}
	t.Fatalf("no row for spell %d in %+v", id, tab.Rows)
	return DamageRow{}
}

// The damage table of the golden warlock (level 20, 100 shadow spell power,
// 10 % crit, 95 % hit against +2 levels). Hand-computed:
//
//	Corruption     6 ticks of 20 + 0.2*100 = 40; 240 * 0.95 = 228 for 50 mana and a global cooldown
//	Curse of Agony 8 ticks of 15 + 0.1*100 = 25; 200 * 0.95 = 190 for 40 mana
//	Shadow Bolt 1  50 + 0.5*100 = 100; * 1.05 (crits) * 0.95 = 99.75 per 2.5 s and 100 mana
//	Shadow Bolt 2  80 + 0.6*100 = 140; * 1.05 * 0.95 = 139.65 per 3 s and 140 mana
//	regeneration   10 mp5 = 2 mana/s while casting, 6.25 more outside the five-second rule
func TestDamageTable(t *testing.T) {
	if out := Run(input()); out.DamageTable != nil {
		t.Error("a damage table without being asked for")
	}
	in := input()
	in.DamageTable = true
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	tab := out.DamageTable
	if tab == nil {
		t.Fatal("no damage table")
	}
	// Chains in the order they are learned, ranks from low to high.
	order := []int{172, 686, 695, 980}
	if len(tab.Rows) != len(order) {
		t.Fatalf("rows: %+v", tab.Rows)
	}
	for i, id := range order {
		if tab.Rows[i].SpellID != id {
			t.Errorf("row %d is %d, want %d", i, tab.Rows[i].SpellID, id)
		}
	}

	bolt := damageRow(t, tab, 695)
	if bolt.Kind != "direct" || bolt.Rank != 2 || bolt.Level != 16 || bolt.BaseID != 686 || bolt.School != "shadow" || !bolt.Top {
		t.Errorf("shadow bolt rank 2: %+v", bolt)
	}
	almost(t, "bolt min", bolt.Min, 140, 1e-9)
	almost(t, "bolt max", bolt.Max, 140, 1e-9)
	almost(t, "bolt crit", bolt.CritPct, 10, 1e-9)
	almost(t, "bolt hit", bolt.HitPct, 95, 1e-9)
	almost(t, "bolt per cast", bolt.PerCast, 139.65, 1e-9)
	almost(t, "bolt dps", bolt.DPS, 46.55, 1e-9)
	almost(t, "bolt dpm", bolt.DPM, 0.9975, 1e-9)
	almost(t, "bolt bonus", bolt.Bonus, 59.85, 1e-9)
	almost(t, "bolt coefficient", bolt.CoefPct, 59.85, 1e-9)
	if first := damageRow(t, tab, 686); first.Top || first.Rank != 1 || first.CastSec != 2.5 {
		t.Errorf("shadow bolt rank 1: %+v", first)
	}

	corruption := damageRow(t, tab, 172)
	if corruption.Kind != "dot" || corruption.Min != 0 || corruption.TickCrits || corruption.CastSec != 0 || !corruption.Top {
		t.Errorf("corruption: %+v", corruption)
	}
	almost(t, "corruption tick", corruption.PerTick, 40, 1e-9)
	almost(t, "corruption ticks", corruption.Ticks, 6, 1e-9)
	almost(t, "corruption duration", corruption.DurationSec, 18, 1e-9)
	almost(t, "corruption per cast", corruption.PerCast, 228, 1e-9)
	almost(t, "corruption per second of casting", corruption.DPS, 152, 1e-9)
	almost(t, "corruption dpm", corruption.DPM, 4.56, 1e-9)
	almost(t, "corruption coefficient", corruption.CoefPct, 114, 1e-9)
	almost(t, "agony per cast", damageRow(t, tab, 980).PerCast, 190, 1e-9)

	// The character's numbers the rows were computed with.
	if len(tab.Schools) != 1 || tab.Schools[0].School != "shadow" || tab.Schools[0].Power != 100 || tab.Schools[0].CritPct != 10 {
		t.Errorf("schools: %+v", tab.Schools)
	}
	if tab.TargetDelta != 2 || tab.ManaMax != 1000 || tab.HealthMax != 500 {
		t.Errorf("table: %+v", tab)
	}
	almost(t, "miss", tab.MissPct, 5, 1e-9)
	almost(t, "regeneration while casting", tab.RegenCastingPS, 2, 1e-9)
	almost(t, "regeneration outside the rule", tab.RegenIdlePS, 8.25, 1e-9)
	almost(t, "wand", tab.WandDPS, 19.364, 1e-3)
	almost(t, "tap mana", tab.TapMana, 120, 1e-9)
	almost(t, "tap health", tab.TapHealth, 100, 1e-9)

	// A talent is in the numbers; a heal is no row.
	in.Character.Talents = map[string]int{"18271": 1} // +10 % shadow damage
	out = Run(in)
	almost(t, "bolt with the talent", damageRow(t, out.DamageTable, 695).PerCast, 139.65*1.1, 1e-6)
	p := shieldInput()
	p.Character.Spellbook = nil
	p.Metric = "dps"
	p.DamageTable = true
	out = Run(p)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	for _, r := range out.DamageTable.Rows {
		if r.SpellID == 2050 || r.SpellID == 139 || r.SpellID == 17 {
			t.Errorf("a heal in the damage table: %+v", r)
		}
	}
}
