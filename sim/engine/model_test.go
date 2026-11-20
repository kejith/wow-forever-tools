package engine

import (
	"testing"
)

// immolate-like: a direct part and a periodic part in one spell.
func twoPart() *Spell {
	return &Spell{
		ID: 348, BaseID: 348, Name: "Immolate", School: Shadow,
		CastMs: 2000, DurationMs: 15000, Cost: Cost{Mana: 100},
		ClassMask: [4]uint32{4, 0, 0, 0},
		Direct:    &Effect{Index: 0, BP: 50, Coeff: 0.2},
		Periodic:  &Effect{Index: 1, BP: 10, Coeff: 0.1, PeriodMs: 3000},
	}
}

func mask(bit uint32) [4]uint32 { return [4]uint32{bit, 0, 0, 0} }

// The client keeps the two amounts apart: op 0 changes the direct part,
// op 22 the periodic one, and percent modifiers multiply.
func TestDirectAndPeriodicModifiers(t *testing.T) {
	st := testState()
	plain := Shape(twoPart(), st, NewTarget(20, 2))
	almost(t, "direct", plain.DirectAvg, 50+0.2*100, 1e-9)
	almost(t, "tick", plain.TickValue, 10+0.1*100, 1e-9)

	st.Mods.SpellMods = []SpellMod{
		{Op: OpDamage, Pct: true, Value: 10, Mask: mask(4)},
		{Op: OpDamage, Pct: true, Value: 10, Mask: mask(4)},
		{Op: OpPeriodic, Pct: true, Value: 50, Mask: mask(4)},
		{Op: OpPeriodic, Pct: true, Value: 99, Mask: mask(8)}, // another spell
	}
	s := Shape(twoPart(), st, NewTarget(20, 2))
	almost(t, "direct with op 0", s.DirectAvg, 70*1.1*1.1, 1e-9)
	almost(t, "tick with op 22", s.TickValue, 20*1.5, 1e-9)
}

// Base-point modifiers change the effect's own value before spell power:
// op 3 effect 0, op 12 effect 1, op 8 every effect.
func TestPointsModifiers(t *testing.T) {
	st := testState()
	st.Mods.SpellMods = []SpellMod{
		{Op: OpPoints0, Pct: true, Value: 20, Mask: mask(4)},
		{Op: OpPoints1, Value: 5, Mask: mask(4)},
		{Op: OpAllPoints, Value: 10, Mask: mask(4)},
	}
	s := Shape(twoPart(), st, NewTarget(20, 2))
	almost(t, "direct", s.DirectAvg, (50+10)*1.2+0.2*100, 1e-9)
	almost(t, "tick", s.TickValue, (10+10+5)+0.1*100, 1e-9)
}

// Op 15 raises the crit damage bonus (Pandemic, Shadowform), op 7 the
// chance, op 16 the hit chance of the spells in its mask.
func TestCritDamageAndHitModifiers(t *testing.T) {
	st := testState()
	sb := bolt()
	sb.ClassMask = mask(1)
	base := Shape(sb, st, NewTarget(20, 2))
	almost(t, "crit bonus", base.CritBonus, 0.5, 1e-9)
	almost(t, "hit", base.HitChance, 0.95, 1e-9) // 6 % miss against +2, 1 % hit

	st.Mods.SpellMods = []SpellMod{
		{Op: OpCritDamage, Pct: true, Value: 100, Mask: mask(1)},
		{Op: OpCrit, Value: 5, Mask: mask(1)},
		{Op: OpHit, Value: 3, Mask: mask(1)},
	}
	s := Shape(sb, st, NewTarget(20, 2))
	almost(t, "crit bonus doubled", s.CritBonus, 1.0, 1e-9)
	almost(t, "crit chance", s.CritPct, 15, 1e-9)
	almost(t, "hit with op 16", s.HitChance, 0.98, 1e-9)
	// (80 + 60) * (1 + 15 % * 1.0) * 98 %
	almost(t, "damage", ResolveCast(sb, st, NewTarget(20, 2), nil).Damage, 140*1.15*0.98, 1e-9)
}

// Aura 72 (Shadowform): the school's spells cost less.
func TestSchoolCost(t *testing.T) {
	st := testState()
	st.Mods.SchoolCostPct[Shadow] = -50
	almost(t, "cost", Shape(bolt(), st, NewTarget(20, 2)).ManaCost, 70, 1e-9)
}

func drain() *Spell {
	return &Spell{
		ID: 689, BaseID: 689, Name: "Drain Life", School: Shadow,
		Channeled: true, DurationMs: 5000, Cost: Cost{Mana: 55},
		Periodic: &Effect{BP: 10, Coeff: 0.1, PeriodMs: 1000, Leech: 1},
	}
}

// A leech spell returns its damage as health: 5 ticks à 20 at 95 % hit.
func TestLeech(t *testing.T) {
	r := ResolveCast(drain(), testState(), NewTarget(20, 2), nil)
	almost(t, "damage", r.Damage, 95, 1e-9)
	almost(t, "leech", r.Leech, 95, 1e-9)
	half := drain()
	half.Periodic.Leech = 0.5
	almost(t, "half leech", ResolveCast(half, testState(), NewTarget(20, 2), nil).Leech, 47.5, 1e-9)
}

// Soul Siphon: the filler gains per DoT the rotation keeps up, up to Max.
func TestDotStack(t *testing.T) {
	st := testState()
	st.Mods.DotStacks = []DotStack{{
		PctPerDot: 4, Max: 1,
		Affects: map[int]bool{689: true},
		Counts:  map[int]bool{172: true, 980: true},
	}}
	tgt := NewTarget(20, 2)
	almost(t, "outside a rotation", Shape(drain(), st, tgt).TickValue, 20, 1e-9)
	agony := corruption()
	agony.ID, agony.BaseID = 980, 980
	ctx := ContextOf([]Action{{Kind: "dot", Spell: corruption()}, {Kind: "dot", Spell: agony}, {Kind: "filler", Spell: drain()}})
	almost(t, "one DoT counts at most", ShapeIn(drain(), st, tgt, ctx).TickValue, 20*1.04, 1e-9)
	st.Mods.DotStacks[0].Max = 3
	almost(t, "both DoTs", ShapeIn(drain(), st, tgt, ctx).TickValue, 20*1.08, 1e-9)
	almost(t, "the DoTs themselves do not gain", ShapeIn(corruption(), st, tgt, ctx).TickValue, 40, 1e-9)
}

// Self-healing feeds Life Tap. Fight 60 s, target +2, mana 500 (8.3333
// per second), Life Tap 100 health -> 120 mana, health 500:
//
//	Corruption 228/18 s, Drain Life 95 damage and health per 5 s for 55 mana.
//	wait = (11/12)*5/2 = 2.29167 s -> corruption cycle 20.29167 s:
//	share 0.073922, 11.2361 dps, 2.46407 mana/s; filler 0.926078.
//	rotMPS = 2.46407 + 0.926078*11 = 12.6509, deficit 4.3176
//	taps: x = 4.3176 / (120 + 11*1.5) = 0.031631 per second
//	health: 0.4*500/60 = 3.3333 plus the leech (0.926078 - 1.5x)*19 = 16.69
//	per second, so 0.2003 taps would be possible: not the limit.
//	filler = 0.926078 - 0.047447 = 0.878631; dps = 11.2361 + 0.878631*19 = 27.930
//
// Without the leech the health allows 1/30 taps per second and the same
// rotation would be health-bound; here that is still enough (x < 1/30).
func TestLeechFeedsLifeTap(t *testing.T) {
	st := testState()
	st.ManaMax = 500
	st.HealthMax = 500
	st.LifeTap = LifeTap{HealthPerCast: 100, ManaPerCast: 120}
	actions := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: drain()}, {Kind: "wand"}}
	fight := Fight{Seconds: 60, TargetDelta: 2}
	m := Evaluate(st, fight, actions, "dps")
	almost(t, "spent", m.ManaSpentPS, 12.6509, 0.001)
	almost(t, "dps", m.DPS, 27.930, 0.01)
	almost(t, "self healing", m.SelfHealPS, 0.878631*19, 0.01)

	// A long fight: the pool is gone, the taps carry the mana. With the
	// leech the rotation runs on; without it health caps the taps.
	long := Fight{Seconds: 600, TargetDelta: 2}
	with := Evaluate(st, long, actions, "dps")
	dry := drain()
	dry.Periodic.Leech = 0
	without := Evaluate(st, long, []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: dry}, {Kind: "wand"}}, "dps")
	tapsOf := func(m Metrics) float64 {
		for _, row := range m.Breakdown {
			if row.Kind == "lifetap" {
				return row.PerMin
			}
		}
		return 0
	}
	// without: (0.4*500/600) / 100 = 1/300 taps per second = 0.2 per minute
	almost(t, "taps without leech", tapsOf(without), 0.2, 1e-6)
	if tapsOf(with) < 10*tapsOf(without) {
		t.Errorf("leech should allow many more taps: %g vs %g", tapsOf(with), tapsOf(without))
	}
	if with.DPS <= without.DPS {
		t.Errorf("leech gained nothing: %g vs %g", with.DPS, without.DPS)
	}
}

// Health regeneration (Demon Armor) is health Life Tap may spend too.
func TestHP5FeedsLifeTap(t *testing.T) {
	st := testState()
	st.ManaMax = 500
	st.HealthMax = 500
	st.LifeTap = LifeTap{HealthPerCast: 100, ManaPerCast: 120}
	actions := []Action{{Kind: "filler", Spell: bolt()}, {Kind: "wand"}}
	fight := Fight{Seconds: 600, TargetDelta: 2}
	base := Evaluate(st, fight, actions, "dps")
	st.HP5 = 50 // 10 health per second = 0.1 taps per second more
	up := Evaluate(st, fight, actions, "dps")
	if up.DPS <= base.DPS {
		t.Errorf("hp5 gained nothing: %g vs %g", up.DPS, base.DPS)
	}
}

func wrack() *Spell {
	return &Spell{
		ID: 1316697, BaseID: 1316697, Name: "Wrack", School: Shadow,
		Channeled: true, DurationMs: 6000, Cost: Cost{Mana: 60},
		Periodic: &Effect{BP: 12, Coeff: 0.2, PeriodMs: 1000},
	}
}

// A channel that makes the DoT tick faster: for the share of the fight it
// is channeled, the DoT deals that much more. Fight 60 s, plenty of mana:
//
//	wait = (11/12)*6/2 = 2.75 s -> corruption cycle 20.75 s: share 0.072289,
//	10.9880 dps; Wrack on the rest 0.927711 with 6*32*0.95/6 = 30.4 dps.
//	Channel haste 50 %: corruption * (1 + 0.927711*0.5) = 16.0848
//	dps = 16.0848 + 0.927711*30.4 = 44.2872
func TestChannelHaste(t *testing.T) {
	st := testState()
	st.ManaMax = 1e9
	actions := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: wrack()}, {Kind: "wand"}}
	fight := Fight{Seconds: 60, TargetDelta: 2}
	plain := Evaluate(st, fight, actions, "dps")
	almost(t, "without haste", plain.DPS, 10.9880+0.927711*30.4, 0.001)

	st.Mods.ChannelHastes = []ChannelHaste{{Channel: 1316697, Pct: 50, Affects: map[int]bool{172: true}}}
	m := Evaluate(st, fight, actions, "dps")
	almost(t, "with haste", m.DPS, 44.2872, 0.001)
	for _, row := range m.Breakdown {
		if row.SpellID == 172 {
			almost(t, "the DoT carries the gain", row.Value, 16.0848, 0.001)
		}
	}
	// Another filler does not hasten anything.
	other := Evaluate(st, fight, []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: bolt()}}, "dps")
	st.Mods.ChannelHastes = nil
	almost(t, "bolt filler", other.DPS, Evaluate(st, fight, []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: bolt()}}, "dps").DPS, 1e-9)

	// The Monte-Carlo loop plays the same mechanic.
	st.Mods.ChannelHastes = []ChannelHaste{{Channel: 1316697, Pct: 50, Affects: map[int]bool{172: true}}}
	mc := RunMC(st, fight, actions, "dps", 2000, 1)
	if d := (mc.Mean - m.DPS) / m.DPS; d > 0.05 || d < -0.05 {
		t.Errorf("mc mean %g too far from the closed form %g", mc.Mean, m.DPS)
	}
}

// Attribute talents and Life Tap's spirit scaling work on item changes.
func TestApplyDeltaTalentScaling(t *testing.T) {
	st := &State{ManaPerInt: 15, HPPerSta: 10}
	st.LifeTap = LifeTap{HealthPerCast: 100, ManaPerCast: 120}
	st.Mods.IntellectPct = 10
	st.Mods.StaminaPct = 5
	st.Mods.SpiritPct = 20
	st.Mods.SPPerSpirit = 0.05
	st.Mods.HealPerSpirit = 0.25
	s := st.ApplyDelta(map[string]float64{"int": 10, "sta": 10, "spi": 10})
	almost(t, "mana", s.ManaMax, 10*1.1*15, 1e-9)
	almost(t, "health", s.HealthMax, 10*1.05*10, 1e-9)
	almost(t, "spirit", s.Spirit, 12, 1e-9)
	almost(t, "spell damage of spirit", s.SP[Shadow], 12*0.05, 1e-9)
	almost(t, "healing of spirit", s.Healing, 12*0.25, 1e-9)
	almost(t, "life tap health", s.LifeTap.HealthPerCast, 112, 1e-9)
	almost(t, "life tap mana", s.LifeTap.ManaPerCast, 132, 1e-9)

	// A character without Life Tap gets none from spirit.
	none := (&State{}).ApplyDelta(map[string]float64{"spi": 10})
	almost(t, "no life tap", none.LifeTap.ManaPerCast, 0, 1e-9)
}

// The Monte-Carlo loop spends the health the leech returns: with a leech
// filler the long fight sustains more than with the same filler dry.
func TestMCLeech(t *testing.T) {
	st := testState()
	st.ManaMax = 500
	st.HealthMax = 500
	st.LifeTap = LifeTap{HealthPerCast: 100, ManaPerCast: 120}
	st.Wand = Wand{DPS: 5, Skill: 100}
	fight := Fight{Seconds: 300, TargetDelta: 2}
	actions := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: drain()}, {Kind: "wand"}}
	dry := drain()
	dry.Periodic.Leech = 0
	dryActions := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: dry}, {Kind: "wand"}}
	with := RunMC(st, fight, actions, "dps", 500, 1)
	without := RunMC(st, fight, dryActions, "dps", 500, 1)
	if with.Mean <= without.Mean*1.2 {
		t.Errorf("leech should sustain the rotation: %g vs %g", with.Mean, without.Mean)
	}
	closed := Evaluate(st, fight, actions, "dps").DPS
	if d := (with.Mean - closed) / closed; d > 0.08 || d < -0.08 {
		t.Errorf("mc mean %g too far from the closed form %g", with.Mean, closed)
	}
}
