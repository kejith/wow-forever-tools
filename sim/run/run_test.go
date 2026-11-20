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
//	Corruption  228 dmg/18 s (6 ticks à 40, hit 95%), share 1.5/18
//	Curse of Agony 190 dmg/24 s (8 ticks à 25, hit 95%), share 1.5/24
//	Shadow Bolt r2 139.65 dmg per 3 s cast (140 base+sp, crit 1.05, hit 95%)
//	rotDPS 60.3448, rotMPS 44.3056, budget 1000/60+2 = 18.6667
//	Life Tap capped by health: 0.4*500/(60*100) = 1/30 taps/s
//	cast fraction (18.6667+4)/44.3056 = 0.51160, wand rest 0.43840 à 19.364
//	DPS = 39.3615, ttoom = 1000/(44.3056-2) = 23.638
func TestGoldenWarlock(t *testing.T) {
	out := Run(input())
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if out.SpellbookSource != "assumed" {
		t.Errorf("spellbook source = %q", out.SpellbookSource)
	}
	almost(t, "dps", out.Baseline.Value, 39.3615, 0.01)
	if out.Baseline.TTOOMSec == nil {
		t.Fatal("ttoom missing")
	}
	almost(t, "ttoom", *out.Baseline.TTOOMSec, 23.638, 0.01)
	almost(t, "spent", out.Baseline.ManaSpentPS, 44.3056, 0.001)
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
		{Key: "hitCap", Delta: map[string]float64{"hit": 4}},
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
	almost(t, "hit past the floor", out.Results["hitOver"].Value, out.Results["hitCap"].Value, 1e-9)
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
	in.Method = "montecarlo"
	if out := Run(in); out.Error == "" {
		t.Error("montecarlo is a Stage-2 seam")
	}
}
