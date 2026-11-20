package engine

import "testing"

// Made-up heals with round numbers (no healing bonus, no crit):
//
//	slow   100 per 2 s cast for 20 mana   -> 50 hps and 10 mana/s while casting, 0.2 mana per point
//	fast   150 per 1.5 s cast for 90 mana -> 100 hps and 60 mana/s, 0.6 mana per point
//	renew  90 over 15 s, instant, 30 mana -> 60 hps and 20 mana/s for a tenth of the time per target
//	shield 100 absorbed, instant, 100 mana, 15 s Weakened Soul -> 66.67 hps and mana/s, a tenth of the time
func demandState() *State {
	st := &State{Level: 20, ManaMax: 1000, MP5: 10, RegenBasePS: 4}
	st.Mods.CastingRegenPct = 25 // 3 mana/s while casting, 6 outside the five-second rule
	return st
}

func slowHeal() HealSource {
	return HealSource{Action: Action{Kind: "heal", Spell: &Spell{ID: 1, BaseID: 1, Name: "Slow", School: Holy, IsHeal: true,
		CastMs: 2000, Cost: Cost{Mana: 20}, Direct: &Effect{BP: 100}}}}
}

func fastHeal() HealSource {
	return HealSource{Action: Action{Kind: "heal", Spell: &Spell{ID: 2, BaseID: 2, Name: "Fast", School: Holy, IsHeal: true,
		CastMs: 1500, Cost: Cost{Mana: 90}, Direct: &Effect{BP: 150}}}}
}

func renewOn(targets int) HealSource {
	return HealSource{Targets: targets, Action: Action{Kind: "hot", Spell: &Spell{ID: 3, BaseID: 3, Name: "Renew", School: Holy,
		IsHeal: true, DurationMs: 15000, Cost: Cost{Mana: 30}, Periodic: &Effect{BP: 18, PeriodMs: 3000}}}}
}

func shieldOn(targets int) HealSource {
	return HealSource{Targets: targets, Action: Action{Kind: "cooldown", Spell: shieldSpell(), CycleSec: 15}}
}

func lineOf(p DemandPlan, id int) DemandLine {
	for _, l := range p.Lines {
		if l.Action.Spell.ID == id {
			return l
		}
	}
	return DemandLine{}
}

// Little demand is met with the cheap heal, and the pauses between casts
// regenerate in full; more demand than the cheap heal supplies mixes in the
// fast one, just as much as the time asks for.
func TestPlanDemandLadder(t *testing.T) {
	st := demandState()
	fight := Fight{Seconds: 60}
	sources := []HealSource{slowHeal(), fastHeal()}
	almost(t, "max", MaxHealing(st, sources), 100, 1e-9)

	// 5 hps: a cast every 20 s; 15 of them outside the five-second rule.
	light := PlanDemand(st, fight, sources, 5, nil)
	almost(t, "light hps", light.HPS, 5, 1e-9)
	almost(t, "light mana", light.ManaPS, 1, 1e-9)
	almost(t, "light free", light.FreePct, 75, 1e-9)
	almost(t, "light regen", light.RegenPS, 5.25, 1e-9)
	almost(t, "light casts", lineOf(light, 1).PerMin, 3, 1e-9)
	if light.OOMSec != nil || len(light.Lines) != 1 || light.Candidates != 3 {
		t.Errorf("light: %+v", light)
	}

	// 40 hps: the cheap heal 80 % of the time, 8 mana/s against 3.
	steady := PlanDemand(st, fight, sources, 40, nil)
	almost(t, "steady cast", steady.CastPct, 80, 1e-9)
	almost(t, "steady free", steady.FreePct, 0, 1e-9)
	almost(t, "steady oom", *steady.OOMSec, 200, 1e-9)
	almost(t, "steady per cast", lineOf(steady, 1).PerCast, 100, 1e-9)

	// 75 hps: half and half, 35 mana/s.
	heavy := PlanDemand(st, fight, sources, 75, nil)
	almost(t, "heavy hps", heavy.HPS, 75, 1e-6)
	almost(t, "heavy slow", lineOf(heavy, 1).SharePct, 50, 1e-4)
	almost(t, "heavy fast", lineOf(heavy, 2).SharePct, 50, 1e-4)
	almost(t, "heavy mana", heavy.ManaPS, 35, 1e-4)
	almost(t, "heavy oom", *heavy.OOMSec, 1000/32.0, 1e-3)

	// More than there is: everything on the fast heal.
	burst := PlanDemand(st, fight, sources, 120, nil)
	almost(t, "burst hps", burst.HPS, 100, 1e-9)
	almost(t, "burst demand", burst.Demand, 120, 1e-9)
	if len(burst.Lines) != 1 || lineOf(burst, 2).SharePct != 100 {
		t.Errorf("burst: %+v", burst.Lines)
	}
	if p := PlanDemand(st, fight, nil, 10, nil); p.HPS != 0 || len(p.Lines) != 0 {
		t.Errorf("no sources: %+v", p)
	}
}

// A HoT and a shield fit on a target once per cycle. When the cheap heal
// alone is too slow, the plan adds them in the order of their price, the
// dearest only as far as needed:
//
//	52 hps from slow (50), renew (6 at most), shield (6.67 at most)
//	renew in full: 6; slow + shield share the other 90 % of the time:
//	50 xs + 66.67 xsh = 46, xs + xsh = 0.9 -> shield 6 %, slow 84 %
//	mana 8.4 + 2 + 4 = 14.4 per second, regeneration 3 -> 87.72 s
func TestPlanDemandUpkeep(t *testing.T) {
	st := demandState()
	fight := Fight{Seconds: 60}
	sources := []HealSource{renewOn(1), shieldOn(1), slowHeal()}
	almost(t, "max", MaxHealing(st, sources), 6.6666667+6+40, 1e-6)
	p := PlanDemand(st, fight, sources, 52, nil)
	almost(t, "hps", p.HPS, 52, 1e-6)
	almost(t, "renew", lineOf(p, 3).SharePct, 10, 1e-4)
	almost(t, "renew casts", lineOf(p, 3).PerMin, 4, 1e-3)
	almost(t, "shield", lineOf(p, 17).SharePct, 6, 1e-4)
	almost(t, "renew kept on", lineOf(p, 3).Targets, 1, 1e-4)
	almost(t, "shield kept on", lineOf(p, 17).Targets, 0.6, 1e-4)
	almost(t, "slow", lineOf(p, 1).SharePct, 84, 1e-4)
	almost(t, "mana", p.ManaPS, 14.4, 1e-4)
	almost(t, "oom", *p.OOMSec, 1000/11.4, 1e-2)
	if p.Candidates != 2 {
		t.Errorf("candidates: %d", p.Candidates)
	}
	// Below what the cheap heal supplies neither is cast.
	if q := PlanDemand(st, fight, sources, 30, nil); len(q.Lines) != 1 || q.Lines[0].Action.Spell.ID != 1 {
		t.Errorf("30 hps: %+v", q.Lines)
	}
	// Five targets take five Renews: half of the time, 30 hps.
	almost(t, "group max", MaxHealing(st, []HealSource{renewOn(5), slowHeal()}), 30+25, 1e-9)
}

// Dark Sacrifice in a plan whose mana does not last: 300 mana for 200
// health, which the plan heals at its own price.
func TestPlanDemandConversion(t *testing.T) {
	st := demandState()
	fight := Fight{Seconds: 60}
	sources := []HealSource{slowHeal(), fastHeal()}
	conv := sacrifice(40)

	// 40 hps at 0.2 mana per point: one cast is worth 300 - 40 = 260 mana,
	// (1000 + 260) / 5 = 252 s.
	p := PlanDemand(st, fight, sources, 40, &conv)
	almost(t, "oom", *p.OOMSec, 252, 1e-9)
	almost(t, "casts", p.Conversions, 1, 1e-9)
	almost(t, "mana", p.ConversionMana, 300, 1e-9)
	almost(t, "health", p.ConversionHealth, 200, 1e-9)
	if p.ConversionSpell == nil || p.ConversionSpell.ID != 1277324 {
		t.Errorf("conversion spell: %+v", p.ConversionSpell)
	}

	// 18 hps: 3.6 mana/s against 3.3; a cast every 14 minutes closes the gap.
	q := PlanDemand(st, fight, sources, 18, &conv)
	if q.OOMSec != nil || q.ConversionSpell == nil {
		t.Fatalf("18 hps: %+v", q)
	}
	almost(t, "per minute", q.ConversionsPerMin, 0.3/260*60, 1e-6)

	// A plan that lasts needs none, and health dearer than the mana is kept.
	if r := PlanDemand(st, fight, sources, 5, &conv); r.ConversionSpell != nil {
		t.Errorf("5 hps: %+v", r)
	}
	costly := sacrifice(400)
	if r := PlanDemand(st, fight, sources, 40, &costly); r.ConversionSpell != nil || *r.OOMSec != 200 {
		t.Errorf("costly: %+v", r)
	}
}
