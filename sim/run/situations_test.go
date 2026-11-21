package run

import (
	"testing"

	"github.com/kejith/wow-forever-tools/sim/engine"
)

func situation(t *testing.T, out Output, key string) Situation {
	t.Helper()
	for _, s := range out.Situations {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("no situation %q", key)
	return Situation{}
}

func step(s Situation, kind string, id int) *Step {
	for i := range s.Steps {
		if s.Steps[i].Kind == kind && s.Steps[i].SpellID == id {
			return &s.Steps[i]
		}
	}
	return nil
}

// The rotation guide of the golden warlock: five fights against one enemy
// and two against several, each with the best of the candidate rotations
// played cast by cast.
func TestDamageSituations(t *testing.T) {
	if out := Run(input()); out.Situations != nil {
		t.Error("situations without being asked for")
	}
	in := input()
	in.Situations = true
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	keys := ""
	for _, s := range out.Situations {
		keys += s.Key + " "
		if s.Metric != "dps" || s.Candidates != 8 || len(s.Steps) == 0 || len(s.Timeline) == 0 {
			t.Errorf("%s: %+v", s.Key, s)
		}
	}
	if keys != "quick standard max boss endurance pack crowd " {
		t.Fatalf("situations: %s", keys)
	}
	// The gear rating is untouched by the guide.
	almost(t, "baseline", out.Baseline.Value, 45.8531, 0.01)

	// 15 s: both DoTs once, four bolts, the mana lasts.
	quick := situation(t, out, "quick")
	almost(t, "quick dps", quick.Value, 55.29, 0.01)
	if quick.OOMSec != nil || step(quick, "lifetap", 0) != nil || step(quick, "wand", 0) != nil {
		t.Errorf("quick: %+v", quick)
	}
	if b := step(quick, "filler", 695); b == nil || b.Casts != 4 || b.FirstSec != 3 || b.Rank != 2 {
		t.Errorf("quick bolts: %+v", b)
	}
	almost(t, "quick kills", quick.Fight.KillsPerMinute, 4, 1e-9)

	// The asked fight: two taps (the health allows no more), the wand from 33 s.
	std := situation(t, out, "standard")
	if std.Fight.Seconds != 60 || std.Fight.TargetDelta != 2 {
		t.Errorf("standard fight: %+v", std.Fight)
	}
	almost(t, "standard dps", std.Value, 44.2573, 0.01)
	if std.OOMSec == nil {
		t.Fatal("standard: the mana should run out")
	}
	almost(t, "standard oom", *std.OOMSec, 33, 1e-9)
	if tap := step(std, "lifetap", 0); tap == nil || tap.Casts != 2 || tap.Value != 0 {
		t.Errorf("taps: %+v", tap)
	}
	if w := step(std, "wand", 0); w == nil || w.FirstSec != 33 || w.Value <= 0 {
		t.Errorf("wand: %+v", w)
	}
	almost(t, "health left", std.EndHPPct, 60, 0.5)
	share, value := 0.0, 0.0
	for _, s := range std.Steps {
		share += s.SharePct
		value += s.Value
	}
	almost(t, "time shares", share, 100, 1e-6)
	almost(t, "values add up", value, std.Value, 1e-6)
	if std.Timeline[0].Kind != "dot" || std.Timeline[0].ManaPct != 100 || std.Timeline[0].N != 1 {
		t.Errorf("timeline: %+v", std.Timeline)
	}

	// The same fight if mana were no limit: no tap, no wand, bolts all the
	// time. The real mana pays for it until 22.5 s: 1000 - 40 - 50 + 6
	// regenerated = 916 at 3 s, six bolts (140 - 6 each) leave 112 at 21 s,
	// Corruption again 65, and the next bolt costs 140.
	max := situation(t, out, "max")
	if !max.FreeMana || max.Fight != std.Fight || std.FreeMana {
		t.Errorf("max: %+v", max)
	}
	almost(t, "max dps", max.Value, 57.9579, 0.01)
	if max.OOMSec == nil {
		t.Fatal("max: the real mana should run out")
	}
	almost(t, "max oom", *max.OOMSec, 22.5, 1e-9)
	if step(max, "lifetap", 0) != nil || step(max, "wand", 0) != nil {
		t.Errorf("max steps: %+v", max.Steps)
	}
	if b := step(max, "filler", 695); b == nil || b.Casts != 17 {
		t.Errorf("max bolts: %+v", b)
	}
	almost(t, "max timeline shows the real mana", max.Timeline[1].ManaPct, 96.3, 1e-6)
	almost(t, "max mana left", max.EndManaPct, 0, 1e-9)
	if max.Value <= std.Value {
		t.Errorf("max %g, standard %g", max.Value, std.Value)
	}

	// A boss: three levels above, no kills; ten minutes: a long timeline is cut.
	boss := situation(t, out, "boss")
	if boss.Fight.Seconds != 180 || boss.Fight.TargetDelta != 3 || boss.Fight.KillsPerMinute != 0 {
		t.Errorf("boss fight: %+v", boss.Fight)
	}
	if boss.Value >= std.Value {
		t.Errorf("boss %g, standard %g", boss.Value, std.Value)
	}
	long := situation(t, out, "endurance")
	if len(long.Timeline) != timelineEntries || long.More == 0 {
		t.Errorf("endurance timeline: %d entries, %d more", len(long.Timeline), long.More)
	}
}

// An enemy that dies in five seconds is not worth a DoT: the guide leaves
// them out and says what casting them would cost.
func TestSituationSkipsWhatDoesNotPay(t *testing.T) {
	in := input()
	in.Situations = true
	in.Fight = engine.Fight{Seconds: 5, TargetDelta: 2}
	out := Run(in)
	std := situation(t, out, "standard")
	if len(std.Steps) != 1 || std.Steps[0].Kind != "filler" {
		t.Fatalf("steps: %+v", std.Steps)
	}
	if len(std.Skipped) != 2 {
		t.Fatalf("skipped: %+v", std.Skipped)
	}
	for i, sk := range std.Skipped {
		if sk.Kind != "dot" || sk.Loss <= 0 || sk.Name == "" || sk.Rank != 1 {
			t.Errorf("skipped: %+v", sk)
		}
		if i > 0 && sk.Loss < std.Skipped[i-1].Loss {
			t.Error("skipped spells: the cheapest mistake first")
		}
	}
	// The gear is rated with the fixed rotation, which keeps its DoTs.
	if len(out.Baseline.Breakdown) < 3 {
		t.Errorf("baseline breakdown: %+v", out.Baseline.Breakdown)
	}
}

// A healer's guide is a ladder of demand: a quarter, half, three quarters
// and all of the most the priest can heal on one target, and half of the
// most on five. The fixture priest (100 healing, 5 % crit):
//
//	Renew       200 per 30 mana, 13.33 hps on a target for a tenth of the time
//	Flash Heal  143.5 per 1.5 s -> 95.67 hps while casting, the fastest
//	most on one target: 13.33 + 0.9 * 95.67 = 99.43 hps
func TestHealSituations(t *testing.T) {
	in := priestInput()
	in.Situations = true
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	keys := ""
	for _, s := range out.Situations {
		keys += s.Key + " "
		if s.Metric != "hps" || s.Demand == nil || len(s.Steps) == 0 || len(s.Timeline) != 0 {
			t.Fatalf("%s: %+v", s.Key, s)
		}
		almost(t, s.Key+" supplies the demand", s.Value, s.Demand.Value, 1e-6)
		almost(t, s.Key+" demand", s.Demand.Value, s.Demand.MaxValue*s.Demand.Pct/100, 1e-9)
		hps, mana := 0.0, 0.0
		for _, st := range s.Steps {
			hps += st.Value
			mana += st.ManaPS
		}
		almost(t, s.Key+" steps", hps, s.Value, 1e-6)
		almost(t, s.Key+" mana", mana, s.Demand.ManaPS, 1e-6)
	}
	if keys != "light steady heavy burst group " {
		t.Fatalf("situations: %s", keys)
	}

	// Light damage: Renew and now and then the heal with the most healing
	// per mana; two fifths of the time regenerate in full, the mana lasts.
	light := situation(t, out, "light")
	almost(t, "light", light.Value, 99.4333/4, 1e-3)
	if light.OOMSec != nil || len(light.Steps) != 2 || light.Steps[0].Kind != "hot" || light.Steps[1].SpellID != 2054 {
		t.Errorf("light: %+v", light.Steps)
	}
	almost(t, "light free", light.Demand.FreePct, 39.895, 0.01)
	if light.Demand.RegenPS <= light.Demand.ManaPS {
		t.Errorf("light balance: %+v", light.Demand)
	}

	// More demand costs more per point and lasts shorter.
	last := 1e18
	for _, key := range []string{"steady", "heavy", "burst"} {
		s := situation(t, out, key)
		if s.OOMSec == nil || *s.OOMSec >= last {
			t.Errorf("%s lasts %v, the level below %g", key, s.OOMSec, last)
			continue
		}
		last = *s.OOMSec
	}

	// Everything there is: Renew and Flash Heal, 44 mana/s against 3.1.
	burst := situation(t, out, "burst")
	almost(t, "burst", burst.Value, 99.4333, 1e-3)
	almost(t, "burst oom", *burst.OOMSec, 2000/(44-3.1), 1e-3)
	if f := step(burst, "heal", 2061); f == nil || f.PerCast != 143.5 || f.Rank != 1 {
		t.Errorf("burst: %+v", burst.Steps)
	}
	almost(t, "flash casts", step(burst, "heal", 2061).PerMin, 36, 1e-6)

	// Five targets: Renew on all of them is the most there (114.5 hps with
	// Flash Heal for the other half of the time); half of that is Renew alone.
	group := situation(t, out, "group")
	if group.Demand.Targets != 5 || len(group.Steps) != 1 || group.Steps[0].Kind != "hot" {
		t.Errorf("group: %+v", group)
	}
	almost(t, "group max", group.Demand.MaxValue, 5*13.3333+0.5*95.6667, 1e-3)
	almost(t, "group renews", group.Steps[0].PerMin, 17.175, 1e-3)
	almost(t, "group targets", group.Steps[0].Targets, 17.175/4, 1e-3)

	// The same priest asked for damage gets the fights instead.
	in.Metric = "dps"
	out = Run(in)
	if len(out.Situations) != 7 || out.Situations[0].Key != "quick" || out.Situations[0].Demand != nil {
		t.Errorf("damage situations: %+v", out.Situations)
	}
}
