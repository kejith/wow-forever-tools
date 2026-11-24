package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/tools/healing"
	"github.com/kejith/wow-tools-forever/sim/tools/rotations"
)

func init() {
	Register(Command{Name: "healing", Summary: "heal table: every heal and shield rank with what it gives and costs",
		Run: runHealing})
}

// guideHitText is what one cast does without a crit, as a tooltip says it (healing.hit_text, damage.hit_text).
func guideHitText(r rview) string {
	var parts []string
	if r.has("absorb") && r.at("absorb").truthy() {
		parts = append(parts, i18n.T("absorbs {n}", rvPairs("n", r.at("absorb"))...))
	}
	if r.at("max").truthy() {
		if r.at("min").num() != r.at("max").num() {
			parts = append(parts, r.at("min").str()+"-"+r.at("max").str())
		} else {
			parts = append(parts, r.at("min").str())
		}
	}
	if r.at("ticks").truthy() {
		parts = append(parts, i18n.T("{ticks:.0f} x {tick:.0f} over {seconds:.0f} s", rvPairs("ticks", r.at("ticks"),
			"tick", r.at("perTick"), "seconds", r.at("durationSec"))...))
	}
	return strings.Join(parts, " + ")
}

// guideCastText is the cast time of a row: "1.5 s" or "instant".
func guideCastText(r rview) string {
	if r.at("castSec").truthy() {
		return i18n.T("{n:.1f} s", rvPairs("n", r.at("castSec"))...)
	}
	return i18n.T("instant")
}

// guideUses lists the situations a rank is cast in.
func guideUses(r rview) string {
	var titles []string
	for _, u := range r.at("uses").list() {
		titles = append(titles, u.at("title").str())
	}
	if len(titles) == 0 {
		return "-"
	}
	return strings.Join(titles, ", ")
}

// guideWeights prints the stat weights; suffix is the key of the equivalent in the main stat ("Bonus" for
// healing, "Power" for damage).
func guideWeights(out io.Writer, w rview, suffix string) {
	for _, row := range w.at("rows").list() {
		cell := func(name string) string {
			eq := row.at(name + suffix)
			text := rvFormat("{v:.3f}", "v", row.at(name))
			if !eq.none() {
				text += rvFormat(" ({eq:.2f})", "eq", eq)
			}
			return text
		}
		fmt.Fprintln(out, "  "+i18n.T("{label}: {short} in a fight of {seconds:.0f} s, {long} in one of {long_seconds:.0f} s",
			rvPairs("label", row.at("label"), "short", cell("short"), "long", cell("long"), "seconds", w.at("seconds"),
				"long_seconds", w.at("longSeconds"))...))
	}
}

func runHealing(args []string, out io.Writer) error {
	f := NewFlags("healing")
	a := addGuideFlags(f, false)
	if err := f.Parse(args); err != nil {
		return err
	}
	if err := a.check(f); err != nil {
		return err
	}
	res, err := healing.Compute(f.Wow, a.Char, a.FightSeconds, a.TargetDelta)
	if err != nil {
		return err
	}
	if a.JSON {
		return JSON(out, res)
	}
	r := rview{res}
	s, unit := r.at("stats"), r.at("unit")
	printGuideHead(out, r)
	fmt.Fprintln(out, i18n.T("Healing bonus {healing}, crit {crit:.1f}%, mana {mana}, spirit {spirit}", rvPairs(
		"healing", s.at("healing"), "crit", s.at("critPct"), "mana", s.at("manaMax"), "spirit", s.at("spirit"))...))
	fmt.Fprintln(out, i18n.T("Mana per 5 sec.: {casting:.1f} while casting, {idle:.1f} outside the five-second rule",
		rvPairs("casting", s.at("castingPer5"), "idle", s.at("idlePer5"))...))
	for _, n := range r.at("notes").list() {
		fmt.Fprintln(out, "  "+n.str())
	}

	table := [][]string{{i18n.T("Spell"), i18n.T("One cast"), i18n.T("Average"), i18n.T("Mana"), i18n.T("Cast"),
		"HPS", i18n.T("Per mana"), i18n.T("From bonus"), i18n.T("Coefficient"), i18n.T("Use it for")}}
	for _, row := range r.at("rows").list() {
		best := row.at("best")
		hps := rvFormat("{v:.1f}", "v", row.at("hps"))
		if best.contains("hps") {
			hps += "*"
		}
		if row.at("hpsOnTarget").truthy() {
			hps = i18n.T("{n:.1f} on the target", rvPairs("n", row.at("hpsOnTarget"))...)
		}
		hpm := rvFormat("{v:.2f}", "v", row.at("hpm"))
		if best.contains("hpm") {
			hpm += "*"
		}
		coef := rvFormat("{v:.0f}%", "v", row.at("coefPct"))
		if row.at("bonusLossPct").truthy() {
			coef += " " + i18n.T("(outgrown: {pct:.0f}% less healing bonus)", rvPairs("pct", row.at("bonusLossPct"))...)
		}
		table = append(table, []string{row.at("label").str(), guideHitText(row), rvFormat("{v:.1f}", "v", row.at("perCast")),
			row.at("mana").str(), guideCastText(row), hps, hpm, rvFormat("{v:.0f}%", "v", row.at("bonusPct")), coef,
			guideUses(row)})
	}
	fmt.Fprintln(out)
	rvTable(out, table, nil)
	fmt.Fprintln(out, i18n.T("* the most healing per mana, and the most per second among the heals you can cast back to back"))

	fmt.Fprintln(out, "\n"+i18n.T("Which rank when"))
	for _, rec := range r.at("recommended").list() {
		lasts := i18n.T("mana lasts")
		if !rec.at("oomSeconds").none() {
			lasts = i18n.T("mana lasts {time}", "time", rotations.Clock(rec.at("oomSeconds").num()))
		}
		var spells []string
		for _, st := range rec.at("steps").list() {
			spells = append(spells, st.at("label").str()+": "+st.at("rule").str())
		}
		fmt.Fprintln(out, rvFormat("  {title} ({value:.1f} {unit}, {lasts}): {spells}", "title", rec.at("title"),
			"value", rec.at("value"), "unit", unit, "lasts", lasts, "spells", strings.Join(spells, "; ")))
	}
	sus := r.at("sustained")
	var labels []string
	for _, st := range sus.at("steps").list() {
		labels = append(labels, st.at("label").str())
	}
	fmt.Fprintln(out, "  "+i18n.T("Sustained over {seconds:.0f} s ({value:.1f} {unit}, what your gear is rated by): {spells}",
		rvPairs("seconds", sus.at("seconds"), "value", sus.at("value"), "unit", unit, "spells", strings.Join(labels, ", "))...))

	fmt.Fprintln(out, "\n"+i18n.T("What a point adds ({unit}; in brackets: the points of healing bonus that do the same)",
		rvPairs("unit", unit)...))
	guideWeights(out, r.at("weights"), "Bonus")
	return nil
}
