package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/engine"
)

func realCacheDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "scripts", "cache"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SpellEffect-1.60.1.71037.csv")); err != nil {
		t.Skip("Forever DB2 cache not present")
	}
	return dir
}

func TestPhase2CastersAndHealers(t *testing.T) {
	cache := realCacheDir(t)
	baseStats := Stats{
		SpellDamage: map[string]float64{"fire": 250, "frost": 250, "arcane": 250, "nature": 250, "holy": 250, "shadow": 250},
		SpellCrit:   map[string]float64{"fire": 15, "frost": 15, "arcane": 15, "nature": 15, "holy": 15, "shadow": 15},
		SpellHit:    4,
		Healing:     350,
		Intellect:   250,
		Spirit:      180,
		MP5:         20,
		HealthMax:   3200,
		ManaMax:     5500,
		CritPerInt:  0.016,
		ManaPerInt:  15,
		HPPerSta:    10,
		Wand:        engine.Wand{DPS: 60, Speed: 1.5, Skill: 300},
	}

	cases := []struct {
		name      string
		classFile string
		spec      string
		metric    string
		talents   map[string]int
		wantTable string // "damage", "heal"
		minVal    float64
	}{
		{"Mage Frost DPS", "MAGE", "frost", "dps", map[string]int{"11151": 3, "11207": 5, "11070": 5}, "damage", 200},
		{"Mage Fire DPS + Ignite", "MAGE", "fire", "dps", map[string]int{"11124": 5, "11119": 5, "29074": 3}, "damage", 200},
		{"Druid Balance DPS", "DRUID", "balance", "dps", map[string]int{"24858": 1, "16909": 5}, "damage", 150},
		{"Druid Restoration HPS", "DRUID", "restoration", "hps", map[string]int{"17106": 3}, "heal", 150},
		{"Shaman Elemental DPS", "SHAMAN", "elemental", "dps", map[string]int{"16035": 5, "16089": 1}, "damage", 150},
		{"Shaman Restoration HPS", "SHAMAN", "restoration", "hps", map[string]int{"16178": 5, "16179": 5}, "heal", 150},
		{"Paladin Holy HPS + Illumination", "PALADIN", "holy", "hps", map[string]int{"20237": 3, "20210": 5}, "heal", 150},
		{"Paladin Holy DPS", "PALADIN", "holy", "dps", nil, "damage", 30},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{
				CacheDir:    cache,
				Build:       "1.60.1.71037",
				Locale:      "enUS",
				Character:   Character{ClassFile: tc.classFile, Level: 60, SpecOverride: tc.spec, Stats: baseStats, Talents: tc.talents},
				Fight:       engine.Fight{Seconds: 90, TargetDelta: 2},
				Metric:      tc.metric,
				Situations:  true,
				DamageTable: tc.wantTable == "damage",
				HealTable:   tc.wantTable == "heal",
				Evaluations: []Evaluation{
					{Key: "sp+20", Delta: map[string]float64{"sp": 20}},
					{Key: "heal+30", Delta: map[string]float64{"healing": 30}},
					{Key: "int+20", Delta: map[string]float64{"int": 20}},
				},
			}
			out := Run(in)
			if out.Error != "" {
				t.Fatalf("unexpected error: %s", out.Error)
			}
			if out.Baseline == nil || out.Baseline.Value < tc.minVal {
				t.Fatalf("baseline = %v, want >= %.1f", out.Baseline, tc.minVal)
			}
			if len(out.Situations) == 0 {
				t.Errorf("expected situations for %s", tc.name)
			}
			if tc.wantTable == "damage" && (out.DamageTable == nil || len(out.DamageTable.Rows) == 0) {
				t.Errorf("expected non-empty DamageTable for %s", tc.name)
			}
			if tc.wantTable == "heal" && (out.HealTable == nil || len(out.HealTable.Rows) == 0) {
				t.Errorf("expected non-empty HealTable for %s", tc.name)
			}
			t.Logf("%s: baseline=%.1f (%s), situations=%d, sp+20=%.2f, heal+30=%.2f",
				tc.name, out.Baseline.Value, tc.metric, len(out.Situations),
				out.Results["sp+20"].Gain, out.Results["heal+30"].Gain)
		})
	}
}
