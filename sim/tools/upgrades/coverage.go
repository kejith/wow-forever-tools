package upgrades

import (
	"sort"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/run"
	"github.com/kejith/wow-tools-forever/sim/tools/enginebundle"
)

// The combat engine's coverage report: what the model did with every known spell and talent. The engine
// answers with a status and a reason code (sim/run/run.go, sim/talents); the sentences are here.
var coverageStatus = []struct{ code, label string }{{"used", i18n.Mark("used")},
	{"sheet", i18n.Mark("in your stats")}, {"idle", i18n.Mark("tried")},
	{"unmodeled", i18n.Mark("not modeled")}, {"none", i18n.Mark("no influence")}}

var coverageSpell = map[string]string{
	"rotation":  i18n.Mark("cast in the rotation"),
	"regen":     i18n.Mark("converts health to mana when the rotation runs short"),
	"buff":      i18n.Mark("taken as always active; its health regeneration feeds Life Tap"),
	"weaker":    i18n.Mark("tried instead: {value:.1f} {unit}, less than the rotation above"),
	"nogain":    i18n.Mark("tried next to the rotation above: this fight does not need it"),
	"damage":    i18n.Mark("deals damage, but the model does not cast it"),
	"heal":      i18n.Mark("heals, but the model does not cast it"),
	"absorb":    i18n.Mark("absorbs damage, which the model does not count yet"),
	"mana":      i18n.Mark("returns mana, which the model does not use yet"),
	"notneeded": i18n.Mark("trades health for mana; the mana lasts without it in this fight"),
	"notworth":  i18n.Mark("trades health for mana; here the health would cost more than the mana brings"),
	"area":      i18n.Mark("hits every enemy at once: played in the fights against several enemies of the rotation guide"),
	"other":     i18n.Mark("does not change this number"),
}

var coverageTalent = map[string]string{
	"mods":        i18n.Mark("changes spells of the rotation"),
	"rule":        i18n.Mark("applied to the rotation"),
	"wand":        i18n.Mark("raises the wand damage"),
	"school":      i18n.Mark("raises the damage of a spell school"),
	"cost":        i18n.Mark("lowers mana costs"),
	"regen":       i18n.Mark("mana regeneration continues while casting"),
	"sheet":       i18n.Mark("already part of the numbers the game shows for your character"),
	"idle":        i18n.Mark("changes only spells the best rotation does not cast"),
	"notcast":     i18n.Mark("changes a spell the model does not cast"),
	"nospell":     i18n.Mark("changes spells you have not learned"),
	"othermetric": i18n.Mark("does not change this number"),
	"utility":     i18n.Mark("range, threat or pushback: no influence on damage or healing"),
	"situational": i18n.Mark("depends on the situation (a kill, a proc), not modeled"),
	"kills":       i18n.Mark("active after your killing blows: {value:.0f}% of the fight with the kills it assumes"),
	"nokills":     i18n.Mark("needs your killing blows, and this fight assumes none"),
	"unknown":     i18n.Mark("an effect the model does not know"),
}

var coverageAssumed = i18n.Mark("An assumption of the model that has not been measured in the game.")

// spellLabeler names a spell with its rank line (rotations.labeler; repeated here, rotations imports this
// package).
type spellLabeler func(spellID int, name string, rank int) string

func newSpellLabeler(c *sv.Table) spellLabeler {
	lines := map[int64]string{}
	spells := c.Tab("spellbook").Tab("spells")
	for _, k := range spells.Keys() {
		id, ok := k.(int64)
		if !ok {
			n, parsed := pyfmt.ParseInt(sv.KeyString(k))
			if !parsed {
				continue
			}
			id = n
		}
		v, _ := spells.Get(k).(*sv.Table)
		lines[id] = v.Str("rank")
	}
	return func(spellID int, name string, rank int) string {
		line := lines[int64(spellID)]
		if line == "" && rank > 1 {
			line = i18n.T("Rank {n}", "n", rank)
		}
		if line != "" {
			if name == "" {
				name = "None"
			}
			return name + " (" + line + ")"
		}
		if name == "" {
			return "?"
		}
		return name
	}
}

func statusIndex(status string) int {
	for i, s := range coverageStatus {
		if s.code == status {
			return i
		}
	}
	return len(coverageStatus)
}

func statusLabel(status string) string {
	for _, s := range coverageStatus {
		if s.code == status {
			return i18n.T(s.label)
		}
	}
	return i18n.T("not modeled")
}

type coverageRow struct {
	obj    *pyjson.Obj
	status string
}

// coverageOut is upgrades.coverage_out: the engine's coverage report with labels and sentences.
func coverageOut(cov *run.Coverage, c *sv.Table, unit string, label spellLabeler) any {
	if cov == nil {
		return nil
	}
	talentOf := map[float64]*sv.Table{}
	for _, tree := range c.Tab("talents").Tab("class").Tables() {
		for _, t := range tree.Tab("talents").Tables() {
			if id, ok := sv.Num(t.Get("spellID")); ok {
				talentOf[id] = t
			}
		}
	}
	entry := func(e run.CoverageEntry, texts map[string]string, label string) coverageRow {
		text, ok := texts[e.Reason]
		if !ok || text == "" {
			if text, ok = coverageSpell[e.Reason]; !ok {
				text = coverageTalent["unknown"]
			}
		}
		out := pyjson.New("spellId", e.SpellID, "label", label, "status", e.Status,
			"statusLabel", statusLabel(e.Status), "text", i18n.T(text, "value", e.Value, "unit", unit))
		if e.Unverified {
			out.Set("unverified", true)
			out.Set("note", i18n.T(coverageAssumed)+" "+e.Note)
		}
		return coverageRow{out, e.Status}
	}
	var spells, talents []coverageRow
	for _, e := range cov.Spells {
		spells = append(spells, entry(e, coverageSpell, label(e.SpellID, e.Name, e.Rank)))
	}
	for _, e := range cov.Talents {
		t := talentOf[float64(e.SpellID)]
		name := t.Str("name")
		if name == "" {
			name = e.Name
		}
		if name == "" {
			name = "#" + itoa(int64(e.SpellID))
		}
		rank := itoa(int64(e.Rank))
		if sv.Truthy(t.Get("maxRank")) {
			rank += "/" + pyfmt.Str(t.Get("maxRank"))
		}
		talents = append(talents, entry(e, coverageTalent, name+" ("+rank+")"))
	}
	lists := make([][]any, 2)
	for i, rows := range [][]coverageRow{spells, talents} {
		sort.SliceStable(rows, func(a, b int) bool { return statusIndex(rows[a].status) < statusIndex(rows[b].status) })
		lists[i] = []any{}
		for _, r := range rows {
			lists[i] = append(lists[i], r.obj)
		}
	}
	return pyjson.New("spells", lists[0], "talents", lists[1], "candidates", cov.Candidates,
		"exactTalents", cov.TalentValues == "tables")
}

// gText is f"{v:g}".
func gText(v any) string {
	s, _ := pyfmt.Spec(v, "g")
	return s
}

// checkTexts is upgrades.check_texts: what the model has and what the game says, for a rank found off.
func checkTexts(o enginebundle.Off) (model, text string) {
	switch o.Kind {
	case "cost":
		return i18n.T("the model has {n} mana", "n", gText(o.Model[0])), i18n.T("the game says {n} mana", "n", gText(o.Game))
	case "cast":
		return i18n.T("the model takes {n} s to cast it", "n", gText(o.Model[0])), i18n.T("the game says {n} s", "n", gText(o.Game))
	}
	parts := ""
	for i, v := range o.Model {
		if i > 0 {
			parts += " / "
		}
		parts += gText(v)
	}
	return i18n.T("the model has {numbers}", "numbers", parts), o.Text
}
