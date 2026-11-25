package run

import (
	"testing"
)

func TestSetBonusBaselineAndEvaluations(t *testing.T) {
	// Baseline without set bonuses.
	plain := input()
	plain.Evaluations = []Evaluation{
		{Key: "gainMod", HasSetChange: true, SetBonusSpells: []int{18271}}, // +10% shadow damage (non-sheet)
		{Key: "gainStat", Delta: map[string]float64{"spDamage": 18}},
		{Key: "gainBoth", Delta: map[string]float64{"spDamage": 18}, HasSetChange: true, SetBonusSpells: []int{18271}},
		{Key: "spellHitOnly", Delta: map[string]float64{"spellHit": 2}},
		{Key: "spellCritShadow", Delta: map[string]float64{"spellCritShadow": 2}},
	}
	outPlain := Run(plain)
	if outPlain.Error != "" {
		t.Fatal(outPlain.Error)
	}
	if outPlain.Results["gainMod"].Gain <= 0 {
		t.Errorf("activating +10%% shadow set bonus should increase DPS, got gain=%g", outPlain.Results["gainMod"].Gain)
	}
	if outPlain.Results["gainBoth"].Gain <= outPlain.Results["gainMod"].Gain ||
		outPlain.Results["gainBoth"].Gain <= outPlain.Results["gainStat"].Gain {
		t.Errorf("combined stat+mod set bonus gain=%g should beat each part (mod=%g, stat=%g)",
			outPlain.Results["gainBoth"].Gain, outPlain.Results["gainMod"].Gain, outPlain.Results["gainStat"].Gain)
	}
	if outPlain.Results["spellHitOnly"].Gain <= 0 {
		t.Errorf("spellHit delta should increase caster DPS, got %g", outPlain.Results["spellHitOnly"].Gain)
	}
	if outPlain.Results["spellCritShadow"].Gain <= 0 {
		t.Errorf("spellCritShadow delta should increase shadow DPS, got %g", outPlain.Results["spellCritShadow"].Gain)
	}

	// Baseline WITH active set bonuses: 18271 (non-sheet +10% shadow), 7778 (sheet +1% hit), 7779 (unmodeled).
	with := input()
	with.Character.SetBonusSpells = []int{18271, 7778, 7779}
	with.Evaluations = []Evaluation{
		{Key: "keep", Delta: map[string]float64{}},
		{Key: "loseMod", HasSetChange: true, SetBonusSpells: nil},
	}
	outWith := Run(with)
	if outWith.Error != "" {
		t.Fatal(outWith.Error)
	}
	if outWith.Baseline.Value <= outPlain.Baseline.Value {
		t.Errorf("baseline with +10%% shadow set bonus (%g) must exceed plain baseline (%g)",
			outWith.Baseline.Value, outPlain.Baseline.Value)
	}
	almost(t, "losing set bonus returns to plain baseline", outWith.Results["loseMod"].Value, outPlain.Baseline.Value, 0.01)
	if outWith.Results["loseMod"].Gain >= 0 {
		t.Errorf("losing active set bonus should be negative gain, got %g", outWith.Results["loseMod"].Gain)
	}

	// Coverage must report all three active set bonuses with their distinct statuses.
	if outWith.Coverage == nil || len(outWith.Coverage.SetBonuses) != 3 {
		t.Fatalf("expected 3 set bonus coverage entries, got %+v", outWith.Coverage)
	}
	statusByID := map[int]string{}
	for _, e := range outWith.Coverage.SetBonuses {
		statusByID[e.SpellID] = e.Status
	}
	if statusByID[18271] != "used" {
		t.Errorf("18271 status = %q, want used", statusByID[18271])
	}
	if statusByID[7778] != "sheet" {
		t.Errorf("7778 status = %q, want sheet", statusByID[7778])
	}
	if statusByID[7779] != "unmodeled" {
		t.Errorf("7779 status = %q, want unmodeled", statusByID[7779])
	}
}
