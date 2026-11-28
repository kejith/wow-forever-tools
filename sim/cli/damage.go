package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/tools/damage"
	"github.com/kejith/wow-tools-forever/sim/tools/rotations"
)

func init() {
	Register(Command{Name: "damage", Summary: "damage analysis: rotations, every damage spell with its DPS, stat weights",
		Run: runDamage})
}

// damageDPSText is the damage per second of a row with what limits it (damage.dps_text).
func damageDPSText(r rview) string {
	text := rvFormat("{v:.1f}", "v", r.at("dps"))
	if r.at("best").contains("dps") {
		text += "*"
	}
	switch r.at("limit").v {
	case "kept":
		return i18n.T("{dps} kept up", "dps", text)
	case "cooldown":
		return i18n.T("{dps} per cooldown", "dps", text)
	}
	return text
}

func runDamage(args []string, out io.Writer) error {
	f := NewFlags("damage")
	a := addGuideFlags(f, true)
	allRanks := f.Bool("all-ranks", false, "list every known rank, not only the highest")
	if err := f.Parse(args); err != nil {
		return err
	}
	if err := a.check(f); err != nil {
		return err
	}
	res, err := damage.Compute(f.Wow, a.Char, a.FightSeconds, a.TargetDelta, a.KillsPerMinute)
	if err != nil {
		return err
	}
	if a.JSON {
		return JSON(out, res)
	}
	r := rview{res}
	s, unit := r.at("stats"), r.at("unit")
	printGuideHead(out, r)
	var schools []string
	for _, x := range s.at("schools").list() {
		schools = append(schools, i18n.T("{school}: {power} spell power, {crit:.1f}% crit", rvPairs("school", x.at("label"),
			"power", x.at("power"), "crit", x.at("critPct"))...))
	}
	fmt.Fprintln(out, strings.Join(schools, " | "))
	line := []string{i18n.T("{miss:g}% of your spells miss an enemy {delta} levels above you", rvPairs("miss", s.at("missPct"),
		"delta", s.at("targetDelta"))...), i18n.T("mana {mana}", rvPairs("mana", s.at("manaMax"))...)}
	if s.at("wandDps").truthy() {
		line = append(line, i18n.T("wand {dps:.1f} DPS", rvPairs("dps", s.at("wandDps"))...))
	}
	fmt.Fprintln(out, strings.Join(line, " | "))
	line = []string{i18n.T("Mana per 5 sec.: {casting:.1f} while casting, {idle:.1f} outside the five-second rule",
		rvPairs("casting", s.at("castingPer5"), "idle", s.at("idlePer5"))...)}
	if s.at("tapMana").truthy() {
		line = append(line, i18n.T("Life Tap: {mana} mana for {health} health", rvPairs("mana", s.at("tapMana"),
			"health", s.at("tapHealth"))...))
	}
	fmt.Fprintln(out, strings.Join(line, " | "))
	for _, n := range r.at("notes").list() {
		fmt.Fprintln(out, "  "+n.str())
	}

	fmt.Fprintln(out, "\n"+i18n.T("Which rotation when"))
	for _, rot := range r.at("rotations").list() {
		head := rvFormat("{title}: {value:.1f} {unit}", "title", rot.at("title"), "value", rot.at("value"), "unit", unit)
		if rot.at("freeMana").truthy() {
			if rot.at("oomSeconds").none() {
				head += ", " + i18n.T("your mana pays for all of it")
			} else {
				head += ", " + i18n.T("your mana pays for {time} of it", "time", rotations.Clock(rot.at("oomSeconds").num()))
			}
		}
		fmt.Fprintln(out, "  "+head)
		fmt.Fprintln(out, "    "+rot.at("about").str()+" "+rot.at("verdict").str())
		for i, st := range rot.at("steps").list() {
			share := ""
			if st.at("value").truthy() {
				share = rvFormat(" ({value:.1f} {unit})", "value", st.at("value"), "unit", unit)
			}
			fmt.Fprintf(out, "    %d. %s%s: %s\n", i+1, st.at("label").str(), share, st.at("rule").str())
		}
		if skipped := rot.at("skipped").list(); len(skipped) > 0 {
			names := make([]string, len(skipped))
			for i, k := range skipped {
				names[i] = i18n.T("{label} (-{loss:.1f})", rvPairs("label", k.at("label"), "loss", k.at("loss"))...)
			}
			fmt.Fprintln(out, "    "+i18n.T("Left out: {spells}", "spells", strings.Join(names, ", ")))
		}
	}
	rated := r.at("rated")
	fmt.Fprintln(out, "  "+i18n.T("Your gear is rated by a faster estimate of the normal fight: {value:.1f} {unit} over "+
		"{seconds:.0f} s.", rvPairs("value", rated.at("value"), "unit", unit, "seconds", rated.at("seconds"))...))

	if groups := r.at("groupRotations").list(); len(groups) > 0 {
		fmt.Fprintln(out, "\n"+i18n.T("Rotations by group size (1-8, 9+)"))
		gtable := [][]string{{i18n.T("Group size"), unit.str(), i18n.T("Per enemy"), i18n.T("Rotation")}}
		for _, g := range groups {
			var labels []string
			for _, st := range g.at("steps").list() {
				labels = append(labels, st.at("label").str())
			}
			gtable = append(gtable, []string{
				g.at("title").str(),
				rvFormat("{v:.1f}", "v", g.at("value")),
				rvFormat("{v:.1f}", "v", g.at("perTarget")),
				strings.Join(labels, " -> "),
			})
		}
		rvTable(out, gtable, nil)
	}

	if *allRanks {
		fmt.Fprintln(out, "\n"+i18n.T("Every rank you know"))
	} else {
		fmt.Fprintln(out, "\n"+i18n.T("Your spells in their highest rank (--all-ranks lists every rank)"))
	}
	table := [][]string{{i18n.T("Spell"), i18n.T("One cast"), i18n.T("Average"), i18n.T("Mana"), i18n.T("Cast"), unit.str(),
		i18n.T("Per cast second"), i18n.T("Per mana"), i18n.T("Coefficient"), i18n.T("Nothing else"), i18n.T("Cast in")}}
	for _, row := range r.at("rows").list() {
		if !(*allRanks || row.at("top").truthy() || row.at("best").truthy()) {
			continue
		}
		alone := "-"
		if row.at("spammable").truthy() {
			if row.at("spamSeconds").none() {
				alone = i18n.T("lasts")
			} else {
				alone = rotations.Clock(row.at("spamSeconds").num())
			}
		}
		dpm := rvFormat("{v:.2f}", "v", row.at("dpm"))
		if row.at("best").contains("dpm") {
			dpm += "*"
		}
		table = append(table, []string{row.at("label").str(), guideHitText(row), rvFormat("{v:.0f}", "v", row.at("perCast")),
			row.at("mana").str(), guideCastText(row), damageDPSText(row), rvFormat("{v:.1f}", "v", row.at("perCastSec")), dpm,
			rvFormat("{v:.0f}%", "v", row.at("coefPct")), alone, guideUses(row)})
	}
	if s.at("wandDps").truthy() {
		wand := rvFormat("{v:.1f}", "v", s.at("wandDps"))
		table = append(table, []string{i18n.T("Wand"), "", "", "0", "", wand, wand, "", "", i18n.T("lasts"), ""})
	}
	rvTable(out, table, nil)
	fmt.Fprintln(out, i18n.T("* the most damage per mana, and the most per second among the spells you can cast back to back"))

	fmt.Fprintln(out, "\n"+i18n.T("What a point adds ({unit}; in brackets: the points of spell power that do the same)",
		rvPairs("unit", unit)...))
	guideWeights(out, r.at("weights"), "Power")
	return nil
}
