package engine

import (
	"math"
	"math/rand"
	"testing"
)

func almost(t *testing.T, name string, got, want, eps float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Errorf("%s = %g, want %g (±%g)", name, got, want, eps)
	}
}

// The classic boss example: weapon skill 300 against defense 315.
func TestMeleeTableBoss(t *testing.T) {
	tab := MeleeTable(300, 315, 25, false)
	almost(t, "miss", tab.Miss, 8, 1e-9)          // 6 + 5*0.4
	almost(t, "dodge", tab.Dodge, 6.5, 1e-9)      // 5 + 15*0.1
	almost(t, "glancing", tab.Glancing, 40, 1e-9) // capped
	almost(t, "penalty", tab.GlancePenalty, 0.65, 1e-9)
	almost(t, "crit", tab.Crit, 22, 1e-9) // 25 - 15*0.2
	if sum := tab.Miss + tab.Dodge + tab.Glancing + tab.Crit; sum > 100 {
		t.Errorf("table overflows: %g", sum)
	}
}

func TestMeleeTableEqualLevel(t *testing.T) {
	tab := MeleeTable(300, 300, 10, false)
	almost(t, "miss", tab.Miss, 5, 1e-9)
	almost(t, "glancing", tab.Glancing, 0, 1e-9)
	almost(t, "crit", tab.Crit, 10, 1e-9)
}

func TestRangedMiss(t *testing.T) {
	almost(t, "miss +5", RangedMiss(300, 305), 5.5, 1e-9)
	almost(t, "miss +15", RangedMiss(300, 315), 8, 1e-9)
}

func TestApplyDeltaChains(t *testing.T) {
	st := &State{ManaPerInt: 15, CritPerInt: 0.02, HPPerSta: 10}
	s := st.ApplyDelta(map[string]float64{"int": 30, "sta": 5, "sp": 10, "spShadow": 7, "hit": 2})
	almost(t, "mana", s.ManaMax, 450, 1e-9)
	almost(t, "crit shadow", s.Crit[Shadow], 0.6, 1e-9)
	almost(t, "health", s.HealthMax, 50, 1e-9)
	almost(t, "sp shadow", s.SP[Shadow], 17, 1e-9)
	almost(t, "sp fire", s.SP[Fire], 10, 1e-9)
	almost(t, "healing", s.Healing, 10, 1e-9)
	almost(t, "hit", s.Hit, 2, 1e-9)
	// The original is untouched.
	almost(t, "original", st.SP[Shadow], 0, 1e-9)
}

func testState() *State {
	st := &State{Level: 20, Hit: 1}
	st.SP[Shadow] = 100
	st.Crit[Shadow] = 10
	return st
}

func bolt() *Spell {
	return &Spell{
		ID: 695, BaseID: 686, Name: "Shadow Bolt", School: Shadow,
		CastMs: 3000, Cost: Cost{Mana: 140},
		Levels: Levels{Base: 16},
		Direct: &Effect{BP: 80, Coeff: 0.6},
	}
}

func corruption() *Spell {
	return &Spell{
		ID: 172, BaseID: 172, Name: "Corruption", School: Shadow,
		DurationMs: 18000, Cost: Cost{Mana: 50},
		Periodic: &Effect{BP: 20, Coeff: 0.2, PeriodMs: 3000},
	}
}

// Hand-computed: (80 + 0.6*100) * (1 + 10%*0.5) * hit 95% = 139.65.
func TestResolveCastDirect(t *testing.T) {
	st := testState()
	r := ResolveCast(bolt(), st, NewTarget(20, 2), nil)
	almost(t, "damage", r.Damage, 139.65, 1e-9)
	almost(t, "execute", r.ExecuteSec, 3, 1e-9)
	almost(t, "mana", r.ManaCost, 140, 1e-9)
}

// Hand-computed: 6 ticks * (20 + 0.2*100) * 95% = 228; DoTs do not crit.
func TestResolveCastPeriodic(t *testing.T) {
	st := testState()
	r := ResolveCast(corruption(), st, NewTarget(20, 2), nil)
	almost(t, "damage", r.Damage, 228, 1e-9)
	almost(t, "duration", r.DurationSec, 18, 1e-9)
	almost(t, "execute (gcd)", r.ExecuteSec, 1.5, 1e-9)
}

func TestDotCritWithPandemic(t *testing.T) {
	st := testState()
	st.Mods.DotCritSchools = 1 << uint(Shadow)
	r := ResolveCast(corruption(), st, NewTarget(20, 2), nil)
	almost(t, "damage", r.Damage, 228*1.05, 1e-9)
}

func TestSpellModsCastTimeAndMask(t *testing.T) {
	st := testState()
	st.Mods.SpellMods = []SpellMod{{Op: OpCastTime, Value: -500, Mask: [4]uint32{1, 0, 0, 0}}}
	sb := bolt()
	sb.ClassMask = [4]uint32{1, 0, 0, 0}
	r := ResolveCast(sb, st, NewTarget(20, 2), nil)
	almost(t, "execute", r.ExecuteSec, 2.5, 1e-9)
	// A spell outside the mask is untouched.
	co := corruption()
	co.ClassMask = [4]uint32{2, 0, 0, 0}
	r = ResolveCast(co, st, NewTarget(20, 2), nil)
	almost(t, "execute corruption", r.ExecuteSec, 1.5, 1e-9)
}

// The hit floor: one percent miss always remains.
func TestHitFloor(t *testing.T) {
	st := testState()
	st.Hit = 5 // miss vs +2 would be 6 - 5 = 1 = the floor
	a := ResolveCast(bolt(), st, NewTarget(20, 2), nil).Damage
	st.Hit = 50
	b := ResolveCast(bolt(), st, NewTarget(20, 2), nil).Damage
	almost(t, "capped hit", a, b, 1e-9)
}

// A channel occupies its full duration and deals its ticks: 3 ticks à
// (15 + 0.15*100) = 90, hit 95%, DoTs do not crit.
func TestResolveCastChanneled(t *testing.T) {
	st := testState()
	flay := &Spell{
		ID: 15407, BaseID: 15407, Name: "Mind Flay", School: Shadow,
		Channeled: true, DurationMs: 3000, Cost: Cost{Mana: 45},
		Periodic: &Effect{BP: 15, Coeff: 0.15, PeriodMs: 1000},
	}
	r := ResolveCast(flay, st, NewTarget(20, 2), nil)
	almost(t, "execute", r.ExecuteSec, 3, 1e-9)
	almost(t, "damage", r.Damage, 85.5, 1e-9)
}

// Direct heals crit and use the healing bonus; they never miss.
func TestResolveCastHeal(t *testing.T) {
	st := testState()
	st.Healing = 100
	st.Crit[Holy] = 10
	heal := &Spell{
		ID: 2050, BaseID: 2050, Name: "Lesser Heal", School: Holy,
		CastMs: 2500, Cost: Cost{Mana: 30}, IsHeal: true,
		Direct: &Effect{BP: 50, Coeff: 0.4},
	}
	r := ResolveCast(heal, st, NewTarget(20, 3), nil)
	almost(t, "healing", r.Healing, (50+0.4*100)*1.05, 1e-9)
	almost(t, "no damage", r.Damage, 0, 1e-9)
}

// Meditation: spirit regeneration enters the budget while casting.
func TestCastingRegen(t *testing.T) {
	st := testState()
	st.ManaMax = 1000
	st.Spirit = 60
	st.RegenBasePS = 6.5
	st.RegenSpiritFactor = 0.125
	st.Mods.CastingRegenPct = 15
	m := Evaluate(st, Fight{Seconds: 60, TargetDelta: 2}, []Action{{Kind: "filler", Spell: bolt()}}, "dps")
	// budget = 1000/60 + (6.5 + 7.5)*0.15 = 16.667 + 2.1
	almost(t, "budget", m.ManaBudgetPS, 18.7667, 0.001)
	// ttoom = 1000 / (140/3 - 2.1)
	if m.TTOOMSec == nil {
		t.Fatal("ttoom missing")
	}
	almost(t, "ttoom", *m.TTOOMSec, 1000/(140.0/3-2.1), 0.001)
}

// The Monte-Carlo seam: the mean of sampled casts approaches the expectation.
func TestResolveCastSampledMean(t *testing.T) {
	st := testState()
	want := ResolveCast(bolt(), st, NewTarget(20, 2), nil).Damage
	rng := rand.New(rand.NewSource(1))
	n := 200000
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += ResolveCast(bolt(), st, NewTarget(20, 2), rng).Damage
	}
	got := sum / float64(n)
	if math.Abs(got-want)/want > 0.02 {
		t.Errorf("sampled mean %g too far from expectation %g", got, want)
	}
}

// The full cycle, hand-computed in the comments (fight 60 s, target +2):
//
//	Corruption: 228 dmg / 18 s for 50 mana, 1.5 s to cast.
//	Shadow Bolt filler: 139.65 per 3 s cast for 140 mana.
//
// Corruption falls off while a bolt is being cast: the recast waits half a
// cast on average for the share of the timeline the filler holds, which
// stretches its cycle. Mana forces Life Tap and wanding, and both take
// their time from the filler; the DoT keeps its uptime.
func TestEvaluateManaClipping(t *testing.T) {
	st := testState()
	st.ManaMax = 1000
	st.HealthMax = 500
	st.MP5 = 10
	st.LifeTap = LifeTap{HealthPerCast: 100, ManaPerCast: 120}
	st.Wand = Wand{DPS: 20, Skill: 100}
	st.RangedCrit = 5

	actions := []Action{
		{Kind: "dot", Spell: corruption()},
		{Kind: "filler", Spell: bolt()},
		{Kind: "wand"},
	}
	m := Evaluate(st, Fight{Seconds: 60, TargetDelta: 2}, actions, "dps")

	// Before the wait: corruption 1/12 of the timeline, filler 11/12.
	// wait = (11/12)*3/2 = 1.375 s, so the cycle is 19.375 s (k = 0.929032):
	// corruption share 0.077419, 11.7677 dps, 2.58065 mana/s;
	// filler share 0.922581 at 46.55 dps and 46.6667 mana/s per unit.
	// rotMPS = 2.58065 + 0.922581*46.6667 = 45.6344
	almost(t, "manaSpentPs", m.ManaSpentPS, 45.6344, 0.001)
	// budget = 1000/60 + 10/5 = 18.6667
	almost(t, "manaBudgetPs", m.ManaBudgetPS, 18.6667, 0.001)
	// ttoom = 1000 / (45.6344 - 2) = 22.9177
	if m.TTOOMSec == nil {
		t.Fatal("ttoom missing")
	}
	almost(t, "ttoom", *m.TTOOMSec, 22.9177, 0.01)
	// Health caps the taps: 0.4*500 / (60*100) = 1/30 taps per second
	// (4 mana/s, 5 % of the time). The filler gets the mana that is left:
	// (18.6667 + 4 - 2.58065) / 46.6667 = 0.430415 of the timeline.
	// wand = 1 - 0.077419 - 0.430415 - 0.05 = 0.442166, wandDPS = 20*0.94*1.03 = 19.364.
	// dps = 11.7677 + 0.430415*46.55 + 0.442166*19.364 = 40.3656
	almost(t, "dps", m.DPS, 40.3656, 0.01)
	for _, row := range m.Breakdown {
		if row.Kind == "dot" {
			almost(t, "the DoT keeps its uptime", row.Value, 11.7677, 0.001)
		}
	}

	// Without mana pressure the full rotation runs.
	st.ManaMax = 1e9
	m2 := Evaluate(st, Fight{Seconds: 60, TargetDelta: 2}, actions, "dps")
	// 11.7677 + 0.922581*46.55 = 54.7139
	almost(t, "unconstrained dps", m2.DPS, 54.7139, 0.001)
	if m2.TTOOMSec != nil && *m2.TTOOMSec < 60 {
		t.Errorf("unexpected early ttoom %g", *m2.TTOOMSec)
	}
	if m2.DPS <= m.DPS {
		t.Error("mana pressure should lower dps")
	}
}

// More spell power must help, and hit past the floor must not.
func TestEvaluateDeltas(t *testing.T) {
	st := testState()
	st.ManaMax = 1e9
	actions := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: bolt()}}
	fight := Fight{Seconds: 60, TargetDelta: 2}
	base := Evaluate(st, fight, actions, "dps").DPS
	up := Evaluate(st.ApplyDelta(map[string]float64{"sp": 10}), fight, actions, "dps").DPS
	if up <= base {
		t.Error("spell power gained nothing")
	}
	capped := Evaluate(st.ApplyDelta(map[string]float64{"hit": 5}), fight, actions, "dps").DPS
	over := Evaluate(st.ApplyDelta(map[string]float64{"hit": 25}), fight, actions, "dps").DPS
	almost(t, "hit beyond the cap", over, capped, 1e-9)
}

// A heal rank the caster has outgrown loses 5 % of its healing bonus per
// level beyond twelve above the rank's max level. Measured on a level-27
// priest: Lesser Heal rank 1 (max level 3) keeps 40 %, rank 2 (max 9) 70 %,
// Renew rank 1 (max 13) 90 %.
func TestRankBonusShare(t *testing.T) {
	for _, c := range []struct {
		level, max int
		want       float64
	}{
		{27, 3, 0.40}, {27, 9, 0.70}, {27, 13, 0.90}, {27, 15, 1}, {27, 21, 1},
		{60, 3, 0}, {60, 30, 0.10}, {60, 48, 1},
		{60, 0, 1}, // no max level: a talent spell, nothing to outgrow
	} {
		almost(t, "share", RankBonusShare(c.level, Levels{Max: c.max}), c.want, 1e-9)
	}
}

// The lost share comes off the bonus, not the base points, for direct and
// periodic heals alike; damage of an old rank keeps its full bonus.
func TestOutgrownHealRank(t *testing.T) {
	st := testState()
	st.Level = 27
	st.Healing = 124
	lesser := &Spell{
		ID: 2052, BaseID: 2050, School: Holy, CastMs: 2000, IsHeal: true,
		Levels: Levels{Base: 4, Max: 9, Spell: 4},
		Direct: &Effect{BP: 94, PerLevel: 0.7, Coeff: 0.571},
	}
	// 94 + 0.7*5 = 97.5 base, 0.571*124*0.7 = 49.56 bonus: the tooltip's 139-156.
	almost(t, "lesser heal", Shape(lesser, st, NewTarget(27, 0)).DirectAvg, 97.5+0.571*124*0.7, 1e-9)
	renew := &Spell{
		ID: 139, BaseID: 139, School: Holy, DurationMs: 15000, IsHeal: true,
		Levels:   Levels{Base: 8, Max: 13, Spell: 8},
		Periodic: &Effect{BP: 9, Coeff: 0.2, PeriodMs: 3000},
	}
	sh := Shape(renew, st, NewTarget(27, 0))
	almost(t, "renew", sh.Ticks*sh.TickBase, 5*(9+0.2*124*0.9), 1e-9) // the tooltip's 156

	st.SP[Shadow] = 100
	old := bolt()
	old.Levels = Levels{Base: 1, Max: 5, Spell: 1}
	almost(t, "old damage rank", Shape(old, st, NewTarget(27, 0)).DirectAvg, 80+0.6*100, 1e-9)
}

// The range of a hit is the range of its base points: spell power adds the
// same amount to both ends. Shadow Bolt rank 7 at level 51 with 266 shadow
// spell power: 148 base, variance 0.114 -> 367 to 385 in the tooltip.
func TestRangeFromBasePoints(t *testing.T) {
	st := testState()
	st.Level = 51
	st.SP[Shadow] = 266
	sp := &Spell{
		ID: 11659, BaseID: 686, School: Shadow, CastMs: 3000,
		Levels: Levels{Base: 44, Max: 49, Spell: 44},
		Direct: &Effect{BP: 141, PerLevel: 1.4, Variance: 0.114, Coeff: 0.857},
	}
	sh := Shape(sp, st, NewTarget(51, 0))
	avg := 148 + 0.857*266
	almost(t, "average", sh.DirectAvg, avg, 1e-9)
	almost(t, "width", sh.DirectAvg*sh.DirectVariance, 148*0.114, 1e-9)
	// Sampled hits stay inside that range (crits aside).
	st.Crit[Shadow] = 0
	sh = Shape(sp, st, NewTarget(51, 0))
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 1000; i++ {
		if v := sh.SampleLanded(rng); v < avg-148*0.057-1e-9 || v > avg+148*0.057+1e-9 {
			t.Fatalf("hit %g outside %g..%g", v, avg-148*0.057, avg+148*0.057)
		}
	}
}
