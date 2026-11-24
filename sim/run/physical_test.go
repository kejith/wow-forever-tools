package run

import (
	"testing"

	"github.com/kejith/wow-tools-forever/sim/engine"
)

func TestPhase3PhysicalClassesAndHybridMelee(t *testing.T) {
	cache := realCacheDir(t)

	cases := []struct {
		name      string
		classFile string
		spec      string
		stats     Stats
		talents   map[string]int
		evalKey   string
		evalDelta map[string]float64
		minDPS    float64
	}{
		{
			name:      "Warrior Arms 2H",
			classFile: "WARRIOR",
			spec:      "arms",
			stats: Stats{
				Strength: 280, Agility: 110, AttackPower: 850, MeleeCrit: 22, MeleeHit: 6,
				HealthMax: 4200, ManaMax: 100,
				MainHand: engine.Weapon{Min: 180, Max: 280, Speed: 3.5, NormSpeed: 3.3, DPS: 65.7, Skill: 300},
			},
			evalKey: "str+20", evalDelta: map[string]float64{"str": 20},
			minDPS: 200,
		},
		{
			name:      "Warrior Fury Dual-Wield",
			classFile: "WARRIOR",
			spec:      "fury",
			stats: Stats{
				Strength: 270, Agility: 130, AttackPower: 820, MeleeCrit: 24, MeleeHit: 9,
				HealthMax: 4000, ManaMax: 100,
				MainHand: engine.Weapon{Min: 100, Max: 185, Speed: 2.6, NormSpeed: 2.4, DPS: 54.8, Skill: 300},
				OffHand:  engine.Weapon{Min: 75, Max: 135, Speed: 1.8, NormSpeed: 2.4, DPS: 58.3, Skill: 300},
			},
			evalKey: "ap+40", evalDelta: map[string]float64{"ap": 40},
			minDPS: 200,
		},
		{
			name:      "Rogue Combat Dual-Wield",
			classFile: "ROGUE",
			spec:      "combat",
			stats: Stats{
				Strength: 100, Agility: 320, AttackPower: 760, MeleeCrit: 26, MeleeHit: 9,
				HealthMax: 3500, ManaMax: 100,
				MainHand: engine.Weapon{Min: 105, Max: 195, Speed: 2.6, NormSpeed: 2.4, DPS: 57.7, Skill: 300},
				OffHand:  engine.Weapon{Min: 60, Max: 110, Speed: 1.6, NormSpeed: 1.7, DPS: 53.1, Skill: 300},
			},
			evalKey: "agi+20", evalDelta: map[string]float64{"agi": 20},
			minDPS: 220,
		},
		{
			name:      "Hunter Marksmanship",
			classFile: "HUNTER",
			spec:      "marksmanship",
			stats: Stats{
				Agility: 310, Intellect: 140, Spirit: 90, RangedAttackPower: 900, RangedCrit: 22, MeleeHit: 6,
				HealthMax: 3600, ManaMax: 3400, MP5: 15,
				Ranged: engine.Weapon{Min: 110, Max: 205, Speed: 2.9, NormSpeed: 2.8, DPS: 54.3, Skill: 300},
				PetDPS: 115,
			},
			evalKey: "rap+40", evalDelta: map[string]float64{"rap": 40},
			minDPS: 250,
		},
		{
			name:      "Druid Feral Cat",
			classFile: "DRUID",
			spec:      "feral",
			stats: Stats{
				Strength: 220, Agility: 260, AttackPower: 800, FeralAP: 180, MeleeCrit: 25, MeleeHit: 6,
				HealthMax: 3800, ManaMax: 2500,
			},
			evalKey: "feralAp+40", evalDelta: map[string]float64{"feralAp": 40},
			minDPS: 140,
		},
		{
			name:      "Shaman Enhancement",
			classFile: "SHAMAN",
			spec:      "enhancement",
			stats: Stats{
				Strength: 220, Agility: 140, Intellect: 140, Spirit: 110, AttackPower: 700, MeleeCrit: 18, MeleeHit: 6,
				SpellDamage: map[string]float64{"nature": 80, "fire": 80},
				SpellCrit:   map[string]float64{"nature": 10, "fire": 10},
				HealthMax: 3800, ManaMax: 3200,
				MainHand: engine.Weapon{Min: 165, Max: 255, Speed: 3.4, NormSpeed: 3.3, DPS: 61.8, Skill: 300},
			},
			evalKey: "str+20", evalDelta: map[string]float64{"str": 20},
			minDPS: 140,
		},
		{
			name:      "Paladin Retribution",
			classFile: "PALADIN",
			spec:      "retribution",
			stats: Stats{
				Strength: 250, Agility: 100, Intellect: 130, Spirit: 90, AttackPower: 740, MeleeCrit: 18, MeleeHit: 6,
				SpellDamage: map[string]float64{"holy": 90},
				SpellCrit:   map[string]float64{"holy": 10},
				HealthMax: 4000, ManaMax: 3000,
				MainHand: engine.Weapon{Min: 170, Max: 265, Speed: 3.5, NormSpeed: 3.3, DPS: 62.1, Skill: 300},
			},
			evalKey: "str+20", evalDelta: map[string]float64{"str": 20},
			minDPS: 120,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{
				CacheDir:    cache,
				Build:       "1.60.1.71037",
				Locale:      "enUS",
				Character:   Character{ClassFile: tc.classFile, Level: 60, SpecOverride: tc.spec, Stats: tc.stats, Talents: tc.talents},
				Fight:       engine.Fight{Seconds: 90, TargetDelta: 2},
				Metric:      "dps",
				Situations:  true,
				DamageTable: true,
				Evaluations: []Evaluation{
					{Key: tc.evalKey, Delta: tc.evalDelta},
					{Key: "crit+1", Delta: map[string]float64{"crit": 1}},
					{Key: "hit+1", Delta: map[string]float64{"hit": 1}},
				},
			}
			out := Run(in)
			if out.Error != "" {
				t.Fatalf("unexpected error: %s", out.Error)
			}
			if out.Baseline == nil || out.Baseline.Value < tc.minDPS {
				t.Fatalf("baseline = %v, want >= %.1f", out.Baseline, tc.minDPS)
			}
			if out.Results[tc.evalKey].Gain <= 0 {
				t.Errorf("expected positive gain for %s, got %.2f", tc.evalKey, out.Results[tc.evalKey].Gain)
			}
			if len(out.Situations) == 0 {
				t.Errorf("expected situations for %s", tc.name)
			}
			t.Logf("%s: baseline=%.1f DPS, %s=+%.2f, crit+1=+%.2f, hit+1=+%.2f, situations=%d",
				tc.name, out.Baseline.Value, tc.evalKey,
				out.Results[tc.evalKey].Gain, out.Results["crit+1"].Gain, out.Results["hit+1"].Gain,
				len(out.Situations))
		})
	}
}
