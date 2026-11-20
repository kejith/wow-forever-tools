package engine

import (
	"math"
	"testing"
)

func mcState() *State {
	st := testState()
	st.ManaMax = 1e9
	return st
}

func mcActions() []Action {
	return []Action{
		{Kind: "dot", Spell: corruption()},
		{Kind: "filler", Spell: bolt()},
		{Kind: "wand"},
	}
}

// The same seed plays the same fights.
func TestMCReproducible(t *testing.T) {
	st := mcState()
	fight := Fight{Seconds: 60, TargetDelta: 2}
	a := RunMC(st, fight, mcActions(), "dps", 500, 7)
	b := RunMC(st, fight, mcActions(), "dps", 500, 7)
	if a != b {
		t.Errorf("not reproducible: %+v vs %+v", a, b)
	}
	c := RunMC(st, fight, mcActions(), "dps", 500, 8)
	if a.Mean == c.Mean {
		t.Error("a different seed should roll differently")
	}
}

// With plenty of mana the sampled mean approaches the closed-form cycle,
// and the rolls leave a real spread.
func TestMCMeanMatchesExpectation(t *testing.T) {
	st := mcState()
	fight := Fight{Seconds: 60, TargetDelta: 2}
	want := Evaluate(st, fight, mcActions(), "dps").DPS
	mc := RunMC(st, fight, mcActions(), "dps", 4000, 1)
	if math.Abs(mc.Mean-want)/want > 0.04 {
		t.Errorf("mc mean %g too far from expectation %g", mc.Mean, want)
	}
	if mc.StdDev <= 0 || mc.P10 >= mc.P90 {
		t.Errorf("degenerate spread: %+v", mc)
	}
}

// Under mana pressure Life Tap and the wand carry the fight, like the
// closed form; the means stay in the same region.
func TestMCManaPressure(t *testing.T) {
	st := testState()
	st.ManaMax = 1000
	st.HealthMax = 500
	st.MP5 = 10
	st.LifeTap = LifeTap{HealthPerCast: 100, ManaPerCast: 120}
	st.Wand = Wand{DPS: 20, Skill: 100}
	st.RangedCrit = 5
	fight := Fight{Seconds: 60, TargetDelta: 2}
	want := Evaluate(st, fight, mcActions(), "dps").DPS
	mc := RunMC(st, fight, mcActions(), "dps", 3000, 1)
	if math.Abs(mc.Mean-want)/want > 0.20 {
		t.Errorf("mc mean %g too far from the closed form %g", mc.Mean, want)
	}
}

// Common random numbers: more spell power never loses against the same
// dice, so a small gain is measurable with few iterations.
func TestMCCommonRandomNumbers(t *testing.T) {
	st := mcState()
	fight := Fight{Seconds: 60, TargetDelta: 2}
	base := RunMC(st, fight, mcActions(), "dps", 800, 3)
	up := RunMC(st.ApplyDelta(map[string]float64{"sp": 10}), fight, mcActions(), "dps", 800, 3)
	if up.Mean <= base.Mean {
		t.Errorf("sp gained nothing under CRN: %g vs %g", up.Mean, base.Mean)
	}
}

// ttoom: the sampled time until the filler first becomes unaffordable.
func TestMCTTOOM(t *testing.T) {
	st := testState()
	st.ManaMax = 1000
	fight := Fight{Seconds: 60, TargetDelta: 2}
	mc := RunMC(st, fight, mcActions(), "ttoom", 500, 1)
	if mc.Mean <= 0 || mc.Mean >= 60 {
		t.Errorf("ttoom mean = %g", mc.Mean)
	}
}
