package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kejith/wow-forever-tools/sim/engine"
	"github.com/kejith/wow-forever-tools/sim/talents"
)

// A level-40 fixture warlock who knows all three fillers (Shadow Bolt,
// Drain Life from the trainer, Wrack from the talent).
func coverageInput() Input {
	in := input()
	in.Character.Level = 40
	in.Character.Stats.ManaMax = 3000
	in.Character.Stats.HealthMax = 2000
	in.Character.Talents = map[string]int{
		"1316697": 1, // Wrack: a spell one casts, and the DoTs tick faster
		"17804":   2, // Soul Siphon: Drain Life and Wrack gain per DoT
		"18271":   1, // Shadow Mastery: +10 % shadow damage
		"9999":    1, // -0.5 s Shadow Bolt cast time
		"7778":    1, // hit: on the sheet
		"7779":    1, // an aura the decoder does not know
	}
	return in
}

func spellEntry(t *testing.T, cov *Coverage, id int) CoverageEntry {
	t.Helper()
	for _, e := range cov.Spells {
		if e.SpellID == id {
			return e
		}
	}
	t.Fatalf("spell %d missing in %+v", id, cov.Spells)
	return CoverageEntry{}
}

func talentEntry(t *testing.T, cov *Coverage, id int) CoverageEntry {
	t.Helper()
	for _, e := range cov.Talents {
		if e.SpellID == id {
			return e
		}
	}
	t.Fatalf("talent %d missing in %+v", id, cov.Talents)
	return CoverageEntry{}
}

// A rule that is an assumption is flagged where it is applied (Shadow
// Weaving: five stacks, and the rotation casts shadow spells).
func TestCoverageFlagsAssumptions(t *testing.T) {
	in := coverageInput()
	in.Character.Talents["15257"] = 1
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if e := talentEntry(t, out.Coverage, 15257); !e.Unverified || e.Note == "" || e.Status != talents.StatusUsed {
		t.Errorf("shadow weaving: %+v", e)
	}
}

// Every known spell and every talent gets a verdict: cast, tried and
// weaker, on the sheet, not modeled, or without influence.
func TestCoverage(t *testing.T) {
	out := Run(coverageInput())
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	cov := out.Coverage
	if cov == nil {
		t.Fatal("coverage missing")
	}
	if cov.TalentValues != "tables" {
		t.Errorf("talent values = %q", cov.TalentValues)
	}
	if cov.Candidates != 5 { // one rotation per filler, two of them also with Drain Life standing by
		t.Errorf("candidates = %d", cov.Candidates)
	}

	// The DoTs are cast; of the three fillers one is, and the other two
	// were tried: Shadow Bolt with a lower result, Drain Life also standing
	// by next to the best rotation, which this fight does not need.
	for _, id := range []int{172, 980} {
		if e := spellEntry(t, cov, id); e.Status != talents.StatusUsed || e.Reason != "rotation" {
			t.Errorf("dot %d: %+v", id, e)
		}
	}
	used, idle := 0, 0
	usedID := 0
	for _, id := range []int{695, 689, 1316697} {
		switch e := spellEntry(t, cov, id); e.Status {
		case talents.StatusUsed:
			used++
			usedID = id
		case talents.StatusIdle:
			idle++
			want := "weaker"
			if id == 689 {
				want = "nogain"
			}
			if e.Reason != want || e.Value <= 0 || e.Value > out.Baseline.Value || (want == "weaker" && e.Value == out.Baseline.Value) {
				t.Errorf("idle filler %d: %+v (baseline %g)", id, e, out.Baseline.Value)
			}
		default:
			t.Errorf("filler %d: %+v", id, e)
		}
	}
	if used != 1 || idle != 2 {
		t.Errorf("fillers used %d, idle %d", used, idle)
	}
	// The filler in the breakdown is the one the coverage calls used.
	inBreakdown := false
	for _, row := range out.Baseline.Breakdown {
		if row.Kind == "filler" && row.SpellID == usedID {
			inBreakdown = true
		}
	}
	if !inBreakdown {
		t.Errorf("used filler %d not in the breakdown %+v", usedID, out.Baseline.Breakdown)
	}
	// Shadow Bolt rank 1 has no entry of its own: one line per spell.
	for _, e := range cov.Spells {
		if e.SpellID == 686 {
			t.Error("lower ranks must not be listed")
		}
		if e.Name == "" {
			t.Errorf("spell without a name: %+v", e)
		}
	}

	// Talents.
	if e := talentEntry(t, cov, 18271); e.Status != talents.StatusUsed || e.Name != "Shadow Mastery" || e.Rank != 1 {
		t.Errorf("shadow mastery: %+v", e)
	}
	if e := talentEntry(t, cov, 7778); e.Status != talents.StatusSheet {
		t.Errorf("hit talent: %+v", e)
	}
	if e := talentEntry(t, cov, 7779); e.Status != talents.StatusUnmodeled || e.Reason != "unknown" {
		t.Errorf("unknown talent: %+v", e)
	}
	// A talent that is a spell follows that spell.
	wrack, wrackSpell := talentEntry(t, cov, 1316697), spellEntry(t, cov, 1316697)
	if wrack.Status != wrackSpell.Status || wrack.Reason != wrackSpell.Reason {
		t.Errorf("wrack talent %+v vs spell %+v", wrack, wrackSpell)
	}
	if wrack.Unverified || wrack.Note == "" {
		t.Errorf("wrack is confirmed in the game and keeps its note: %+v", wrack)
	}
	// A talent that changes spells is used when the rotation casts one of
	// them, idle when only a losing candidate did.
	bolt := talentEntry(t, cov, 9999)
	if usedID == 695 && (bolt.Status != talents.StatusUsed || bolt.Reason != "mods") {
		t.Errorf("cast time talent with Shadow Bolt cast: %+v", bolt)
	}
	if usedID != 695 && bolt.Status != talents.StatusIdle {
		t.Errorf("cast time talent without Shadow Bolt: %+v", bolt)
	}
	siphon := talentEntry(t, cov, 17804)
	if usedID == 695 && siphon.Status != talents.StatusIdle {
		t.Errorf("soul siphon with Shadow Bolt cast: %+v", siphon)
	}
	if usedID != 695 && siphon.Status != talents.StatusUsed {
		t.Errorf("soul siphon with a drain cast: %+v", siphon)
	}
}

// With only Shadow Bolt in the book, a talent for another spell has
// nothing to work on.
func TestCoverageTalentWithoutItsSpell(t *testing.T) {
	in := coverageInput()
	in.Character.Spellbook = []int{695, 172}
	in.Character.Talents = map[string]int{"17804": 2}
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if e := talentEntry(t, out.Coverage, 17804); e.Status != talents.StatusNone || e.Reason != "nospell" {
		t.Errorf("soul siphon without a drain: %+v", e)
	}
	if out.Coverage.Candidates != 1 {
		t.Errorf("candidates = %d", out.Coverage.Candidates)
	}
}

// A healer's damage spells cannot move the healing metric; the heals the
// rank choice tried and dropped are listed with what they would sustain.
func TestCoveragePriest(t *testing.T) {
	out := Run(priestInput())
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	cov := out.Coverage
	if e := spellEntry(t, cov, 585); e.Status != talents.StatusNone {
		t.Errorf("smite under hps: %+v", e)
	}
	if e := spellEntry(t, cov, 2061); e.Status != talents.StatusUsed {
		t.Errorf("flash heal: %+v", e)
	}
	if e := spellEntry(t, cov, 2054); e.Status != talents.StatusIdle || e.Value <= 0 {
		t.Errorf("heal: %+v", e)
	}
	if e := talentEntry(t, cov, 14521); e.Status != talents.StatusUsed || e.Reason != "regen" || e.Name != "Meditation" {
		t.Errorf("meditation: %+v", e)
	}
}

func TestRelevant(t *testing.T) {
	heal := &engine.Spell{IsHeal: true, Direct: &engine.Effect{}}
	dmg := &engine.Spell{Direct: &engine.Effect{}}
	shield := &engine.Spell{Absorb: &engine.Effect{BP: 44}}
	mana := &engine.Spell{GivesMana: true}
	buff := &engine.Spell{}
	for _, c := range []struct {
		sp     *engine.Spell
		metric string
		want   bool
		what   string
	}{
		{heal, "hps", true, "heal"}, {heal, "dps", false, "heal"},
		{dmg, "dps", true, "damage"}, {dmg, "hps", false, "damage"},
		{shield, "hps", true, "absorb"}, {shield, "dps", false, "absorb"},
		{mana, "dps", true, "mana"}, {mana, "ttoom", true, "mana"},
		{buff, "dps", false, ""},
	} {
		if ok, what := relevant(c.sp, c.metric); ok != c.want || what != c.what {
			t.Errorf("relevant(%+v, %s) = %v %q", c.sp, c.metric, ok, what)
		}
	}
}

// Drain Life as the only filler: the breakdown and the baseline carry the
// health it returns.
func TestSelfHealing(t *testing.T) {
	in := coverageInput()
	in.Character.Spellbook = []int{172, 689}
	in.Character.Talents = nil
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if out.Baseline.SelfHealPS <= 0 {
		t.Errorf("self healing = %g", out.Baseline.SelfHealPS)
	}
	for _, row := range out.Baseline.Breakdown {
		if row.SpellID == 689 {
			almost(t, "drain life returns its damage", row.Leech, row.Value, 1e-9)
			return
		}
	}
	t.Errorf("Drain Life missing: %+v", out.Baseline.Breakdown)
}

// The trait tables decide the rank's value: Meditation 1/3 is 5 %, 3/3 15 %.
// budget = 2000/60 + 5/5 + (6.5 + 60*0.125) * pct
func TestTalentRankValues(t *testing.T) {
	in := priestInput()
	in.Character.Talents = map[string]int{"14521": 1}
	out := Run(in)
	almost(t, "budget at rank 1", out.Baseline.ManaBudgetPS, 2000.0/60+1+14*0.05, 1e-6)
	out = Run(priestInput())
	almost(t, "budget at rank 3", out.Baseline.ManaBudgetPS, 2000.0/60+1+14*0.15, 1e-6)
}

// Without the trait tables in the cache the answer says that the talent
// values are the spells' own numbers.
func TestCoverageWithoutTraitTables(t *testing.T) {
	dir := t.TempDir()
	files, _ := filepath.Glob("../testdata/*.csv")
	for _, f := range files {
		name := filepath.Base(f)
		if strings.HasPrefix(name, "Trait") || strings.HasPrefix(name, "CurvePoint") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	in := priestInput()
	in.CacheDir = dir
	out := Run(in)
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	if out.Coverage.TalentValues != "basepoints" {
		t.Errorf("talent values = %q", out.Coverage.TalentValues)
	}
	almost(t, "budget with the spell's own 5 %", out.Baseline.ManaBudgetPS, 2000.0/60+1+14*0.05, 1e-6)
}

// Curse of the Elements comes from its aura (87: percent damage taken, the
// schools in the misc value): a rotation with it in front is tried next to
// every rotation without, and the curse's row says what it does.
func TestCurseOfElements(t *testing.T) {
	in := coverageInput()
	in.Character.Level = 45
	in.Character.Talents = nil
	in.Character.Stats.ManaMax = 1e6
	in.Character.Spellbook = []int{695, 172}
	plain := Run(in)
	in.Character.Spellbook = []int{695, 172, 440892}
	out := Run(in)
	if plain.Error != "" || out.Error != "" {
		t.Fatal(plain.Error, out.Error)
	}
	if out.Coverage.Candidates != 2*plain.Coverage.Candidates {
		t.Errorf("candidates %d, without the curse %d", out.Coverage.Candidates, plain.Coverage.Candidates)
	}
	first := out.Baseline.Breakdown[0]
	if first.Kind != "debuff" || first.SpellID != 440892 || first.TakenPct != 10 || first.Value != 0 {
		t.Errorf("first row: %+v", first)
	}
	// One cast per fight is paid back by 10 % on everything after it.
	if gain := out.Baseline.Value / plain.Baseline.Value; gain < 1.05 || gain > 1.1 {
		t.Errorf("gain %g (%g against %g)", gain, out.Baseline.Value, plain.Baseline.Value)
	}
	if e := spellEntry(t, out.Coverage, 440892); e.Status != talents.StatusUsed || e.Reason != "rotation" {
		t.Errorf("coverage: %+v", e)
	}

	// In a fight of a few seconds the cast costs more than it brings: the
	// curse was tried and left out.
	in.Fight.Seconds = 8
	short := Run(in)
	for _, row := range short.Baseline.Breakdown {
		if row.Kind == "debuff" {
			t.Errorf("curse in an 8 s fight: %+v", short.Baseline.Breakdown)
		}
	}
	if e := spellEntry(t, short.Coverage, 440892); e.Status != talents.StatusIdle || e.Reason != "weaker" || e.Value <= 0 {
		t.Errorf("coverage in the short fight: %+v", e)
	}
}
