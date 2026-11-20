package run

import (
	"testing"

	"github.com/kejith/wow-forever-tools/sim/engine"
	"github.com/kejith/wow-forever-tools/sim/talents"
)

// A level-22 fixture priest with Power Word: Shield, Dark Sacrifice and the
// talents around them. The book: Renew, Lesser Heal rank 1, the shield and
// Dark Sacrifice.
func shieldInput() Input {
	in := priestInput()
	in.Character.Level = 22
	in.Character.Stats.HealthMax = 1000
	in.Character.Spellbook = []int{139, 2050, 17, 1277324}
	in.Character.Talents = map[string]int{
		"14521":  3, // Meditation: 15 % regeneration while casting
		"14748":  2, // Improved Power Word: Shield: +20 %
		"402000": 1, // Soul Warding: no cooldown, 15 % cheaper
		"15270":  2, // Spirit Tap: 40 % per kill
	}
	in.Fight.KillsPerMinute = -1 // automatic
	return in
}

func rowOf(m *engine.Metrics, id int) *engine.Row {
	for i := range m.Breakdown {
		if m.Breakdown[i].SpellID == id {
			return &m.Breakdown[i]
		}
	}
	return nil
}

// With mana to spare the shield joins the rotation. Hand-computed:
//
//	Shield: 100*1.2 + 0.1*100 = 130 for 85 mana, once per Weakened Soul (15 s)
//	Renew 200 per 15 s; both wait 0.8*2.5/2 = 1 s on the heal: cycles of 16 s
//	Lesser Heal keeps 1 - 2*1.5/16 = 0.8125 of the fight at 36.9 hps
//	HPS = 130/16 + 200/16 + 0.8125*36.9 = 50.60625 (without the shield: 45.8705)
func TestShieldInTheRotation(t *testing.T) {
	out := Run(shieldInput())
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	almost(t, "hps", out.Baseline.Value, 50.60625, 1e-4)
	shield := rowOf(out.Baseline, 17)
	if shield == nil || !shield.Absorb || shield.Rank != 1 {
		t.Fatalf("shield row = %+v in %+v", shield, out.Baseline.Breakdown)
	}
	almost(t, "shield hps", shield.Value, 130.0/16, 1e-6)
	// With and without the shield, with and without Renew.
	if out.Coverage.Candidates != 4 {
		t.Errorf("candidates = %d", out.Coverage.Candidates)
	}
	cov := out.Coverage
	if e := spellEntry(t, cov, 17); e.Status != talents.StatusUsed || e.Reason != "rotation" {
		t.Errorf("shield: %+v", e)
	}
	for _, id := range []int{14748, 402000} {
		if e := talentEntry(t, cov, id); e.Status != talents.StatusUsed || e.Reason != "mods" {
			t.Errorf("talent %d: %+v", id, e)
		}
	}
	// Enough mana: Dark Sacrifice is modeled, and not needed.
	if e := spellEntry(t, cov, 1277324); e.Status != talents.StatusIdle || e.Reason != "notneeded" {
		t.Errorf("dark sacrifice: %+v", e)
	}
	if rowOf(out.Baseline, 1277324) != nil {
		t.Error("Dark Sacrifice cast without need")
	}
	// A healer lands no killing blows: Spirit Tap cannot count.
	if out.Fight == nil || out.Fight.KillsPerMinute != 0 || out.Fight.Seconds != 60 {
		t.Errorf("fight = %+v", out.Fight)
	}
	if e := talentEntry(t, cov, 15270); e.Status != talents.StatusNone || e.Reason != "nokills" || e.Unverified {
		t.Errorf("spirit tap: %+v", e)
	}
	// The heal-rank list compares within the chosen rotation only.
	if len(out.HealOptions) != 1 || !out.HealOptions[0].Chosen {
		t.Errorf("heal options = %+v", out.HealOptions)
	}
}

// Short of mana the shield loses (130 absorb for 85 mana against 92 healing
// for 30), and Dark Sacrifice pays: 5*60 + 60 spirit = 360 mana for 200
// health, which comes off the healing. Only the mana that is missing is
// converted (TestPriestHealerGolden has the rotation):
//
//	rotation 12.7442 mana/s, budget 300/60 + 3.1 = 8.1 -> 4.6442 missing
//	a cast gives 360 mana and takes 1.5 s from the heal (12 mana/s):
//	4.6442 / (360 + 18) = 0.012286 casts per second
func TestDarkSacrificeWhenManaRunsShort(t *testing.T) {
	in := shieldInput()
	in.Character.Stats.ManaMax = 300
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if rowOf(out.Baseline, 17) != nil {
		t.Errorf("the shield should lose: %+v", out.Baseline.Breakdown)
	}
	if e := spellEntry(t, out.Coverage, 17); e.Status != talents.StatusIdle || e.Reason != "weaker" || e.Value <= 0 {
		t.Errorf("shield: %+v", e)
	}
	for _, id := range []int{14748, 402000} {
		if e := talentEntry(t, out.Coverage, id); e.Status != talents.StatusIdle {
			t.Errorf("talent %d: %+v", id, e)
		}
	}
	sac := rowOf(out.Baseline, 1277324)
	if sac == nil || sac.Kind != "mana" || sac.Rank != 1 {
		t.Fatalf("no Dark Sacrifice row: %+v", out.Baseline.Breakdown)
	}
	almost(t, "casts per minute", sac.PerMin, 0.012286*60, 1e-4)
	almost(t, "mana per second", sac.ManaGain, 0.012286*360, 1e-3)
	almost(t, "health per second", sac.HealthCost, 0.012286*200, 1e-3)
	if e := spellEntry(t, out.Coverage, 1277324); e.Status != talents.StatusUsed || e.Reason != "regen" {
		t.Errorf("dark sacrifice: %+v", e)
	}
	with := out.Baseline.Value

	in.Character.Spellbook = []int{139, 2050, 17}
	out = Run(in)
	if with <= out.Baseline.Value {
		t.Errorf("Dark Sacrifice should raise the healing: %g vs %g", with, out.Baseline.Value)
	}

	// The time-to-OOM metric counts the cast as well.
	in.Metric = "ttoom"
	without := Run(in).Baseline.Value
	in.Character.Spellbook = []int{139, 2050, 17, 1277324}
	out = Run(in)
	if out.Baseline.Value <= without {
		t.Errorf("ttoom with Dark Sacrifice %g, without %g", out.Baseline.Value, without)
	}
	if e := spellEntry(t, out.Coverage, 1277324); e.Status != talents.StatusUsed {
		t.Errorf("dark sacrifice under ttoom: %+v", e)
	}
}

// Spirit Tap counts for the kills the fight assumes: none for a healer, one
// per fight for the damage rotation against normal enemies, none against a
// boss, or what the caller sets.
func TestSpiritTapKills(t *testing.T) {
	in := shieldInput()
	in.Character.Stats.ManaMax = 300
	base := Run(in).Baseline.Value

	in.Fight.KillsPerMinute = 4 // one kill per buff duration: up 40 % of the time
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	e := talentEntry(t, out.Coverage, 15270)
	if e.Status != talents.StatusUsed || e.Reason != "kills" || !e.Unverified || e.Note == "" {
		t.Errorf("spirit tap: %+v", e)
	}
	almost(t, "uptime", e.Value, 40, 1e-9)
	almost(t, "reported uptime", out.Baseline.KillBuffPct, 40, 1e-9)
	if out.Baseline.Value <= base {
		t.Errorf("more regeneration should heal more: %g vs %g", out.Baseline.Value, base)
	}

	in.Metric = "dps"
	in.Fight.KillsPerMinute = -1
	in.Character.Spellbook = nil
	out = Run(in)
	if out.Fight.KillsPerMinute != 1 {
		t.Errorf("automatic kills for damage = %g", out.Fight.KillsPerMinute)
	}
	if e := talentEntry(t, out.Coverage, 15270); e.Status != talents.StatusUsed {
		t.Errorf("spirit tap under dps: %+v", e)
	}
	in.Fight.TargetDelta = 3
	out = Run(in)
	if out.Fight.KillsPerMinute != 0 {
		t.Errorf("a boss fight has no kills: %g", out.Fight.KillsPerMinute)
	}
}

// The Monte-Carlo run plays the shield and the conversion too.
func TestShieldMonteCarlo(t *testing.T) {
	in := shieldInput()
	in.Method = "montecarlo"
	in.Iterations = 300
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	mc := out.Baseline.MC
	if mc == nil || mc.Mean < 45 || mc.Mean > 56 { // closed form: 50.6
		t.Errorf("mc = %+v", mc)
	}
}
