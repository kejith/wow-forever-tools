package upgrades

import (
	"strings"

	"github.com/kejith/wow-tools-forever/sim/engine"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/tools/enginebundle"
)

// FightObj is an engine fight as the engine's JSON has it.
func FightObj(f engine.Fight) *pyjson.Obj {
	o := pyjson.New("seconds", f.Seconds, "targetDelta", f.TargetDelta, "killsPerMinute", f.KillsPerMinute)
	if f.Targets != 0 {
		o.Set("targets", f.Targets)
	}
	return o
}

func floatOrNil(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func zeroNil(x float64) any {
	if x == 0 {
		return nil
	}
	return x
}

// engineSection is the part of upgrades.compute that describes the combat model: the header text, the
// breakdown rows in place of the scale, and the "engine" object.
func engineSection(eng *enginebundle.Engine, c *sv.Table, classFile string, o Options, tracker *SetTracker) (string, []any, *pyjson.Obj) {
	b := eng.Baseline
	if b == nil {
		b = &engine.Metrics{}
	}
	label := newSpellLabeler(c)
	rows := []any{}
	for _, row := range b.Breakdown {
		note := i18n.T("{share:.1f}% of the fight time", "share", row.SharePct)
		if row.Kind == "lifetap" || row.Kind == "mana" {
			note = i18n.T("{n:.1f} casts per minute", "n", row.PerMin) + ", " + note
			if row.ManaGain != 0 {
				note += ", " + i18n.T("{mana:.1f} mana per second for {health:.1f} health per second",
					"mana", row.ManaGain, "health", row.HealthCost)
				if b.Metric != "dps" {
					note += " " + i18n.T("(the health is taken off the healing)")
				}
			}
		}
		if row.TakenPct != 0 {
			note += ", " + i18n.T("the other spells deal {pct:g}% more under it", "pct", row.TakenPct)
		}
		if row.Leech != 0 {
			note += ", " + i18n.T("returns {n:.1f} health per second", "n", row.Leech)
		}
		if row.Absorb {
			note += ", " + i18n.T("a shield: counted as if all of it absorbs damage")
		}
		name := row.Label
		if name == "" {
			name = row.Kind
		}
		text := label(row.SpellID, name, row.Rank)
		if row.Kind == "wand" {
			text = i18n.T("Wand")
		}
		rows = append(rows, pyjson.New("label", text, "weight", pyfmt.Round(row.Value, 1), "note", note))
	}
	fight := eng.Fight
	value, metric := b.Value, b.Metric
	var text string
	switch metric {
	case "hps":
		text = i18n.T("Combat model: {hps:.1f} HPS over a {seconds:g}s fight", "hps", value, "seconds", fight.Seconds)
	case "ttoom":
		text = i18n.T("Combat model: the mana lasts {value:.0f}s at full healing", "value", value)
	default:
		text = i18n.T("Combat model: {dps:.1f} DPS against +{target} levels, {seconds:g}s fight",
			"dps", value, "target", fight.TargetDelta, "seconds", fight.Seconds)
	}
	if metric != "ttoom" && b.TTOOMSec != nil {
		text += i18n.T(", mana for {ttoom:.0f}s", "ttoom", *b.TTOOMSec)
	}
	if b.MC != nil {
		text += i18n.T(" · Monte-Carlo over {n} fights: spread ±{sd:.1f}, worst tenth {p10:.1f}",
			"n", b.MC.Iterations, "sd", b.MC.StdDev, "p10", b.MC.P10)
	}
	if eng.SpellbookSource == "assumed" {
		text += i18n.T(", spellbook assumed from level and talents")
	}
	metrics := enginebundle.Metrics[classFile]
	if len(metrics) == 0 {
		metrics = []string{"dps"}
	}
	var mc any
	if b.MC != nil {
		mc = pyjson.New("iterations", b.MC.Iterations, "mean", b.MC.Mean, "stdDev", b.MC.StdDev,
			"p10", b.MC.P10, "p90", b.MC.P90)
	}
	killBuff := false
	if eng.Coverage != nil {
		for _, e := range eng.Coverage.Talents {
			killBuff = killBuff || e.Reason == "kills" || e.Reason == "nokills"
		}
	}
	unit := map[string]string{"dps": "DPS", "hps": "HPS"}[metric]
	if unit == "" {
		unit = "s"
	}
	out := pyjson.New("version", eng.Version, "metric", metric, "value", value,
		"ttoomSeconds", floatOrNil(b.TTOOMSec), "spellbookSource", eng.SpellbookSource,
		"manaSpentPs", b.ManaSpentPS, "manaBudgetPs", b.ManaBudgetPS, "fight", FightObj(fight),
		"metrics", metrics, "spec", enginebundle.SpecOf(c), "mc", mc, "selfHealPs", zeroNil(b.SelfHealPS),
		// a talent that needs killing blows: the window then offers "kills per minute"
		"killBuff", killBuff, "coverage", coverageOut(eng.Coverage, c, unit, label, tracker))
	if d := eng.Downrank; d != nil {
		spell := label(d.SpellID, d.Name, d.Rank)
		instead := label(d.InsteadID, d.InsteadName, d.InsteadRank)
		out.Set("downrank", pyjson.New("spellId", d.SpellID, "name", d.Name, "rank", d.Rank, "insteadId", d.InsteadID,
			"insteadName", d.InsteadName, "insteadRank", d.InsteadRank, "gainPct", d.GainPct,
			"spell", spell, "instead", instead,
			"text", i18n.T("Downranking pays: heal with {spell} instead of {instead} ({pct:+.0f}% healing over the fight).",
				"spell", spell, "instead", instead, "pct", d.GainPct)))
	}
	if len(eng.HealOptions) > 0 {
		var list []any
		for _, h := range eng.HealOptions {
			row := pyjson.New("spellId", h.SpellID, "name", h.Name, "rank", h.Rank, "healPerCast", h.HealPerCast,
				"manaPerCast", h.ManaPerCast, "castSec", h.CastSec, "healPerMana", h.HealPerMana,
				"hpsThroughput", h.HPSThroughput, "hpsSustained", h.HPSSustained)
			if h.BonusLossPct != 0 {
				row.Set("bonusLossPct", h.BonusLossPct)
			}
			if h.Chosen {
				row.Set("chosen", true)
			}
			row.Set("spell", label(h.SpellID, h.Name, h.Rank))
			list = append(list, row)
		}
		out.Set("healOptions", list)
	}
	if check := enginebundle.TooltipCheck(eng.Spells, c.Tab("spellbook").Tab("spells"), eng.Coverage); check != nil {
		// The game's own spell texts are the reference: they hold this character's numbers.
		var said string
		if check.Costs {
			said = i18n.T("Checked against the spell texts, mana costs and cast times of the game: "+
				"{matched} of {checked} spell ranks agree.", "matched", check.Matched, "checked", check.Checked)
		} else {
			said = i18n.T("Checked against the spell texts of the game: {matched} of {checked} spell ranks agree.",
				"matched", check.Matched, "checked", check.Checked)
		}
		off := []any{}
		for _, x := range check.Off {
			model, text := checkTexts(x)
			off = append(off, pyjson.New("spellId", x.SpellID, "label", label(x.SpellID, x.Name, 0),
				"model", model, "text", text))
		}
		out.Set("tooltipCheck", pyjson.New("checked", check.Checked, "matched", check.Matched, "text", said, "off", off))
	}
	if o.Consumables != nil {
		names := make([]any, len(o.Consumables.Names))
		for i, n := range o.Consumables.Names {
			names[i] = n
		}
		out.Set("consumables", pyjson.New("names", names, "gain", o.Consumables.Gain, "unit", o.Consumables.Unit,
			"text", o.Consumables.Text))
	}
	var buffs []string
	for _, bf := range c.Tab("character").Tab("buffs").Tables() {
		if n := bf.Str("name"); n != "" {
			buffs = append(buffs, n)
		}
	}
	if len(buffs) > 0 {
		// The sheet's numbers are the engine's start, and they are the numbers under these buffs.
		out.Set("buffs", pyjson.New("names", buffs, "text", i18n.T("The snapshot was taken with these buffs, and its "+
			"numbers include them: {names}.", "names", strings.Join(buffs, ", "))))
	}
	return text, rows, out
}
