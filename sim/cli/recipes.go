package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/tools/recipes"
)

// recipes is scripts/recipes.py: profit per craft of the learned recipes and the missing recipes that pay.
func init() {
	Register(Command{Name: "recipes", Summary: "profession recipes: profit of the learned ones and which missing ones pay",
		Run: func(args []string, out io.Writer) error {
			f := NewFlags("recipes")
			char := f.String("char", "", "part of the character name")
			profession := f.String("profession", "",
				"part of the profession name as the client shows it (Alchemy, Alchimie) or its skill line ID")
			top := f.Int("top", 15, "recipes per list (0 = all)")
			asJSON := f.Bool("json", false, "print the result as JSON")
			if err := f.Parse(args); err != nil {
				return err
			}
			r, err := recipes.Compute(f.Wow, *char, *profession, max(*top, 0), nil)
			if err != nil {
				return err
			}
			if *top < 0 { // learned[:top] with a negative top drops the last ones
				for _, p := range pyList(r.Get("professions")) {
					for _, k := range []string{"learned", "missing"} {
						rows := pyList(pyGet(p, k))
						p.(*pyjson.Obj).Set(k, rows[:pySliceEnd(len(rows), *top)])
					}
				}
			}
			if *asJSON {
				return JSON(out, r)
			}
			printRecipes(out, r)
			return nil
		}})
}

func printRecipes(out io.Writer, r *pyjson.Obj) {
	money := func(c any) string { // "–" for None, else writs.fmt
		if n, ok := sv.Num(c); ok {
			return game.Money(n)
		}
		return "–"
	}
	fmt.Fprintln(out, i18n.T("{char}  |  AH scan {date}  |  build {build}  |  vendor prices {vendors}",
		"char", r.Get("character"), "date", r.Get("scanDate"), "build", r.Get("build"), "vendors", r.Get("vendorPrices")))
	kinds := map[string]string{"ah": i18n.T("AH"), "vendor": i18n.T("vendor"), "item": i18n.T("recipe item"),
		"trainer": i18n.T("trainer")}
	kind := func(o any) string { s, _ := pyGet(o, "kind").(string); return kinds[s] }
	learnable := func(v any) string {
		switch v {
		case true:
			return i18n.T("yes")
		case false:
			return i18n.T("no")
		}
		return "?"
	}
	for _, p := range pyList(r.Get("professions")) {
		scanned := pyGet(p, "scanned")
		state := i18n.T("learned recipes unknown (open the profession window)")
		if pyTruthy(scanned) {
			secs, _ := sv.Num(scanned)
			when := time.Unix(0, int64(secs*1e9)).Local().Format("2006-01-02 15:04")
			state = i18n.T("profession window read {when}", "when", when)
		}
		fmt.Fprintf(out, "\n== %s %s/%s (%s) ==\n", pyfmt.Str(pyGet(p, "name")), pyfmt.Str(pyGet(p, "rank")),
			pyfmt.Str(pyGet(p, "max")), state)
		if pyTruthy(scanned) {
			fmt.Fprintln(out, "  "+i18n.T("Learned:"))
			for _, x := range pyList(pyGet(p, "learned")) {
				var notes []string
				for _, n := range pyList(pyGet(x, "notes")) {
					notes = append(notes, pyfmt.Str(n))
				}
				fmt.Fprintln(out, "   "+pySpec(pyCut(pyfmt.Str(pyGet(x, "name")), 40), "<40")+" "+
					i18n.T("profit {profit}  cost {cost}  AH {listed}x", "profit", pySpec(money(pyGet(x, "profit")), ">10"),
						"cost", pySpec(money(pyGet(x, "cost")), ">9"), "listed", pySpec(pyGet(x, "listed"), ">5"))+
					"  "+strings.Join(notes, "; "))
			}
		}
		if pyTruthy(scanned) {
			fmt.Fprintln(out, "  "+i18n.T("Not learned yet:"))
		} else {
			fmt.Fprintln(out, "  "+i18n.T("All recipes:"))
		}
		for _, x := range pyList(pyGet(p, "missing")) {
			acq := pyGet(x, "acquire")
			var src string
			if best := pyGet(acq, "best"); pyTruthy(best) {
				src = kind(best) + " " + money(pyGet(best, "price"))
			} else {
				var names []string
				for _, o := range pyList(pyGet(acq, "options")) {
					names = append(names, kind(o))
				}
				if src = strings.Join(names, ", "); src == "" {
					src = "?"
				}
			}
			flag := " "
			if pyTruthy(pyGet(x, "better")) {
				flag = "*"
			}
			skill := pyGet(acq, "reqSkill")
			if !pyTruthy(skill) {
				skill = "?"
			}
			fmt.Fprintln(out, "  "+flag+pySpec(pyCut(pyfmt.Str(pyGet(x, "name")), 40), "<40")+" "+
				i18n.T("profit {profit}  learnable {learnable} from {skill} {source}",
					"profit", pySpec(money(pyGet(x, "profit")), ">10"),
					"learnable", pySpec(learnable(pyGet(acq, "learnable")), "<5"), "skill", pySpec(skill, "<4"),
					"source", src))
		}
	}
}
