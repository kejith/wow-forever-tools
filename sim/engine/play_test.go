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
	// The closed form does not know the standby spell and skips it.
	a, b := Evaluate(st, fight, plain, "dps").DPS, Evaluate(st, fight, standby, "dps").DPS
	almost(t, "closed form", b, a, 1e-9)
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
