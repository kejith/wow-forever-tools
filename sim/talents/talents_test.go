package talents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/engine"
)

func decodeFrom(t *testing.T, dir, class string, ranks map[int]int) (engine.Mods, map[int]Info) {
	t.Helper()
	cat, err := catalog.Build(dir, "9.9.9.9", "xxXX", class)
	if err != nil {
		t.Fatal(err)
	}
	mods, infos, err := Decode(cat, ranks)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int]Info{}
	for i, info := range infos {
		if i > 0 && infos[i-1].SpellID >= info.SpellID {
			t.Error("infos are not ordered by spell id")
		}
		byID[info.SpellID] = info
	}
	return mods, byID
}

func decode(t *testing.T, ranks map[int]int) (engine.Mods, map[int]Info) {
	return decodeFrom(t, "../testdata", "WARLOCK", ranks)
}

func TestGenericSchoolDamage(t *testing.T) {
	mods, infos := decode(t, map[int]int{18271: 1})
	if mods.SchoolDamagePct[engine.Shadow] != 10 {
		t.Errorf("shadow pct = %g", mods.SchoolDamagePct[engine.Shadow])
	}
	if mods.SchoolDamagePct[engine.Fire] != 0 {
		t.Error("fire should be untouched")
	}
	info := infos[18271]
	if info.Status != StatusUsed || info.Reason != "school" {
		t.Errorf("info = %+v", info)
	}
	if info.Exact {
		t.Error("no trait row: the value is the spell's base points, not exact")
	}
}

// A spell modifier is kept with the talent: whether it is used depends on
// the rotation and is judged there.
func TestGenericSpellMod(t *testing.T) {
	mods, infos := decode(t, map[int]int{9999: 1})
	if len(mods.SpellMods) != 1 {
		t.Fatalf("mods = %+v", mods.SpellMods)
	}
	m := mods.SpellMods[0]
	if m.Op != engine.OpCastTime || m.Pct || m.Value != -500 || m.Mask[0] != 1 {
		t.Errorf("mod = %+v", m)
	}
	if info := infos[9999]; len(info.Mods) != 1 || info.Status != "" {
		t.Errorf("info = %+v", info)
	}
}

// Forever has one spell per talent; the value of each rank is in the trait
// tables. Meditation: the spell row holds 5, the curve 5/10/15.
func TestTraitTablesGiveTheRankValue(t *testing.T) {
	for rank, want := range map[int]float64{1: 5, 2: 10, 3: 15} {
		mods, infos := decodeFrom(t, "../testdata", "PRIEST", map[int]int{14521: rank})
		if mods.CastingRegenPct != want {
			t.Errorf("rank %d: regen while casting = %g, want %g", rank, mods.CastingRegenPct, want)
		}
		if info := infos[14521]; !info.Exact || info.Status != StatusUsed || info.Reason != "regen" {
			t.Errorf("rank %d: info = %+v", rank, info)
		}
	}
}

// Without the trait tables in the cache the spell's own number is all there
// is, whatever the rank.
func TestFallbackWithoutTraitTables(t *testing.T) {
	dir := t.TempDir()
	files, err := filepath.Glob("../testdata/*.csv")
	if err != nil {
		t.Fatal(err)
	}
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
	mods, infos := decodeFrom(t, dir, "PRIEST", map[int]int{14521: 3})
	if mods.CastingRegenPct != 5 {
		t.Errorf("regen while casting = %g", mods.CastingRegenPct)
	}
	if infos[14521].Exact {
		t.Error("a base-points value must not count as exact")
	}
}

// One talent, two effects on different spells: op 22 (periodic) for mask 2
// and op 0 (direct) for mask 1, each with the rank's value.
func TestDirectAndPeriodicEffects(t *testing.T) {
	mods, infos := decode(t, map[int]int{7777: 2})
	if len(mods.SpellMods) != 2 {
		t.Fatalf("mods = %+v", mods.SpellMods)
	}
	seen := map[engine.SpellModOp]engine.SpellMod{}
	for _, m := range mods.SpellMods {
		seen[m.Op] = m
	}
	if m := seen[engine.OpPeriodic]; !m.Pct || m.Value != 4 || m.Mask[0] != 2 {
		t.Errorf("periodic mod = %+v", m)
	}
	if m := seen[engine.OpDamage]; !m.Pct || m.Value != 4 || m.Mask[0] != 1 {
		t.Errorf("direct mod = %+v", m)
	}
	if !infos[7777].Exact {
		t.Error("both values come from the trait tables")
	}
}

// What the character sheet already shows is not applied again, and what the
// decoder does not know is reported, not dropped.
func TestSheetAndUnknownAuras(t *testing.T) {
	mods, infos := decode(t, map[int]int{7778: 1, 7779: 1})
	if len(mods.SpellMods) != 0 || mods.SchoolDamagePct != [engine.NumSchools]float64{} {
		t.Errorf("mods = %+v", mods)
	}
	if info := infos[7778]; info.Status != StatusSheet {
		t.Errorf("hit aura: %+v", info)
	}
	if info := infos[7779]; info.Status != StatusUnmodeled || info.Reason != "unknown" {
		t.Errorf("unknown aura: %+v", info)
	}
}

// The hand-written rules take their number from the trait tables where the
// cache has them and from perRank otherwise.
func TestRules(t *testing.T) {
	mods, infos := decode(t, map[int]int{18182: 2, 427712: 1, 1316697: 1, 17804: 2, 437032: 1, 14524: 2, 15257: 1})
	if mods.LifeTapPct != 20 {
		t.Errorf("life tap = %g", mods.LifeTapPct)
	}
	if infos[18182].Exact {
		t.Error("perRank fallback is not exact")
	}
	if mods.DotCritSchools&(1<<uint(engine.Shadow)) == 0 {
		t.Error("pandemic missing")
	}
	if info := infos[427712]; info.Unverified || info.Note == "" || info.Status != StatusUsed {
		t.Errorf("pandemic info = %+v", info) // confirmed in the game: not an assumption
	}
	// Shadow Weaving decodes into a rank-aware StackProc (unverified assumption).
	if info := infos[15257]; !info.Unverified || info.Note == "" || len(mods.StackProcs) != 1 || mods.StackProcs[0].MaxStacks != 5 {
		t.Errorf("shadow weaving info = %+v, stack procs = %+v", info, mods.StackProcs)
	}

	if len(mods.ChannelHastes) != 1 {
		t.Fatalf("channel hastes = %+v", mods.ChannelHastes)
	}
	ch := mods.ChannelHastes[0]
	if ch.Channel != 1316697 || ch.Pct != 50 || !ch.Affects[172] || !ch.Affects[980] {
		t.Errorf("wrack = %+v", ch)
	}
	if info := infos[1316697]; info.Unverified || !info.Bases[1316697] {
		t.Errorf("wrack info = %+v", info)
	}

	if len(mods.DotStacks) != 1 {
		t.Fatalf("dot stacks = %+v", mods.DotStacks)
	}
	ds := mods.DotStacks[0]
	if ds.PctPerDot != 8 || ds.Max != 3 || !ds.Affects[689] || !ds.Counts[172] {
		t.Errorf("soul siphon = %+v", ds)
	}
	if !infos[17804].Exact {
		t.Error("soul siphon's value comes from the trait tables")
	}

	if info := infos[437032]; info.Status != StatusUnmodeled || info.Reason != "situational" {
		t.Errorf("soul harvest = %+v", info)
	}
	if mods.WandPct != 10 {
		t.Errorf("wand = %g", mods.WandPct)
	}
	if info := infos[14524]; info.Status != StatusUsed || info.Reason != "wand" {
		t.Errorf("wand info = %+v", info)
	}
}

func TestZeroRankIgnored(t *testing.T) {
	mods, infos := decode(t, map[int]int{18271: 0})
	if mods.SchoolDamagePct[engine.Shadow] != 0 {
		t.Error("rank 0 must not count")
	}
	if len(infos) != 0 {
		t.Errorf("infos = %+v", infos)
	}
}

// Every rule names where its number comes from, and its kind is one the
// decoder knows.
func TestOverridesFile(t *testing.T) {
	over, err := loadOverrides()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{"life_tap_pct": true, "dot_crit": true, "healing_pct": true, "school_damage_pct": true,
		"school_crit_bonus_pct": true, "crit_mana_refund": true,
		"wand_pct": true, "dot_stack": true, "channel_dot_haste": true, "kill_proc": true, "stack_proc": true,
		"crit_debuff": true, "instant_proc": true, "generic": true, "ignore": true}
	for id, o := range over {
		if o.Source == "" {
			t.Errorf("%d (%s): no source", id, o.Name)
		}
		if !kinds[o.Kind] {
			t.Errorf("%d (%s): unknown kind %q", id, o.Name, o.Kind)
		}
		if o.Kind == "dot_stack" && (len(o.AffectsBases) == 0 || len(o.CountsBases) == 0) {
			t.Errorf("%d (%s): a dot_stack needs both lists", id, o.Name)
		}
	}
}

func TestBetter(t *testing.T) {
	if !Better(StatusUsed, StatusSheet) || !Better(StatusIdle, StatusUnmodeled) || Better(StatusNone, StatusNone) {
		t.Error("status order is wrong")
	}
}

// Spirit Tap: the chance is the talent's value at the rank, the buff it
// triggers says what it gives and for how long. An assumption (which kills
// count), so flagged.
func TestKillProc(t *testing.T) {
	mods, infos := decodeFrom(t, "../testdata", "PRIEST", map[int]int{15270: 2})
	if len(mods.KillProcs) != 1 {
		t.Fatalf("kill procs = %+v", mods.KillProcs)
	}
	p := mods.KillProcs[0]
	if p.ChancePct != 40 || p.DurationSec != 15 || p.SpiritPct != 100 || p.CastingRegenPct != 50 {
		t.Errorf("spirit tap = %+v", p)
	}
	if mods.SpiritPct != 0 || mods.CastingRegenPct != 0 {
		t.Errorf("the buff is not permanent: %+v", mods)
	}
	if info := infos[15270]; info.Status != StatusUsed || info.Reason != "kills" || !info.Unverified || !info.Exact {
		t.Errorf("info = %+v", info)
	}
}

// The shield's talents are plain spell modifiers: points of effect 0, the
// cooldown and the cost, each on the shield's class mask.
func TestShieldTalents(t *testing.T) {
	mods, infos := decodeFrom(t, "../testdata", "PRIEST", map[int]int{14748: 2, 402000: 1})
	want := map[engine.SpellModOp]float64{engine.OpPoints0: 20, engine.OpCooldown: -4000, engine.OpCost: -15}
	if len(mods.SpellMods) != 3 {
		t.Fatalf("mods = %+v", mods.SpellMods)
	}
	for _, m := range mods.SpellMods {
		if v, ok := want[m.Op]; !ok || v != m.Value || m.Mask[0] != 1 {
			t.Errorf("mod = %+v", m)
		}
		if m.Pct != (m.Op != engine.OpCooldown) {
			t.Errorf("flat/percent wrong: %+v", m)
		}
	}
	if len(infos[14748].Mods) != 1 || len(infos[402000].Mods) != 2 {
		t.Errorf("infos = %+v", infos)
	}
}
