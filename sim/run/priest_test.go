package run

import (
	"testing"

	"github.com/kejith/wow-forever-tools/sim/engine"
)

func priestInput() Input {
	return Input{
		CacheDir: "../testdata",
		Build:    "9.9.9.9",
		Locale:   "xxXX",
		Character: Character{
			ClassFile: "PRIEST",
			Level:     20,
			Stats: Stats{
				Healing:   100,
				SpellCrit: map[string]float64{"holy": 5},
				Spirit:    60,
				ManaMax:   2000,
				MP5:       5,
			},
			Talents: map[string]int{"14521": 3}, // Meditation 3/3: 15% regen while casting
		},
		Fight: engine.Fight{Seconds: 60, TargetDelta: 0},
	}
}

// Hand-computed golden with a fixed spellbook (Renew + Lesser Heal r1 only):
//
//	Lesser Heal: (50 + 0.4*100) * (1 + 5%*0.5) = 92.25 heal per 2.5 s cast
//	Renew: 5 ticks à (20 + 0.2*100) = 200 over 15 s, share 1.5/15 = 0.1
//	rotHPS = 200/15 + 0.9*92.25/2.5 = 13.3333 + 33.21 = 46.5433
//	rotMPS = 30/15 + 0.9*30/2.5 = 12.8
//	regen while casting = 5/5 + (6.5 + 60*0.125) * 15% = 1 + 2.1 = 3.1
//	budget = 2000/60 + 3.1 = 36.433 -> sustainable, ttoom = 2000/9.7 = 206.19
func TestPriestHealerGolden(t *testing.T) {
	in := priestInput()
	in.Character.Spellbook = []int{139, 2050}
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if out.Baseline.Metric != "hps" {
		t.Errorf("default metric = %q", out.Baseline.Metric)
	}
	almost(t, "hps", out.Baseline.Value, 46.5433, 0.001)
	if out.Baseline.TTOOMSec == nil {
		t.Fatal("ttoom missing")
	}
	almost(t, "ttoom", *out.Baseline.TTOOMSec, 206.19, 0.01)
}

// With the whole book the engine tries every known heal rank and keeps the
// best sustained healing: Flash Heal wins although mana clips it
// (82.33 HPS vs 77.91 with Heal; hand-computed from the fixtures).
func TestPriestRankChoice(t *testing.T) {
	out := Run(priestInput())
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	almost(t, "hps", out.Baseline.Value, 82.334, 0.01)
	found := false
	for _, row := range out.Baseline.Breakdown {
		if row.SpellID == 2061 {
			found = true
		}
		if row.SpellID == 2060 {
			t.Error("Greater Heal needs level 30")
		}
	}
	if !found {
		t.Errorf("Flash Heal not chosen: %+v", out.Baseline.Breakdown)
	}
}

// Rank numbers land on the breakdown, and the comfortable mana pool means
// the biggest heal is also the best: no downrank recommendation.
func TestPriestRanksAndNoDownrank(t *testing.T) {
	out := Run(priestInput())
	if out.Downrank != nil {
		t.Errorf("unexpected downrank: %+v", out.Downrank)
	}
	for _, row := range out.Baseline.Breakdown {
		if row.SpellID == 139 && row.Rank != 1 {
			t.Errorf("Renew rank = %d", row.Rank)
		}
	}
}

// A tiny mana pool turns the rank choice around: Lesser Heal rank 1
// sustains more healing than Flash Heal, and the answer says so.
func TestPriestDownrankRecommendation(t *testing.T) {
	in := priestInput()
	in.Character.Stats.ManaMax = 300
	in.Character.Stats.MP5 = 0
	in.Character.Stats.Spirit = 0
	in.Character.Talents = nil
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	d := out.Downrank
	if d == nil {
		t.Fatal("downrank recommendation missing")
	}
	if d.SpellID != 2050 || d.Rank != 1 || d.Name != "Lesser Heal" {
		t.Errorf("chosen = %+v", d)
	}
	// The comparison stays within the chain: the next higher known rank of
	// the same heal (Heal, rank 3 of the Lesser Heal chain), not Flash Heal.
	if d.InsteadID != 2054 || d.InsteadName != "Heal" || d.InsteadRank != 3 {
		t.Errorf("instead = %+v", d)
	}
	if d.GainPct <= 0 {
		t.Errorf("gain = %g", d.GainPct)
	}
}

// Every heal-rank candidate lands in the visible list with its efficiency
// numbers, sorted by what the rotation sustains; hand-checked: Lesser Heal
// rank 1 gives 92.25 healing for 30 mana over 2.5 s.
func TestHealOptions(t *testing.T) {
	out := Run(priestInput())
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if len(out.HealOptions) != 4 { // 2050, 2052, 2054 and 2061
		t.Fatalf("options: %+v", out.HealOptions)
	}
	if !out.HealOptions[0].Chosen || out.HealOptions[0].SpellID != 2061 {
		t.Errorf("first option should be the chosen Flash Heal: %+v", out.HealOptions[0])
	}
	for i := 1; i < len(out.HealOptions); i++ {
		if out.HealOptions[i].HPSSustained > out.HealOptions[i-1].HPSSustained {
			t.Error("options not sorted by sustained HPS")
		}
	}
	var lh *HealOption
	for i := range out.HealOptions {
		if out.HealOptions[i].SpellID == 2050 {
			lh = &out.HealOptions[i]
		}
	}
	if lh == nil {
		t.Fatal("Lesser Heal missing")
	}
	almost(t, "heal per cast", lh.HealPerCast, 92.25, 1e-9)
	almost(t, "mana", lh.ManaPerCast, 30, 1e-9)
	almost(t, "heal per mana", lh.HealPerMana, 92.25/30, 1e-9)
	almost(t, "throughput", lh.HPSThroughput, 92.25/2.5, 1e-9)
	if lh.Rank != 1 {
		t.Errorf("rank = %d", lh.Rank)
	}
}

// Monte-Carlo for a healer: the hps distribution comes back and the mean
// stays in the closed form's region (the loop may idle-regenerate, so it
// is allowed to sit a bit higher).
func TestPriestMonteCarloHPS(t *testing.T) {
	in := priestInput()
	in.Method = "montecarlo"
	in.Iterations = 500
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	mc := out.Baseline.MC
	if mc == nil || mc.Mean <= 0 || mc.StdDev <= 0 {
		t.Fatalf("mc = %+v", mc)
	}
	if mc.Mean < 50 || mc.Mean > 120 { // closed form: 82.3
		t.Errorf("hps mean out of range: %g", mc.Mean)
	}
}

// The healing metric only moves for stats a healer can use.
func TestPriestHealerDeltas(t *testing.T) {
	in := priestInput()
	in.Evaluations = []Evaluation{
		{Key: "healing", Delta: map[string]float64{"healing": 20}},
		{Key: "damage", Delta: map[string]float64{"spDamage": 50}},
		{Key: "spirit", Delta: map[string]float64{"spi": 20}},
	}
	out := Run(in)
	if out.Results["healing"].Gain <= 0 {
		t.Errorf("healing gain = %g", out.Results["healing"].Gain)
	}
	if out.Results["damage"].Gain != 0 {
		t.Errorf("damage sp must not heal: %g", out.Results["damage"].Gain)
	}
	if out.Results["spirit"].Gain <= 0 {
		t.Errorf("spirit gain with Meditation = %g", out.Results["spirit"].Gain)
	}

	// Without Meditation the model casts all fight, so spirit gains nothing.
	in.Character.Talents = nil
	out = Run(in)
	if out.Results["spirit"].Gain != 0 {
		t.Errorf("spirit gain without Meditation = %g", out.Results["spirit"].Gain)
	}
}

func TestPriestTTOOMMetric(t *testing.T) {
	in := priestInput()
	in.Metric = "ttoom"
	in.Evaluations = []Evaluation{{Key: "mp5", Delta: map[string]float64{"mp5": 10}}}
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	// Rank choice stays HPS-driven (Flash Heal), the metric reports its ttoom.
	almost(t, "ttoom value", out.Baseline.Value, 2000/40.9, 0.05)
	if out.Results["mp5"].Gain <= 0 {
		t.Errorf("mp5 gain under ttoom = %g", out.Results["mp5"].Gain)
	}
}

func TestPriestShadow(t *testing.T) {
	in := priestInput()
	in.Metric = "dps"
	in.Character.Talents = map[string]int{"15407": 1} // Mind Flay known through the talent
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	ids := map[int]bool{}
	for _, row := range out.Baseline.Breakdown {
		ids[row.SpellID] = true
	}
	if !ids[589] || !ids[8092] || !ids[15407] {
		t.Errorf("shadow rotation wrong: %+v", out.Baseline.Breakdown)
	}
	if out.Baseline.Value <= 0 {
		t.Error("no dps")
	}
}
