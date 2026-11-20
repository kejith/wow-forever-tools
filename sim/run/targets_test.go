package run

import (
	"testing"

	"github.com/kejith/wow-forever-tools/sim/talents"
)

// A warlock who knows the two spells for several enemies. In the fixture
// Hellfire (1949) channels 15 s and casts "Hellfire Effect" (5857, 40 + 10 %
// of the bonus) on the enemies every second while its own periodic effect
// (90 + 10 %) burns the caster; Rain of Fire (5740) channels 8 s with a
// dummy aura pulsing every 2 s, and the damage (30 + 10 %) sits in another
// spell of the same name (1282380).
func areaInput() Input {
	in := input()
	in.Character.Level = 50
	in.Character.Stats.SpellDamage["fire"] = 100
	in.Character.Stats.ManaMax = 3000
	in.Character.Stats.HealthMax = 1500
	in.Character.Spellbook = []int{695, 172, 1949, 5740}
	in.Situations = true
	return in
}

func numbersOf(t *testing.T, out Output, id int) SpellNumbers {
	t.Helper()
	for _, s := range out.Spells {
		if s.SpellID == id {
			return s
		}
	}
	t.Fatalf("spell %d missing in %+v", id, out.Spells)
	return SpellNumbers{}
}

// The class module joins the channel and its damage spell, so the numbers of
// one cast are the tooltip's: Hellfire 50 a second, Rain of Fire 160 over
// its four pulses.
func TestAreaSpellNumbers(t *testing.T) {
	out := Run(areaInput())
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	hell := numbersOf(t, out, 1949)
	if hell.Periodic != 750 || hell.Ticks != 15 || hell.Mana != 600 || hell.CastSec != 15 {
		t.Errorf("Hellfire: %+v", hell)
	}
	rain := numbersOf(t, out, 5740)
	if rain.Periodic != 160 || rain.Ticks != 4 || rain.Mana != 300 || rain.CastSec != 8 {
		t.Errorf("Rain of Fire: %+v", rain)
	}

	// Against one enemy neither is offered: the gear rating is the one of a
	// warlock without them, and the coverage says where they are played.
	plain := areaInput()
	plain.Character.Spellbook = []int{695, 172}
	without := Run(plain)
	almost(t, "baseline", out.Baseline.Value, without.Baseline.Value, 1e-9)
	if out.Coverage.Candidates != without.Coverage.Candidates {
		t.Errorf("candidates %d, without the area spells %d", out.Coverage.Candidates, without.Coverage.Candidates)
	}
	for _, id := range []int{1949, 5740} {
		if e := spellEntry(t, out.Coverage, id); e.Status != talents.StatusIdle || e.Reason != "area" {
			t.Errorf("coverage of %d: %+v", id, e)
		}
	}
	for _, key := range []string{"quick", "standard", "boss", "endurance"} {
		a, b := situation(t, out, key), situation(t, without, key)
		if a.Value != b.Value || a.Candidates != b.Candidates || a.Fight.Targets != 0 {
			t.Errorf("%s: %g of %d against %g of %d", key, a.Value, a.Candidates, b.Value, b.Candidates)
		}
	}
}

// Three enemies for 30 s, hit 95 %:
//
//	Corruption on each at 0, 1.5 and 3 s, again when it runs out (18.5, 20,
//	21.5 s): 18 + 8 ticks of 38 = 988
//	Hellfire at 4.5 s for the six ticks the health above 60 % pays for
//	(600 of 1500, 100 a tick): 6 * 47.5 * 3 = 855
//	Rain of Fire at 10.5 s and at 23 s, the second cut after 7 of its 8 s:
//	(1 + 7/8) * 4 * 38 * 3 = 855
//	together 2698 / 30 s = 89.9333 dps
func TestPackSituation(t *testing.T) {
	out := Run(areaInput())
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	pack := situation(t, out, "pack")
	if pack.Fight.Targets != 3 || pack.Fight.Seconds != 30 || pack.Fight.TargetDelta != 2 || pack.Candidates != 12 {
		t.Errorf("pack: %+v, %d candidates", pack.Fight, pack.Candidates)
	}
	almost(t, "pack dps", pack.Value, 2698.0/30, 1e-9)
	dot := step(pack, "dot", 172)
	if dot == nil || dot.Targets != 3 || dot.Casts != 6 || dot.Area {
		t.Fatalf("Corruption: %+v", dot)
	}
	almost(t, "Corruption", dot.Value, 988.0/30, 1e-9)
	burn := step(pack, "burn", 1949)
	if burn == nil || !burn.Area || burn.Casts != 1 || burn.FirstSec != 4.5 || burn.HealthPerSec != 100 || burn.Targets != 0 {
		t.Fatalf("Hellfire: %+v", burn)
	}
	almost(t, "Hellfire", burn.Value, 28.5, 1e-9)
	almost(t, "Hellfire's share of the time", burn.SharePct, 20, 1e-9)
	almost(t, "the health it stops at", burn.FloorPct, 60, 1e-9)
	rain := step(pack, "filler", 5740)
	if rain == nil || !rain.Area || rain.Casts != 2 || rain.HealthPerSec != 0 {
		t.Fatalf("Rain of Fire: %+v", rain)
	}
	almost(t, "Rain of Fire", rain.Value, 28.5, 1e-9)
	almost(t, "health left", pack.EndHPPct, 60, 1e-9)
	if len(pack.Skipped) != 1 || pack.Skipped[0].SpellID != 695 {
		t.Fatalf("left out: %+v", pack.Skipped)
	}
	almost(t, "bolts instead of the rain", pack.Skipped[0].Loss, 5.225, 1e-9)

	// The timeline folds the three Corruptions into one entry.
	if e := pack.Timeline[0]; e.Kind != "dot" || e.N != 3 || pack.Timeline[1].Kind != "burn" || pack.Timeline[1].T != 4.5 {
		t.Errorf("timeline: %+v", pack.Timeline)
	}
	if e := pack.Timeline[2]; e.Kind != "filler" || e.SpellID != 5740 || e.HPPct != 60 {
		t.Errorf("after the channel: %+v", e)
	}

	// Six enemies for 20 s: Corruption on all six (30 ticks), Hellfire at
	// 9 s, 5 of the rain's 8 s: (1140 + 1710 + 570) / 20 = 171 dps.
	crowd := situation(t, out, "crowd")
	if crowd.Fight.Targets != 6 || crowd.Fight.Seconds != 20 {
		t.Errorf("crowd: %+v", crowd.Fight)
	}
	almost(t, "crowd dps", crowd.Value, 171, 1e-9)
	if dot := step(crowd, "dot", 172); dot == nil || dot.Targets != 6 {
		t.Errorf("Corruption on six: %+v", dot)
	}

	// Without the health for a third of the channel Hellfire stays out, and
	// the guide says what it would have brought: nothing.
	in := areaInput()
	in.Character.Stats.HealthMax = 1000
	weak := situation(t, Run(in), "pack")
	if step(weak, "burn", 1949) != nil {
		t.Errorf("Hellfire with 400 health to spend: %+v", weak.Steps)
	}

	// A warlock without the two plays the pack with what there is.
	in = areaInput()
	in.Character.Spellbook = []int{695, 172}
	plain := situation(t, Run(in), "pack")
	if plain.Candidates != 4 || step(plain, "dot", 172).Targets != 3 || step(plain, "filler", 695).Area {
		t.Errorf("pack without area spells: %+v", plain.Steps)
	}
	if plain.Value >= pack.Value {
		t.Errorf("pack without area spells %g, with them %g", plain.Value, pack.Value)
	}
}
