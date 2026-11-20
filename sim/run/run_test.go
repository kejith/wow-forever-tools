package run

import (
	"math"
	"testing"

	"github.com/kejith/wow-forever-tools/sim/engine"
)

func input() Input {
	return Input{
		CacheDir: "../testdata",
		Build:    "9.9.9.9",
		Locale:   "xxXX",
		Character: Character{
			ClassFile: "WARLOCK",
			Level:     20,
			Stats: Stats{
				SpellDamage: map[string]float64{"shadow": 100},
				SpellCrit:   map[string]float64{"shadow": 10},
				SpellHit:    1,
				ManaMax:     1000,
				HealthMax:   500,
				MP5:         10,
				RangedCrit:  5,
				Wand:        engine.Wand{DPS: 20, Skill: 100},
				LifeTap:     engine.LifeTap{HealthPerCast: 100, ManaPerCast: 120},
			},
		},
		Fight: engine.Fight{Seconds: 60, TargetDelta: 2},
	}
}

func almost(t *testing.T, name string, got, want, eps float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Errorf("%s = %g, want %g (±%g)", name, got, want, eps)
	}
}

// The golden fixture character, every number hand-computed:
//
//	Corruption  228 dmg/18 s (6 ticks à 40, hit 95%), 50 mana
//	Curse of Agony 190 dmg/24 s (8 ticks à 25, hit 95%), 40 mana
//	Shadow Bolt r2 139.65 dmg per 3 s cast (140 base+sp, crit 1.05, hit 95%), 140 mana
//	Before the wait the DoTs take 1.5/18 + 1.5/24 = 0.145833 of the timeline;
//	a recast waits (1 - 0.145833)*3/2 = 1.28125 s behind a bolt on average:
//	Corruption cycle 19.28125 s -> 11.8250 dps, 2.59319 mana/s, share 0.077796
//	Curse of Agony cycle 25.28125 s -> 7.51545 dps, 1.58220 mana/s, share 0.059333
//	filler share 0.862872 at 46.55 dps and 46.6667 mana/s per unit
//	rotMPS 44.4427, budget 1000/60+2 = 18.6667, ttoom = 1000/(44.4427-2) = 23.561
//	Life Tap capped by health: 0.4*500/(60*100) = 1/30 taps/s (4 mana/s, 5 % of the time)
//	The DoTs stay up; the filler gets (18.6667+4-4.17539)/46.6667 = 0.396242,
//	the wand the rest 0.416630 à 19.364
//	DPS = 19.3405 + 18.4451 + 8.0676 = 45.8531
func TestGoldenWarlock(t *testing.T) {
	out := Run(input())
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if out.SpellbookSource != "assumed" {
		t.Errorf("spellbook source = %q", out.SpellbookSource)
	}
	almost(t, "dps", out.Baseline.Value, 45.8531, 0.01)
	if out.Baseline.TTOOMSec == nil {
		t.Fatal("ttoom missing")
	}
	almost(t, "ttoom", *out.Baseline.TTOOMSec, 23.561, 0.01)
	almost(t, "spent", out.Baseline.ManaSpentPS, 44.4427, 0.001)
	almost(t, "budget", out.Baseline.ManaBudgetPS, 18.6667, 0.001)
	// Curse of Agony, Corruption, Shadow Bolt, Life Tap, Wand.
	if len(out.Baseline.Breakdown) != 5 {
		t.Errorf("breakdown: %+v", out.Baseline.Breakdown)
	}
}

func TestEvaluationsAndHitCap(t *testing.T) {
	in := input()
	in.Evaluations = []Evaluation{
		{Key: "sp", Delta: map[string]float64{"sp": 10}},
		{Key: "hitCap", Delta: map[string]float64{"hit": 5}}, // 6 % miss against +2, 1 % on the sheet
		{Key: "hitOver", Delta: map[string]float64{"hit": 25}},
	}
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if out.Results["sp"].Gain <= 0 {
		t.Errorf("sp gain = %g", out.Results["sp"].Gain)
	}
	if out.Results["hitCap"].Gain <= 0 {
		t.Errorf("hit gain = %g", out.Results["hitCap"].Gain)
	}
	almost(t, "hit past the cap", out.Results["hitOver"].Value, out.Results["hitCap"].Value, 1e-9)
	if p := out.Results["sp"].GainPct; p <= 0 {
		t.Errorf("gainPct = %g", p)
	}
}

// An exported spellbook wins over the assumption: without rank 2 the
// character bolts with rank 1, and the missing Curse of Agony is not cast.
func TestExportedSpellbook(t *testing.T) {
	in := input()
	in.Character.Spellbook = []int{686, 172}
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if out.SpellbookSource != "export" {
		t.Errorf("source = %q", out.SpellbookSource)
	}
	for _, row := range out.Baseline.Breakdown {
		if row.SpellID == 695 || row.SpellID == 980 {
			t.Errorf("unknown spell in rotation: %+v", row)
		}
		if row.SpellID == 686 {
			return
		}
	}
	t.Error("Shadow Bolt rank 1 missing")
}

func TestTalentsRaiseTheMetric(t *testing.T) {
	base := Run(input())
	in := input()
	in.Character.Talents = map[string]int{"18271": 1} // +10% shadow damage
	out := Run(in)
	if out.Baseline.Value <= base.Baseline.Value {
		t.Errorf("talent gained nothing: %g vs %g", out.Baseline.Value, base.Baseline.Value)
	}
}

func TestUnsupportedClassAndMethod(t *testing.T) {
	in := input()
	in.Character.ClassFile = "MAGE"
	if out := Run(in); out.Error == "" {
		t.Error("mage should not be modeled yet")
	}
	in = input()
	in.Method = "bogus"
	if out := Run(in); out.Error == "" {
		t.Error("an unknown method must refuse")
	}
}

// The Monte-Carlo method: the baseline carries the distribution, only the
// biggest gains are replayed (the rest keep the closed-form delta).
func TestMonteCarloRun(t *testing.T) {
	in := input()
	in.Method = "montecarlo"
	in.Iterations = 400
	in.MCTop = 1
	in.Evaluations = []Evaluation{
		{Key: "sp", Delta: map[string]float64{"sp": 10}},
		{Key: "tiny", Delta: map[string]float64{"spi": 1}},
	}
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	mc := out.Baseline.MC
	if mc == nil || mc.Iterations != 400 || mc.StdDev <= 0 {
		t.Fatalf("baseline mc = %+v", mc)
	}
	almost(t, "baseline value is the mc mean", out.Baseline.Value, mc.Mean, 1e-9)
	if !out.Results["sp"].MC || out.Results["sp"].Gain <= 0 {
		t.Errorf("sp not refined: %+v", out.Results["sp"])
	}
	if out.Results["tiny"].MC {
		t.Error("only the top gains are refined")
	}
}
