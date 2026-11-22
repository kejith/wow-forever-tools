package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/tools/combatlog"
	"github.com/kejith/wow-forever-tools/sim/tools/rotations"
)

// combatlogMaxBuffs is how many buffs the report names (combatlog.MAX_BUFFS).
const combatlogMaxBuffs = 20

func init() {
	Register(Command{Name: "combatlog", Summary: "combat log: what the character really did, next to the combat model",
		Run: runCombatlog})
}

// combatlogCell is combatlog.cell: "-" for None, else the number with digits decimals and a unit.
func combatlogCell(x rview, digits int, unit string) string {
	if x.none() {
		return "-"
	}
	return rvFormat("{x:."+fmt.Sprint(digits)+"f}", "x", x) + unit
}

// combatlogFightLine is combatlog.fight_line: one fight in a line.
func combatlogFightLine(f rview) string {
	var targets []string
	for _, t := range f.at("targets").list() {
		targets = append(targets, t.str())
	}
	shown := targets
	if len(shown) > 3 {
		shown = shown[:3]
	}
	names := strings.Join(shown, ", ")
	if len(targets) > 3 {
		names += ", ..."
	}
	line := i18n.T("{n:3d}  {start}  {time:>8}  enemies {enemies}  {damage:>7,} damage  {dps:7.1f} DPS", rvPairs(
		"n", f.at("n"), "start", f.at("start"), "time", rotations.Clock(f.at("seconds").num()), "enemies", f.at("enemies"),
		"damage", f.at("damage"), "dps", f.at("dps"))...)
	if !f.at("manaStartPct").none() {
		line += i18n.T("  mana {a}% > {b}%", rvPairs("a", f.at("manaStartPct"), "b", f.at("manaEndPct"))...)
	}
	if !f.at("levelDelta").none() {
		line += i18n.T("  level {delta:+d}", rvPairs("delta", f.at("levelDelta"))...)
	}
	line += "  " + names
	if f.at("encounter").truthy() {
		line += "  [" + f.at("encounter").str() + "]"
	}
	return line
}

func runCombatlog(args []string, out io.Writer) error {
	f := NewFlags("combatlog")
	char := f.String("char", "", "start of the character's name (default: the one with the most lines)")
	file := f.String("file", "", "the log to read (default: the newest in the game's Logs folder)")
	gap := f.Float64("gap", combatlog.Gap, "seconds without an event that end a fight")
	fight := f.Int("fight", 0, "show this fight cast by cast")
	noModel := f.Bool("no-model", false, "do not ask the combat model for its numbers")
	asJSON := f.Bool("json", false, "print the result as JSON")
	if err := f.Parse(args); err != nil {
		return err
	}
	res, err := combatlog.Compute(f.Wow, *char, *file, *gap, !*noModel)
	if err != nil {
		return err
	}
	if *asJSON {
		data, err := pyjson.Encode(res)
		if err != nil {
			return err
		}
		_, err = out.Write(append(combatlogIndent(data, 1), '\n'))
		return err
	}
	r := rview{res}
	t := r.at("total")
	fmt.Fprintln(out, i18n.T("{file}: {name}, {fights} fights, {time} fighting, {damage:,.0f} damage, {dps:.1f} DPS while fighting",
		rvPairs("file", r.at("file"), "name", r.at("name"), "fights", t.at("fights"),
			"time", rotations.Clock(t.at("seconds").num()), "damage", t.at("damage"), "dps", t.at("dps"))...))
	if !r.at("advanced").truthy() {
		fmt.Fprintln(out, i18n.T("The log was written without advanced logging: no mana, no health and no enemy levels. "+
			"Turn it on in the game (System > Network > Advanced Combat Logging)."))
	}
	if r.at("modelError").truthy() {
		fmt.Fprintln(out, i18n.T("No numbers of the combat model: {err}", rvPairs("err", r.at("modelError"))...))
	}
	if sp := r.at("spellPower"); sp.truthy() {
		line := i18n.T("Spell power in the log: {low} to {high}.", rvPairs("low", sp.at("low"), "high", sp.at("high"))...)
		if r.at("modelSpellPower").truthy() {
			line += " " + i18n.T("The model's numbers are at {power}, the log's median.",
				rvPairs("power", r.at("modelSpellPower"))...)
		}
		fmt.Fprintln(out, line)
	}

	if *fight != 0 {
		var one *rview
		for _, x := range r.at("fights").list() {
			if x.at("n").num() == float64(*fight) {
				x := x
				one = &x
				break
			}
		}
		if one == nil {
			return errors.New(i18n.T("The log has no fight {n}.", "n", *fight))
		}
		fmt.Fprintln(out, combatlogFightLine(*one))
		for _, c := range one.at("casts").list() {
			state := "[" + combatlogCell(c.at("manaPct"), 0, "") + "/" + combatlogCell(c.at("healthPct"), 0, "") + "]"
			line := rvFormat("  {t:6.1f} s  {state:10} {label}", "t", c.at("t"), "state", state, "label", c.at("label"))
			if c.at("target").truthy() {
				line += " > " + c.at("target").str()
			}
			fmt.Fprintln(out, line)
		}
		if one.at("moreCasts").truthy() {
			fmt.Fprintln(out, "  "+i18n.T("... and {n} more casts", rvPairs("n", one.at("moreCasts"))...))
		}
		return nil
	}

	table := [][]string{{i18n.T("Spell"), i18n.T("Casts"), i18n.T("/min"), i18n.T("Hits"), i18n.T("Crit"),
		i18n.T("Missed"), i18n.T("Avg hit"), i18n.T("Model"), i18n.T("Ticks"), i18n.T("Crit"), i18n.T("Avg tick"),
		i18n.T("Model"), i18n.T("Tick every"), i18n.T("Up"), i18n.T("Enemy"), i18n.T("Crit x"), i18n.T("Resisted"),
		i18n.T("Damage"), i18n.T("Share")}}
	spells := r.at("spells").list()
	for _, s := range spells {
		m := s.at("model")
		name := s.at("label").str()
		if s.at("pet").truthy() {
			name += " (" + s.at("pet").str() + ")"
		}
		ticks := "-"
		if s.at("tickSec").truthy() {
			ticks = rvFormat("{a:.1f}/{b:.1f}/{c:.1f} s", "a", s.at("tickSecFast"), "b", s.at("tickSec"),
				"c", s.at("tickSecSlow"))
		}
		taken := "-"
		if !s.at("takenPct").none() {
			taken = rvFormat("{v:+d}%", "v", pyfmt.RoundInt(s.at("takenPct").num()))
		}
		table = append(table, []string{name, s.at("casts").str(), combatlogCell(s.at("castsPerMin"), 1, ""),
			s.at("hits").str(), combatlogCell(s.at("critPct"), 0, "%"), combatlogCell(s.at("missPct"), 0, "%"),
			combatlogCell(s.at("avgHit"), 1, ""), combatlogCell(m.at("hit"), 1, ""), s.at("ticks").str(),
			combatlogCell(s.at("tickCritPct"), 0, "%"), combatlogCell(s.at("avgTick"), 1, ""),
			combatlogCell(m.at("tick"), 1, ""), ticks, combatlogCell(s.at("uptimePct"), 0, "%"), taken,
			combatlogCell(s.at("critMult"), 2, ""), combatlogCell(s.at("resistedPct"), 1, "%"),
			rvFormat("{v:,}", "v", s.at("damage")), combatlogCell(s.at("sharePct"), 1, "%")})
	}
	rvTable(out, table, func(i int) bool { return i != 0 })
	fmt.Fprintln(out, i18n.T("Avg hit and avg tick are the amounts before crit and mitigation, as a tooltip shows them; "+
		"\"Enemy\" is what the enemies added to them (debuffs) and \"Crit x\" what a crit multiplied. "+
		"\"Tick every\" is the fastest tenth, the median and the slowest tenth of the time between two ticks."))
	for _, s := range spells {
		var extra []string
		if s.at("mana").truthy() {
			extra = append(extra, i18n.T("{n:,} mana, {count}x ({each:.0f} each)", rvPairs("n", s.at("mana"),
				"count", s.at("energizes"), "each", s.at("mana").num()/max(s.at("energizes").num(), 1))...))
		}
		if s.at("healing").truthy() || s.at("overheal").truthy() {
			extra = append(extra, i18n.T("{n:,} healing, {over:,} overhealing", rvPairs("n", s.at("healing"),
				"over", s.at("overheal"))...))
		}
		if s.at("selfDamage").truthy() {
			extra = append(extra, i18n.T("{n:,} damage to yourself", rvPairs("n", s.at("selfDamage"))...))
		}
		if len(extra) > 0 {
			fmt.Fprintln(out, "  "+s.at("label").str()+": "+strings.Join(extra, ", "))
		}
	}
	for _, s := range spells {
		c := s.at("channel")
		if !c.truthy() {
			continue
		}
		var parts []string
		if c.at("casts").truthy() {
			parts = append(parts, i18n.T("{avg:.1f} of {ticks} ticks per channel; channels {casts}, ran out {full}",
				rvPairs("avg", c.at("avgTicks"), "ticks", c.at("ticks"), "casts", c.at("casts"), "full", c.at("full"))...))
		}
		if c.at("died").truthy() {
			parts = append(parts, i18n.T("target died {n}", rvPairs("n", c.at("died"))...))
		}
		if c.at("cut").truthy() {
			parts = append(parts, i18n.T("next cast before the end {n} ({lost} ticks short, {clipped}x only the last one)",
				rvPairs("n", c.at("cut"), "lost", c.at("lost").at("cut"), "clipped", c.at("clipped"))...))
		}
		if c.at("short").truthy() {
			parts = append(parts, i18n.T("stopped early with no cast after {n} ({lost} ticks short)",
				rvPairs("n", c.at("short"), "lost", c.at("lost").at("short"))...))
		}
		if c.at("missed").truthy() {
			parts = append(parts, i18n.T("missed {n}", rvPairs("n", c.at("missed"))...))
		}
		fmt.Fprintln(out, "  "+s.at("label").str()+": "+strings.Join(parts, ", "))
	}
	if buffs := r.at("buffs").list(); len(buffs) > 0 {
		if len(buffs) > combatlogMaxBuffs {
			buffs = buffs[:combatlogMaxBuffs]
		}
		names := make([]string, len(buffs))
		for i, b := range buffs {
			names[i] = rvFormat("{name} {pct:.0f}%", "name", b.at("name"), "pct", b.at("uptimePct"))
		}
		fmt.Fprintln(out, i18n.T("Buffs on you, of the time fighting: {list}", "list", strings.Join(names, ", ")))
	}
	if hits := r.at("hit").list(); len(hits) > 0 {
		fmt.Fprintln(out, i18n.T("Casts on enemies that missed, by the enemy's level above yours:"))
		for _, h := range hits {
			line := i18n.T("  {delta:+d}: {misses} of {casts} ({pct:.1f}%)", rvPairs("delta", h.at("levelDelta"),
				"misses", h.at("misses"), "casts", h.at("casts"), "pct", h.at("missPct"))...)
			if !h.at("modelMissPct").none() {
				line += i18n.T(", the model expects {pct:.0f}%", rvPairs("pct", h.at("modelMissPct"))...)
			}
			fmt.Fprintln(out, line)
		}
	}
	fmt.Fprintln(out, i18n.T("Fights (--fight N shows one cast by cast):"))
	for _, x := range r.at("fights").list() {
		fmt.Fprintln(out, combatlogFightLine(x))
	}
	return nil
}

// combatlogIndent re-indents the compact JSON pyjson writes the way json.dumps(…, indent=n) does: every item
// on its own line, "," at the end of a line, ": " after a key, empty lists and objects as [] and {}.
func combatlogIndent(data []byte, n int) []byte {
	var b bytes.Buffer
	depth := 0
	newline := func() {
		b.WriteByte('\n')
		b.WriteString(strings.Repeat(" ", depth*n))
	}
	inString := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			b.WriteByte(c)
			switch c {
			case '\\':
				i++
				b.WriteByte(data[i])
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
			b.WriteByte(c)
		case '{', '[':
			if i+1 < len(data) && (data[i+1] == '}' || data[i+1] == ']') {
				b.WriteByte(c)
				b.WriteByte(data[i+1])
				i++
				continue
			}
			b.WriteByte(c)
			depth++
			newline()
		case '}', ']':
			depth--
			newline()
			b.WriteByte(c)
		case ',':
			b.WriteByte(',')
			newline()
			if i+1 < len(data) && data[i+1] == ' ' {
				i++
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.Bytes()
}
