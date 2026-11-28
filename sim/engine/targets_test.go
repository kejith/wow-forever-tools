package engine

import (
	"math"
	"testing"
)

// rain is an area channel: four pulses over 8 s on every enemy, each pulse
// a spell hit of its own.
func rain() *Spell {
	return &Spell{
		ID: 5740, BaseID: 5740, Name: "Rain of Fire", School: Shadow,
		Channeled: true, DurationMs: 8000, Cost: Cost{Mana: 300}, Area: true,
		Periodic: &Effect{BP: 30, Coeff: 0.1, PeriodMs: 2000, Crits: true},
	}
}

// hellfire is an area channel that burns its caster as well: 15 ticks of
// 50 on every enemy and of 100 on the caster.
func hellfire() *Spell {
	return &Spell{
		ID: 1949, BaseID: 1949, Name: "Hellfire", School: Shadow,
		Channeled: true, DurationMs: 15000, Cost: Cost{Mana: 600}, Area: true,
		Periodic: &Effect{BP: 40, Coeff: 0.1, PeriodMs: 1000},
		SelfTick: &Effect{BP: 90, Coeff: 0.1},
	}
}

func TestEnemies(t *testing.T) {
	for targets, want := range map[int]int{-1: 1, 0: 1, 1: 1, 3: 3, 99: MaxTargets} {
		if got := (Fight{Targets: targets}).Enemies(); got != want {
			t.Errorf("Targets %d: %d enemies, want %d", targets, got, want)
		}
	}
	// A DoT is kept on every enemy unless its cooldown allows fewer casts
	// within its duration.
	for _, c := range []struct {
		enemies            int
		duration, cooldown float64
		want               int
	}{{3, 18, 0, 3}, {3, 24, 24, 1}, {6, 24, 8, 3}, {2, 24, 8, 2}, {3, 10, 30, 1}} {
		if got := KeptOn(c.enemies, c.duration, c.cooldown); got != c.want {
			t.Errorf("KeptOn(%d, %g, %g) = %d, want %d", c.enemies, c.duration, c.cooldown, got, c.want)
		}
	}
}

// Three enemies in the closed form, endless mana, 60 s: Corruption is kept
// on each of them, the bolts go to one.
//
//	Corruption takes 3 * 1.5/18 = 25 % of the time before the wait, which is
//	0.75 * 3 s / 2 = 1.125 s: a cycle of 19.125 s. 3 * 228/19.125 = 35.7647
//	dps on 23.5294 % of the time; bolts on the rest at 46.55 dps -> 71.3618
func TestSeveralEnemiesClosedForm(t *testing.T) {
	st := mcState()
	m := Evaluate(st, Fight{Seconds: 60, TargetDelta: 2, Targets: 3}, mcActions(), "dps")
	almost(t, "three enemies", m.DPS, 71.3618, 0.001)
	for _, row := range m.Breakdown {
		if row.Kind == "dot" {
			almost(t, "corruption on three", row.Value, 35.7647, 0.001)
			almost(t, "its share of the time", row.SharePct, 23.5294, 0.001)
		}
	}
	// One enemy is the fight as before, and a healer's metric has no enemies.
	almost(t, "one enemy", Evaluate(st, Fight{Seconds: 60, TargetDelta: 2, Targets: 1}, mcActions(), "dps").DPS, 54.7139, 0.001)
	heal := []Action{{Kind: "heal", Spell: lesserHeal()}}
	one := Evaluate(healerState(), Fight{Seconds: 60}, heal, "hps").HPS
	almost(t, "healing does not count enemies", Evaluate(healerState(), Fight{Seconds: 60, Targets: 5}, heal, "hps").HPS, one, 1e-9)
}

// The played fight puts the DoT on one enemy after the other. 18 s with
// nothing else to cast: with 95 % hit each DoT takes 1.5/0.95 s in
// expectation and ticks at full strength (6 ticks on enemy 1, 5 on enemy 2,
// 4 on enemy 3 before 18 s = 15 ticks of 40).
func TestPlaySeveralEnemies(t *testing.T) {
	st := mcState()
	dot := []Action{{Kind: "dot", Spell: corruption()}, {Kind: "wand"}}
	one := Play(st, Fight{Seconds: 18, TargetDelta: 2}, dot, "dps")
	almost(t, "one enemy", one.Value, 6*40.0/18, 1e-9)
	three := Play(st, Fight{Seconds: 18, TargetDelta: 2, Targets: 3}, dot, "dps")
	almost(t, "three enemies", three.Value, 15*40.0/18, 1e-9)
	step := 1.5 / 0.95
	for i, want := range []float64{0, step, 2 * step} {
		if e := three.Events[i]; e.Kind != "dot" || math.Abs(e.T-want) > 1e-9 {
			t.Errorf("event %d: %+v", i, e)
		}
	}
	if three.Events[3].Kind != "wand" {
		t.Errorf("a fourth cast with three enemies: %+v", three.Events[3])
	}

	// A DoT whose cooldown is its duration stays on one enemy.
	plague := corruption()
	plague.CooldownMs = 18000
	single := Play(st, Fight{Seconds: 18, TargetDelta: 2, Targets: 3}, []Action{{Kind: "dot", Spell: plague}, {Kind: "wand"}}, "dps")
	almost(t, "a DoT with a cooldown", single.Value, one.Value, 1e-9)

	// The dice agree with the fight played at its expectation.
	fight := Fight{Seconds: 60, TargetDelta: 2, Targets: 3}
	want := Play(st, fight, mcActions(), "dps").Value
	if mc := RunMC(st, fight, mcActions(), "dps", 3000, 1); math.Abs(mc.Mean-want)/want > 0.03 {
		t.Errorf("mc mean %g too far from the played fight %g", mc.Mean, want)
	}
}

// An area channel hits every enemy with one cast, and its pulses crit
// without a talent: 4 pulses of (30 + 0.1*100) * 1.05 at 95 % hit = 159.6
// per enemy and channel, 19.95 dps per enemy.
func TestAreaFiller(t *testing.T) {
	st := mcState()
	sh := Shape(rain(), st, NewTarget(20, 2))
	almost(t, "pulse with crits", sh.TickValue, 42, 1e-9)
	almost(t, "pulse on the tooltip", sh.TickBase, 40, 1e-9)
	if !sh.Area {
		t.Error("the shape lost the area")
	}
	actions := []Action{{Kind: "filler", Spell: rain()}, {Kind: "wand"}}
	for enemies, want := range map[int]float64{1: 19.95, 4: 79.8} {
		fight := Fight{Seconds: 64, TargetDelta: 2, Targets: enemies}
		almost(t, "closed form", Evaluate(st, fight, actions, "dps").DPS, want, 1e-9)
		almost(t, "played", Play(st, fight, actions, "dps").Value, want, 1e-9)
		if mc := RunMC(st, fight, actions, "dps", 3000, 1); math.Abs(mc.Mean-want)/want > 0.03 {
			t.Errorf("%d enemies: mc mean %g, want about %g", enemies, mc.Mean, want)
		}
	}
}

// Hellfire burns its caster: the played fight channels it in front of the
// filler for the ticks the health above its floor (40 %) pays for, and not
// at all when that is less than a third of the channel.
func TestBurn(t *testing.T) {
	st := mcState()
	st.HealthMax = 1000 // 600 above the floor: six ticks of 100
	tgt := NewTarget(20, 2)
	sh := Shape(hellfire(), st, tgt)
	almost(t, "tick on the caster", sh.SelfTick, 100, 1e-9)
	almost(t, "tick on an enemy", sh.TickValue, 50, 1e-9)

	actions := []Action{{Kind: "burn", Spell: hellfire()}, {Kind: "filler", Spell: bolt()}, {Kind: "wand"}}
	plain := actions[1:]
	fight := Fight{Seconds: 30, TargetDelta: 2, Targets: 4}
	pb := Play(st, fight, actions, "dps")
	if pb.Events[0].Kind != "burn" || pb.Events[1].Kind != "filler" {
		t.Fatalf("events: %+v", pb.Events[:2])
	}
	almost(t, "six ticks, then the filler", pb.Events[1].T, 6, 1e-9)
	almost(t, "health after the channel", pb.Events[1].Health, 400, 1e-9)
	almost(t, "its damage: 6 ticks of 50 at 95 % on four enemies", pb.BySpell[1949], 6*50*0.95*4, 1e-9)
	burns := 0
	for _, e := range pb.Events {
		if e.Kind == "burn" {
			burns++
		}
	}
	if burns != 1 {
		t.Errorf("%d channels: the health stays at the floor after the first", burns)
	}
	almost(t, "the whole fight", pb.Value, (1140+8*139.65)/30, 1e-9)

	// The channel pays its mana in full at the start.
	st.ManaMax = 1000
	short := Play(st, fight, actions, "dps")
	almost(t, "mana after the channel", short.Events[1].Mana, 400, 1e-9)
	st.ManaMax = 1e9

	// Four ticks are less than a third of fifteen: not worth the mana.
	st.HealthMax = 700
	if pb := Play(st, fight, actions, "dps"); pb.Events[0].Kind != "filler" {
		t.Errorf("a channel of four ticks: %+v", pb.Events[0])
	}
	st.HealthMax = 1000

	// One enemy: it is not offered. And the closed form leaves it out.
	if pb := Play(st, Fight{Seconds: 30, TargetDelta: 2}, actions, "dps"); pb.Events[0].Kind != "filler" {
		t.Errorf("Hellfire on one enemy: %+v", pb.Events[0])
	}
	almost(t, "closed form", Evaluate(st, fight, actions, "dps").DPS, Evaluate(st, fight, plain, "dps").DPS, 1e-9)

	// The end of the fight cuts the channel: 5 s are five ticks, and 4 s
	// are too few to start it.
	cut := Play(st, Fight{Seconds: 5, TargetDelta: 2, Targets: 4}, actions, "dps")
	if len(cut.Events) != 1 || cut.Events[0].Kind != "burn" {
		t.Fatalf("a 5 s fight: %+v", cut.Events)
	}
	almost(t, "cut by the fight", cut.BySpell[1949], 5*50*0.95*4, 1e-9)
	if cut := Play(st, Fight{Seconds: 4, TargetDelta: 2, Targets: 4}, actions, "dps"); cut.Events[0].Kind != "filler" {
		t.Errorf("a 4 s fight: %+v", cut.Events[0])
	}
}

// An AoE cooldown hits every enemy up to AreaMax, applying ChainFalloff per
// jump when set. Both Evaluate and Play must agree with the exact expectation.
func TestAreaCooldownAndChainFalloff(t *testing.T) {
	st := mcState()
	st.Crit[Nature] = 5
	// Cooldown hitting up to 3 targets with 0.7 chain falloff (Chain Lightning):
	// 100 base + 0 SP = 100, 5% crit (1.025x), 95% hit = 97.375 per full target.
	// On 1 target: 1x; on 2: 1+0.7=1.7x; on 3 or 5: 1+0.7+0.49=2.19x.
	chain := &Spell{
		ID: 421, BaseID: 421, Name: "Chain Lightning", School: Nature,
		CastMs: 1500, CooldownMs: 6000, Cost: Cost{Mana: 100},
		Area: true, AreaMax: 3, ChainFalloff: 0.7,
		Direct: &Effect{BP: 100, Coeff: 0},
	}
	actions := []Action{{Kind: "cooldown", Spell: chain}}
	oneHit := 100.0 * (1 + 0.05*0.5) * 0.95
	for enemies, mult := range map[int]float64{1: 1.0, 2: 1.7, 3: 2.19, 5: 2.19} {
		fight := Fight{Seconds: 60, TargetDelta: 2, Targets: enemies}
		want := 10 * oneHit * mult / 60.0 // 10 casts in 60s (at 0, 6, ..., 54)
		almost(t, "closed form", Evaluate(st, fight, actions, "dps").DPS, want, 1e-6)
		almost(t, "played", Play(st, fight, actions, "dps").Value, want, 1e-6)
	}
}

// A ground AoE DoT (Flamestrike, Consecration) is cast once on the ground and
// damages every enemy on both its direct hit and its periodic ticks.
func TestGroundAreaDoT(t *testing.T) {
	st := mcState()
	st.Crit[Fire] = 5
	// Direct 100 (5% crit -> 102.5) + 4 ticks of 50 (no crit) over 8 s, 95% hit.
	// Cast once every 8 s = 4 casts in 32 s.
	ground := &Spell{
		ID: 2120, BaseID: 2120, Name: "Flamestrike", School: Fire,
		CastMs: 2000, DurationMs: 8000, Cost: Cost{Mana: 200}, Area: true,
		Direct:   &Effect{BP: 100, Coeff: 0},
		Periodic: &Effect{BP: 50, Coeff: 0, PeriodMs: 2000},
	}
	actions := []Action{{Kind: "dot", Spell: ground}}
	perCastOne := (100*(1+0.05*0.5) + 4*50) * 0.95
	fight := Fight{Seconds: 32, TargetDelta: 2, Targets: 4}
	// Play casts at t=0 (lands t=2, 4 ticks by t=10), t=10 (lands t=12, 4 ticks by t=20),
	// t=20 (lands t=22, 4 ticks by t=30), t=30 (lands t=32 -> direct hit only, 0 ticks).
	// Between casts, wand swings fill idle gaps (at t=10..10.5 etc., shifting cast starts by 0.5 s).
	pb := Play(st, fight, actions, "dps")
	dotCasts := 0
	for _, e := range pb.Events {
		if e.Kind == "dot" {
			dotCasts++
		}
	}
	if dotCasts != 3 && dotCasts != 4 {
		t.Fatalf("expected 3-4 ground casts (not 4x per enemy), got %d", dotCasts)
	}
	// Verify damage scales linearly with 4 targets vs 1 target.
	pbOne := Play(st, Fight{Seconds: 32, TargetDelta: 2, Targets: 1}, actions, "dps")
	almost(t, "ground AoE DoT scales 4x", pb.BySpell[2120], pbOne.BySpell[2120]*4, 1e-6)
	almost(t, "ground AoE DoT closed form", Evaluate(st, fight, actions, "dps").DPS, perCastOne*4/8.0, 1e-6)
}
