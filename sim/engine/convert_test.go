package engine

import "testing"

func shieldSpell() *Spell {
	return &Spell{
		ID: 17, BaseID: 17, Name: "Power Word: Shield", School: Holy,
		Cost: Cost{Mana: 100}, CooldownMs: 4000, DurationMs: 30000,
		ClassMask: mask(1),
		Absorb:    &Effect{BP: 100, Coeff: 0.1},
	}
}

func lesserHeal() *Spell {
	return &Spell{
		ID: 2050, BaseID: 2050, Name: "Lesser Heal", School: Holy,
		CastMs: 2500, Cost: Cost{Mana: 30}, IsHeal: true,
		Direct: &Effect{BP: 50, Coeff: 0.4},
	}
}

// Dark Sacrifice as the fixtures have it: 5 ticks of 60 mana plus the
// spirit, for 5 ticks of 40 health, every ten minutes.
func sacrifice(healthPerTick float64) Action {
	return Action{
		Kind: "mana",
		Spell: &Spell{
			ID: 1277324, BaseID: 1277324, Name: "Dark Sacrifice", School: Shadow,
			DurationMs: 15000, CooldownMs: 600000, GivesMana: true,
			Energize: &Effect{BP: 60, PeriodMs: 3000, Index: 1},
		},
		HealthCost:    &Effect{BP: healthPerTick, PeriodMs: 3000},
		ManaPerSpirit: 1,
	}
}

func healerState() *State {
	st := &State{Level: 20, Healing: 100, Spirit: 60, ManaMax: 300}
	st.Crit[Holy] = 5
	return st
}

// A shield absorbs its base points (which talents raise) plus a tenth of the
// healing bonus. It counts as healing, never crits, and the talents that
// change its cost and cooldown apply.
func TestAbsorbShape(t *testing.T) {
	st := healerState()
	st.Crit[Holy] = 50
	st.Mods.HealingPct = 10 // "healing done" does not raise a shield
	st.Mods.SpellMods = []SpellMod{
		{Op: OpPoints0, Pct: true, Value: 20, Mask: mask(1)},   // Improved Power Word: Shield
		{Op: OpCooldown, Value: -4000, Mask: mask(1)},          // Soul Warding
		{Op: OpCost, Pct: true, Value: -15, Mask: mask(1)},     // Soul Warding
		{Op: OpPoints0, Pct: true, Value: 50, Mask: mask(128)}, // another spell's talent
	}
	sp := shieldSpell()
	if !sp.Heals() || sp.IsHeal {
		t.Fatal("a shield counts for the healer metrics without being a heal")
	}
	r := ResolveCast(sp, st, NewTarget(20, 0), nil)
	almost(t, "absorb", r.Healing, 100*1.2+0.1*100, 1e-9)
	almost(t, "damage", r.Damage, 0, 1e-9)
	almost(t, "cost", r.ManaCost, 85, 1e-9)
	almost(t, "cooldown", r.CooldownSec, 0, 1e-9)
	almost(t, "cooldown without the talent", CooldownSec(sp, healerState()), 4, 1e-9)
}

// Weakened Soul sets the shield's pace on one target, whatever its cooldown:
// with the 2.5 s heal as the filler it comes due 0.9*2.5/2 = 1.125 s late on
// average, so one shield per 16.125 s.
func TestShieldCycle(t *testing.T) {
	st := healerState()
	st.ManaMax = 100000
	actions := []Action{
		{Kind: "cooldown", Spell: shieldSpell(), CycleSec: 15},
		{Kind: "heal", Spell: lesserHeal()},
	}
	m := Evaluate(st, Fight{Seconds: 60}, actions, "hps")
	shield := m.Breakdown[0]
	if !shield.Absorb || shield.Kind != "cooldown" {
		t.Fatalf("shield row = %+v", shield)
	}
	almost(t, "shield hps", shield.Value, 110/16.125, 1e-6)
	fillerShare := 1 - 1.5/16.125
	almost(t, "hps", m.HPS, 110/16.125+fillerShare*92.25/2.5, 1e-6)
	if m.Breakdown[1].Absorb {
		t.Error("a heal is not an absorb")
	}

	// The Monte-Carlo run plays the same cycle.
	mc := RunMC(st, Fight{Seconds: 600}, actions, "hps", 200, 1)
	if mc.Mean < m.HPS*0.97 || mc.Mean > m.HPS*1.03 {
		t.Errorf("mc %g vs closed form %g", mc.Mean, m.HPS)
	}
}

// A healer short of mana trades health for it when the mana heals more than
// the health costs. Hand-computed: 5*60 + 60 spirit = 360 mana for 200
// health, one cast in a 60 s fight.
//
//	Lesser Heal: 92.25 per 2.5 s for 30 mana -> 36.9 hps, 12 mana/s
//	budget 300/60 = 5 mana/s -> without: share 5/12, 15.375 hps
//	with: (5 + 360/60)/12 = 0.91667 of the fight -> 33.825 hps - 200/60
func TestManaConversionForAHealer(t *testing.T) {
	st := healerState()
	heal := Action{Kind: "heal", Spell: lesserHeal()}
	fight := Fight{Seconds: 60}

	without := Evaluate(st, fight, []Action{heal}, "hps")
	almost(t, "hps without", without.HPS, 15.375, 1e-9)
	almost(t, "ttoom without", *without.TTOOMSec, 25, 1e-9)

	with := Evaluate(st, fight, []Action{heal, sacrifice(40)}, "hps")
	almost(t, "hps with", with.HPS, 0.916666667*36.9-200.0/60, 1e-6)
	// The mana of one cast stretches the pool: (300 + 360) / 12.
	almost(t, "ttoom with", *with.TTOOMSec, 55, 1e-9)
	var row *Row
	for i := range with.Breakdown {
		if with.Breakdown[i].Kind == "mana" {
			row = &with.Breakdown[i]
		}
	}
	if row == nil {
		t.Fatalf("no conversion row: %+v", with.Breakdown)
	}
	almost(t, "casts per minute", row.PerMin, 1, 1e-9)
	almost(t, "mana per second", row.ManaGain, 6, 1e-9)
	almost(t, "health per second", row.HealthCost, 200.0/60, 1e-9)
	if row.SpellID != 1277324 || row.Value != 0 {
		t.Errorf("row = %+v", row)
	}

	// Health that costs more healing than the mana buys is not traded.
	costly := Evaluate(st, fight, []Action{heal, sacrifice(400)}, "hps")
	almost(t, "hps, too costly", costly.HPS, 15.375, 1e-9)
	for _, r := range costly.Breakdown {
		if r.Kind == "mana" {
			t.Errorf("unexpected conversion: %+v", r)
		}
	}

	// Enough mana: nothing to convert.
	st.ManaMax = 5000
	rich := Evaluate(st, fight, []Action{heal, sacrifice(40)}, "hps")
	almost(t, "hps with mana", rich.HPS, 36.9, 1e-9)
	if len(rich.Breakdown) != 1 {
		t.Errorf("breakdown = %+v", rich.Breakdown)
	}

	// The time-to-OOM metric names the cast that stretches the mana.
	st.ManaMax = 300
	ttoom := Evaluate(st, fight, []Action{heal, sacrifice(400)}, "ttoom")
	almost(t, "ttoom metric", ttoom.Value, 55, 1e-9)
	found := false
	for _, r := range ttoom.Breakdown {
		found = found || r.Kind == "mana"
	}
	if !found {
		t.Errorf("ttoom breakdown = %+v", ttoom.Breakdown)
	}
}

// The Monte-Carlo run trades health for mana the same way: the mana arrives
// over the effect's duration and the health comes off the healing.
func TestMCManaConversion(t *testing.T) {
	st := healerState()
	heal := Action{Kind: "heal", Spell: lesserHeal()}
	fight := Fight{Seconds: 60}
	without := RunMC(st, fight, []Action{heal}, "hps", 300, 1)
	with := RunMC(st, fight, []Action{heal, sacrifice(40)}, "hps", 300, 1)
	costly := RunMC(st, fight, []Action{heal, sacrifice(400)}, "hps", 300, 1)
	if with.Mean <= without.Mean*1.5 {
		t.Errorf("conversion should pay: %g vs %g", with.Mean, without.Mean)
	}
	closed := Evaluate(st, fight, []Action{heal, sacrifice(40)}, "hps")
	if with.Mean < closed.HPS*0.9 || with.Mean > closed.HPS*1.1 {
		t.Errorf("mc %g vs closed form %g", with.Mean, closed.HPS)
	}
	almost(t, "too costly: not traded", costly.Mean, without.Mean, 1e-9)

	oom := RunMC(st, fight, []Action{heal}, "ttoom", 50, 1)
	oomWith := RunMC(st, fight, []Action{heal, sacrifice(40)}, "ttoom", 50, 1)
	if oomWith.Mean <= oom.Mean+20 {
		t.Errorf("ttoom with the conversion %g, without %g", oomWith.Mean, oom.Mean)
	}
}

// A damage dealer has a health budget instead: 40 % of the pool over the
// fight. One cast costs 200 health; with 300 health only 120 may go.
func TestManaConversionHealthBudget(t *testing.T) {
	st := testState()
	st.ManaMax = 300
	st.Spirit = 60
	st.HealthMax = 300
	fight := Fight{Seconds: 60, TargetDelta: 2}
	actions := []Action{{Kind: "filler", Spell: bolt()}, sacrifice(40)}
	m := Evaluate(st, fight, actions, "dps")
	var row *Row
	for i := range m.Breakdown {
		if m.Breakdown[i].Kind == "mana" {
			row = &m.Breakdown[i]
		}
	}
	if row == nil {
		t.Fatalf("no conversion: %+v", m.Breakdown)
	}
	almost(t, "health per second", row.HealthCost, 0.4*300/60, 1e-9)
	almost(t, "mana per second", row.ManaGain, 0.4*300/60*360/200, 1e-9)

	st.HealthMax = 5000 // the cooldown caps it at one cast
	m = Evaluate(st, fight, actions, "dps")
	for i := range m.Breakdown {
		if m.Breakdown[i].Kind == "mana" {
			almost(t, "casts per minute", m.Breakdown[i].PerMin, 1, 1e-9)
		}
	}
}

// Spirit Tap: with a chance per kill the spirit doubles for 15 s and half
// of the regeneration continues while casting. Hand-computed for 60 spirit
// (6.5 + 60/8 = 14 mana/s idle, 15 % of it while casting):
//
//	one kill per minute: a quarter of a kill per buff duration -> up 40% * 0.25
//	buffed: 6.5 + 120/8 = 21.5 idle, 65 % of it while casting = 13.975
func TestKillProcRegen(t *testing.T) {
	st := healerState()
	st.RegenBasePS, st.RegenSpiritFactor = 6.5, 0.125
	st.MP5 = 5
	st.Mods.CastingRegenPct = 15
	casting, idle := st.Regen(Fight{Seconds: 60, KillsPerMinute: 1})
	almost(t, "casting, no talent", casting, 1+2.1, 1e-9)
	almost(t, "idle, no talent", idle, 1+14, 1e-9)

	st.Mods.KillProcs = []KillProc{{ChancePct: 40, DurationSec: 15, SpiritPct: 100, CastingRegenPct: 50}}
	casting, idle = st.Regen(Fight{Seconds: 60})
	almost(t, "casting, no kills", casting, 3.1, 1e-9)
	almost(t, "uptime, no kills", st.KillProcUptime(Fight{Seconds: 60}), 0, 1e-9)

	fight := Fight{Seconds: 60, KillsPerMinute: 1}
	casting, idle = st.Regen(fight)
	almost(t, "uptime", st.KillProcUptime(fight), 0.1, 1e-9)
	almost(t, "casting", casting, 3.1+0.1*(13.975-2.1), 1e-9)
	almost(t, "idle", idle, 15+0.1*(21.5-14), 1e-9)

	// Two kills per buff duration: up unless both miss the 40 %.
	almost(t, "uptime, many kills", st.KillProcUptime(Fight{Seconds: 60, KillsPerMinute: 8}), 1-0.6*0.6, 1e-9)

	// More spirit on the gear counts double while the buff is up.
	more := st.ApplyDelta(map[string]float64{"spi": 8})
	c2, _ := more.Regen(fight)
	almost(t, "spirit delta", c2-casting, 0.15+0.1*(2*0.65-0.15), 1e-9)

	m := Evaluate(st, fight, []Action{{Kind: "heal", Spell: lesserHeal()}}, "hps")
	almost(t, "budget", m.ManaBudgetPS, 300.0/60+casting, 1e-9)
	almost(t, "reported uptime", m.KillBuffPct, 10, 1e-9)
}
