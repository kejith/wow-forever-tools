package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/tools/consumables"
	"github.com/kejith/wow-tools-forever/sim/tools/rotations"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

// guideFlags are the options of the fight the engine plays, as rotations.py, healing.py and damage.py had
// them.
type guideFlags struct {
	Char           string
	FightSeconds   float64
	TargetDelta    int
	KillsPerMinute float64
	JSON           bool
}

func addGuideFlags(f *Flags, kills bool) *guideFlags {
	a := &guideFlags{}
	f.StringVar(&a.Char, "char", "", "start of the character's name")
	f.Float64Var(&a.FightSeconds, "fight-seconds", 60, "length of the normal fight")
	f.IntVar(&a.TargetDelta, "target-delta", -1,
		"enemy levels above you in the normal fight (default: 2 below level 60, else 3)")
	if kills {
		f.Float64Var(&a.KillsPerMinute, "kills-per-minute", -1,
			"your killing blows per minute, for buffs like Spirit Tap (default: one per fight)")
	}
	f.BoolVar(&a.JSON, "json", false, "print the result as JSON (for the app)")
	return a
}

// check is argparse's choices=[-1, 0, 1, 2, 3] of --target-delta.
func (a *guideFlags) check(f *Flags) error {
	if a.TargetDelta < -1 || a.TargetDelta > 3 {
		fmt.Fprintf(os.Stderr, "%s: argument --target-delta: invalid choice: %d (choose from -1, 0, 1, 2, 3)\n",
			f.Name(), a.TargetDelta)
		return ErrUsage
	}
	return nil
}

// printGuideHead prints the first lines the engine's reports share: who, and where the spellbook came from.
func printGuideHead(out io.Writer, r rview) {
	fmt.Fprintln(out, i18n.T("{name}, level {level} {cls} | combat engine {version}", rvPairs("name", r.at("name"),
		"level", r.at("level"), "cls", r.at("class"), "version", r.at("engine").at("version"))...))
	if r.at("engine").at("spellbookSource").v == "assumed" {
		fmt.Fprintln(out, i18n.T("No spellbook in the export: every trainer rank up to your level is assumed."))
	}
}

func init() {
	Register(Command{Name: "rotations", Summary: "rotation guide: what to cast in which fight, from the combat engine",
		Run: runRotations})
}

func runRotations(args []string, out io.Writer) error {
	f := NewFlags("rotations")
	a := addGuideFlags(f, true)
	withConsumables := f.Bool("consumables", false,
		"play every fight with the best set of consumables active (consumables.py)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if err := a.check(f); err != nil {
		return err
	}
	var buffs upgrades.FightBuffsFunc
	if *withConsumables {
		wow, char := f.Wow, a.Char
		buffs = func(metric string, seconds float64, targetDelta int) (*upgrades.Consumed, error) {
			return consumables.FightBuffs(wow, char, metric, seconds, targetDelta)
		}
	}
	res, err := rotations.Compute(f.Wow, a.Char, a.FightSeconds, a.TargetDelta, a.KillsPerMinute, buffs)
	if err != nil {
		return err
	}
	if a.JSON {
		return JSON(out, res)
	}
	r := rview{res}
	printGuideHead(out, r)
	for _, g := range r.at("guides").list() {
		fmt.Fprintf(out, "\n=== %s ===\n%s\n", g.at("title").str(), g.at("about").str())
		if g.at("consumables").truthy() {
			fmt.Fprintln(out, g.at("consumables").at("text").str())
		}
		for _, s := range g.at("situations").list() {
			unit := s.at("unit")
			fmt.Fprintln(out, rvFormat("\n{title}: {value:.1f} {unit}  ({about})", "title", s.at("title"),
				"value", s.at("value"), "unit", unit, "about", s.at("about")))
			fmt.Fprintln(out, "  "+s.at("verdict").str())
			for i, st := range s.at("steps").list() {
				numbers := i18n.T("{n:.1f}/min, {share:.0f}% of the time", rvPairs("n", st.at("perMin"),
					"share", st.at("sharePct"))...)
				if st.at("value").truthy() {
					numbers += rvFormat(", {value:.1f} {unit}", "value", st.at("value"), "unit", unit)
				}
				fmt.Fprintf(out, "  %d. %s: %s  [%s]\n", i+1, st.at("label").str(), st.at("rule").str(), numbers)
			}
			for _, k := range s.at("skipped").list() {
				fmt.Fprintln(out, "  "+i18n.T("Left out: {label} ({loss:.1f} {unit} less; {why})", rvPairs(
					"label", k.at("label"), "loss", k.at("loss"), "unit", unit, "why", k.at("why"))...))
			}
			if timeline := s.at("timeline").list(); len(timeline) > 0 {
				parts := make([]string, len(timeline))
				for i, e := range timeline {
					part := rotations.Clock(e.at("t").num()) + " " + e.at("label").str()
					if e.at("n").num() > 1 {
						part += " x" + e.at("n").str()
					}
					parts[i] = part + rvFormat(" [{mana}/{hp}]", "mana", e.at("manaPct"), "hp", e.at("hpPct"))
				}
				line := "  " + i18n.T("Start (mana/health in % before each action):") + " " + strings.Join(parts, " > ")
				if s.at("more").truthy() {
					line += " " + i18n.T("... and {n} more actions", rvPairs("n", s.at("more"))...)
				}
				fmt.Fprintln(out, line)
			}
		}
		if groups := g.at("groupRotations").list(); len(groups) > 0 {
			unit := g.at("unit")
			fmt.Fprintln(out, "\n"+i18n.T("Rotations by group size (1-8, 9+)"))
			gtable := [][]string{{i18n.T("Group size"), unit.str(), i18n.T("Per enemy"), i18n.T("Rotation")}}
			for _, gr := range groups {
				var labels []string
				for _, st := range gr.at("steps").list() {
					labels = append(labels, st.at("label").str())
				}
				gtable = append(gtable, []string{
					gr.at("title").str(),
					rvFormat("{v:.1f}", "v", gr.at("value")),
					rvFormat("{v:.1f}", "v", gr.at("perTarget")),
					strings.Join(labels, " -> "),
				})
			}
			rvTable(out, gtable, nil)
		}
		for _, n := range g.at("notes").list() {
			fmt.Fprintln(out, "\n"+n.str())
		}
	}
	return nil
}
