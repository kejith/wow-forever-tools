package engine

import (
	"math"
	"testing"
)

// Without dice the played fight is what many rolled fights average to.
func TestPlayIsTheMeanFight(t *testing.T) {
	st := mcState()
	fight := Fight{Seconds: 60, TargetDelta: 2}
	pb := Play(st, fight, mcActions(), "dps")
	mc := RunMC(st, fight, mcActions(), "dps", 4000, 1)
	if math.Abs(pb.Value-mc.Mean)/mc.Mean > 0.02 {
		t.Errorf("played %g, mean of the rolled fights %g", pb.Value, mc.Mean)
	}
	if pb.OOMSec != nil {
		t.Errorf("out of mana at %g with endless mana", *pb.OOMSec)
	}
	// Corruption first, then bolts; every action is on the record with the
	// time it starts at.
	if len(pb.Events) < 3 || pb.Events[0].Kind != "dot" || pb.Events[0].SpellID != 172 || pb.Events[1].Kind != "filler" {
		t.Fatalf("events: %+v", pb.Events)
	}
	almost(t, "second action starts", pb.Events[1].T, 1.5, 1e-9)
	sum := 0.0
	for _, v := range pb.BySpell {
		sum += v
	}
	almost(t, "damage per spell adds up", sum/fight.Seconds, pb.Value, 1e-9)
}

// A cast that runs past the end of the fight counts for the part inside it:
// 4 s of 3 s bolts à 139.65 are one bolt and a third, 46.55 dps.
func TestPlayCutsTheLastCast(t *testing.T) {
	st := mcState()
	pb := Play(st, Fight{Seconds: 4, TargetDelta: 2}, []Action{{Kind: "filler", Spell: bolt()}}, "dps")
	almost(t, "dps", pb.Value, 139.65/3, 1e-6)
	if len(pb.Events) != 2 {
		t.Errorf("events: %+v", pb.Events)
	}
}

// A DoT cast late ticks only for what is left of the fight: Corruption
// (6 ticks à 40, hit 95 %) in a 7 s fight ticks twice.
func TestPlayDotOutlivesTheFight(t *testing.T) {
	st := mcState()
	pb := Play(st, Fight{Seconds: 7, TargetDelta: 2}, []Action{{Kind: "dot", Spell: corruption()}}, "dps")
	almost(t, "dps", pb.Value, 2*40*0.95/7, 1e-6)
}

// When the health is too low for Life Tap, a spell that returns health goes
// in front of a filler the mana no longer pays for: its health is the next
// tap. Without it the rotation ends on the wand.
func TestPlayRecover(t *testing.T) {
	st := testState()
	st.ManaMax = 500
	st.HealthMax = 500
	st.LifeTap = LifeTap{HealthPerCast: 100, ManaPerCast: 120}
	st.Wand = Wand{DPS: 5, Skill: 100}
	fight := Fight{Seconds: 300, TargetDelta: 2}
	plain := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: bolt()}, {Kind: "wand"}}
	standby := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: bolt()}, {Kind: "recover", Spell: drain()}, {Kind: "wand"}}
	without := Play(st, fight, plain, "dps")
	with := Play(st, fight, standby, "dps")
	if with.Value <= without.Value*1.1 {
		t.Errorf("standby Drain Life should carry the long fight: %g vs %g", with.Value, without.Value)
	}
	drains, taps := 0, 0
	for _, e := range with.Events {
		switch e.Kind {
		case "recover":
			drains++
			if e.Health-st.LifeTap.HealthPerCast >= 0.6*st.HealthMax {
				t.Errorf("Drain Life at %g s with %g health: a tap was possible", e.T, e.Health)
			}
		case "lifetap":
			taps++
			if e.Health-st.LifeTap.HealthPerCast < 0.6*st.HealthMax-1e-6 {
				t.Errorf("tap at %g s takes the health below the floor (%g)", e.T, e.Health)
			}
		}
	}
	if drains == 0 || taps <= 2 {
		t.Errorf("drains %d, taps %d", drains, taps)
	}
	if without.OOMSec == nil {
		t.Error("the plain rotation should run out of mana")
	}
}

// The closed form splits the time the DoTs leave between the filler, the
// standby spell, Life Tap and the wand so that the damage is the highest
// the mana and the health allow. The fixture of TestPlayRecover by hand:
//
//	Corruption  every 18 s + half a bolt of waiting (0.9167 * 1.5 s): 19.375 s,
//	            7.742 % of the time, 228/19.375 = 11.768 dps, 2.581 mana/s
//	time        f + r + 1.5 x = 0.92258             (bolt, Drain Life, taps per second)
//	mana        46.667 f + 11 r - 120 x = 500/300 - 2.581
//	health      100 x - 19 r = 0.4 * 500/300        (Drain Life returns its 19 dps)
//
// which gives r = 0.594999, f = 0.148007, x = 0.119716 and no wand:
// 11.768 + 0.148007 * 46.55 + 0.594999 * 19 = 29.9625 dps. Without the
// standby spell the health pays for 0.4 taps a minute, which is not even the
// mana of Corruption: 15.5532 dps, most of it on the wand.
func TestEvaluateStandby(t *testing.T) {
	st := testState()
	st.ManaMax = 500
	st.HealthMax = 500
	st.LifeTap = LifeTap{HealthPerCast: 100, ManaPerCast: 120}
	st.Wand = Wand{DPS: 5, Skill: 100}
	fight := Fight{Seconds: 300, TargetDelta: 2}
	plain := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: bolt()}, {Kind: "wand"}}
	standby := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: bolt()}, {Kind: "recover", Spell: drain()}, {Kind: "wand"}}
	almost(t, "plain", Evaluate(st, fight, plain, "dps").DPS, 15.5532, 1e-9)
	m := Evaluate(st, fight, standby, "dps")
	almost(t, "with Drain Life standing by", m.DPS, 29.962457, 1e-6)
	almost(t, "self healing", m.SelfHealPS, 0.594999*19, 1e-4)
	shares := map[string]float64{}
	for _, row := range m.Breakdown {
		shares[row.Kind] = row.SharePct
		if row.Kind == "recover" {
			if row.SpellID != 689 {
				t.Errorf("standby row: %+v", row)
			}
			almost(t, "Drain Life dps", row.Value, 0.594999*19, 1e-4)
			almost(t, "Drain Life leech", row.Leech, 0.594999*19, 1e-4)
		}
		if row.Kind == "lifetap" {
			almost(t, "taps per minute", row.PerMin, 0.119716*60, 1e-3)
		}
	}
	almost(t, "bolt share", shares["filler"], 14.8007, 1e-3)
	almost(t, "Drain Life share", shares["recover"], 59.4999, 1e-3)
	if _, wand := shares["wand"]; wand {
		t.Errorf("no time should be left for the wand: %+v", m.Breakdown)
	}

	// While the mana lasts the standby spell changes nothing: no row, the
	// same number.
	short := Fight{Seconds: 8, TargetDelta: 2}
	a, b := Evaluate(st, short, plain, "dps"), Evaluate(st, short, standby, "dps")
	almost(t, "short fight", b.DPS, a.DPS, 1e-12)
	if len(b.Breakdown) != len(a.Breakdown) {
		t.Errorf("breakdown: %+v", b.Breakdown)
	}
	// A healer's rotation has no health budget: the standby spell is skipped.
	almost(t, "other metric", Evaluate(st, fight, standby, "hps").DPS, Evaluate(st, fight, plain, "hps").DPS, 1e-12)
}

// maximize finds the best corner: 3x + 2y under x + y <= 4 and x + 3y <= 6
// is 12 at (4, 0); with x + y = 2 added it is 6 at (2, 0).
func TestMaximize(t *testing.T) {
	x, v, ok := maximize([]float64{3, 2}, nil, [][]float64{{1, 1, 4}, {1, 3, 6}})
	if !ok {
		t.Fatal("feasible")
	}
	almost(t, "value", v, 12, 1e-9)
	almost(t, "x", x[0], 4, 1e-9)
	almost(t, "y", x[1], 0, 1e-9)
	x, v, ok = maximize([]float64{3, 2}, [][]float64{{1, 1, 2}}, [][]float64{{1, 1, 4}, {1, 3, 6}})
	if !ok {
		t.Fatal("feasible with the equality")
	}
	almost(t, "value", v, 6, 1e-9)
	almost(t, "x", x[0], 2, 1e-9)
	// 2y - x: the corner where both bounds meet, (3, 1), loses to (0, 2).
	x, v, _ = maximize([]float64{-1, 2}, nil, [][]float64{{1, 1, 4}, {1, 3, 6}})
	almost(t, "value", v, 4, 1e-9)
	almost(t, "y", x[1], 2, 1e-9)
	// Nothing fits x + y = 2 and x + y <= 1.
	if _, _, ok := maximize([]float64{1, 1}, [][]float64{{1, 1, 2}}, [][]float64{{1, 1, 1}}); ok {
		t.Error("infeasible")
	}
}

// Every subset of the optional spells, one filler, Drain Life standing by
// next to the other fillers.
func TestChoicesRotations(t *testing.T) {
	c := Choices{
		Optional: []Action{{Kind: "dot", Spell: corruption()}, {Kind: "cooldown", Spell: twoPart()}},
		Fillers:  []*Spell{bolt(), drain()},
		Recover:  drain(),
		Tail:     []Action{{Kind: "wand"}},
	}
	rots := c.Rotations()
	// 4 subsets x (bolt, bolt + standby, drain, no filler).
	if len(rots) != 16 {
		t.Fatalf("%d rotations", len(rots))
	}
	seen := map[string]bool{}
	for _, r := range rots {
		key, fillers, recovers := "", 0, 0
		for i, a := range r {
			key += a.Kind
			if a.Spell != nil {
				key += a.Spell.Name
			}
			key += "|"
			switch a.Kind {
			case "filler":
				fillers++
			case "recover":
				recovers++
				if r[i-1].Kind != "filler" || r[i-1].Spell.ID == a.Spell.ID {
					t.Errorf("standby next to itself: %s", key)
				}
			}
		}
		if seen[key] {
			t.Errorf("twice: %s", key)
		}
		seen[key] = true
		if fillers > 1 || recovers > fillers || r[len(r)-1].Kind != "wand" {
			t.Errorf("rotation %s", key)
		}
	}
	if !seen["dotCorruption|cooldownImmolate|fillerShadow Bolt|recoverDrain Life|wand|"] {
		t.Errorf("order of the full rotation: %v", seen)
	}
	if !seen["dotCorruption|wand|"] || !seen["wand|"] {
		t.Errorf("the wand as the filler: %v", seen)
	}
	if got := (Choices{Tail: []Action{{Kind: "wand"}}}).Rotations(); len(got) != 1 || len(got[0]) != 1 {
		t.Errorf("no choices: %+v", got)
	}
}

func curse() *Spell {
	return &Spell{
		ID: 1311680, BaseID: 440892, Name: "Curse of the Elements", School: Shadow,
		DurationMs: 300000, Cost: Cost{Mana: 200},
	}
}

// A debuff deals nothing itself: it costs a cast per duration (at least one
// per fight) and makes every spell of its schools hit harder. The fixture
// with endless mana over 60 s, by hand:
//
//	without     Corruption every 18 s + (1 - 1.5/18) * 1.5 s of waiting = 19.375 s:
//	            228/19.375 = 11.7677 dps, 7.7419 % of the time; bolts on the
//	            rest at 46.55 dps -> 54.7139 dps
//	with +10 %  the curse once per fight: the two take 1.5/18 + 1.5/60 = 10.8333 %
//	            before the wait, which is 0.891667 * 1.5 s = 1.3375 s;
//	            Corruption every 19.3375 s: 250.8/19.3375 = 12.9696 dps,
//	            the curse every 61.3375 s; bolts on the remaining 89.7976 %
//	            at 51.205 dps -> 58.9505 dps
func TestDebuff(t *testing.T) {
	st := mcState()
	fight := Fight{Seconds: 60, TargetDelta: 2}
	plain := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "filler", Spell: bolt()}, {Kind: "wand"}}
	cursed := append([]Action{{Kind: "debuff", Spell: curse(), TakenPct: 10, TakenMask: 1 << Shadow}}, plain...)
	almost(t, "without the curse", Evaluate(st, fight, plain, "dps").DPS, 54.7139, 0.001)
	with := Evaluate(st, fight, cursed, "dps")
	almost(t, "with the curse", with.DPS, 58.9505, 0.001)
	for _, row := range with.Breakdown {
		if row.Kind == "debuff" {
			almost(t, "the curse's own damage", row.Value, 0, 1e-12)
			almost(t, "the curse's share of the time", row.SharePct, 150/61.3375, 1e-4)
		}
	}

	// Only the schools of the mask gain: a fire spell lands as before.
	fire := bolt()
	fire.School = Fire
	st.SP[Fire], st.Crit[Fire] = 100, 10
	ctx := ContextOf(cursed)
	tgt := NewTarget(20, 2)
	almost(t, "fire under a shadow curse", ResolveCastIn(fire, st, tgt, ctx, nil).Damage, 139.65, 1e-9)
	almost(t, "shadow under it", ResolveCastIn(bolt(), st, tgt, ctx, nil).Damage, 139.65*1.1, 1e-9)

	// The played fight opens with the curse and renews it when it runs out:
	// once in 60 s, twice in 400 s.
	pb := Play(st, fight, cursed, "dps")
	if len(pb.Events) < 3 || pb.Events[0].Kind != "debuff" || pb.Events[1].Kind != "dot" {
		t.Fatalf("events: %+v", pb.Events[:3])
	}
	almost(t, "second action starts", pb.Events[1].T, 1.5, 1e-9)
	if pb.Value <= Play(st, fight, plain, "dps").Value {
		t.Errorf("the curse should pay over 60 s: %g", pb.Value)
	}
	casts := func(pb Playback) (n int) {
		for _, e := range pb.Events {
			if e.Kind == "debuff" {
				n++
			}
		}
		return n
	}
	if n := casts(pb); n != 1 {
		t.Errorf("curse cast %d times in 60 s", n)
	}
	long := Play(st, Fight{Seconds: 400, TargetDelta: 2}, cursed, "dps")
	if n := casts(long); n != 2 {
		t.Errorf("curse cast %d times in 400 s", n)
	}
	// In a fight shorter than the cast pays back, the closed form says so.
	short := Fight{Seconds: 6, TargetDelta: 2}
	if with, without := Evaluate(st, short, cursed, "dps").DPS, Evaluate(st, short, plain, "dps").DPS; with >= without {
		t.Errorf("a 6 s fight has no time for the curse: %g vs %g", with, without)
	}
}
