package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/tools/writs"
)

// writs is scripts/writs.py: the best craftsman's writ per tier by silver per reputation, and the trade
// crates by silver per Merchant's Favor.
func init() {
	Register(Command{Name: "writs", Summary: "the best crafting writ per tier and the trade crates",
		Run: func(args []string, out io.Writer) error {
			f := NewFlags("writs")
			char := f.String("char", "", "part of the character name (folder under WTF), e.g. Malgruk")
			top := f.Int("top", 3, "alternatives per tier")
			ownedAtAH := f.Bool("owned-at-ah-price", false,
				"count your own items at their AH price instead of free (what you could sell them for)")
			asJSON := f.Bool("json", false, "print the result as JSON (for the app)")
			if err := f.Parse(args); err != nil {
				return err
			}
			r, err := writs.Calc(writs.Options{Wow: f.Wow, Char: *char, Top: *top, OwnedAtAHPrice: *ownedAtAH})
			if err != nil {
				return err
			}
			if *asJSON {
				return JSON(out, r.Obj())
			}
			printWrits(out, r, *top)
			return nil
		}})
}

func printWrits(out io.Writer, r *writs.Result, top int) {
	var scan any
	if r.ScanDate != "" {
		scan = r.ScanDate
	}
	who := r.Character
	if who == "" {
		who = "-"
	}
	fmt.Fprintln(out, i18n.T("AH data: last scan day {date}  |  character: {char}", "date", scan, "char", who))
	if rep := r.Reputation; rep != nil {
		fmt.Fprintln(out, i18n.T("Reputation {name}: {standing} {value}/{max} ({left} to the next standing)",
			"name", rep.Name, "standing", rep.Standing, "value", rep.Value, "max", rep.Max, "left", rep.Max-rep.Value))
	}
	fmt.Fprintln(out, i18n.T("Note: prices are the lowest buyout at the last scan, not live auctions.")+"\n")
	money := func(c int64) string { return game.Money(float64(c)) }
	for _, t := range r.Tiers {
		fmt.Fprintf(out, "== %s ==\n", t.Name)
		for n, b := range t.Best {
			mark := "  "
			if n == 0 {
				mark = ">>"
			}
			q := ""
			if b.Qty > 1 {
				q = fmt.Sprintf(" (%dx)", b.Qty)
			}
			fmt.Fprintln(out, " "+mark+" "+pySpec(b.Name+q, "<36")+" "+pySpec(b.Prof, "<16")+" "+
				i18n.T("writ {writ} + item {item} - pouch {pouch} = net {net}  {per} s/rep  [{listed} writs in the AH]",
					"writ", pySpec(money(b.WCost), ">8"), "item", pySpec(money(b.ICost), ">8"),
					"pouch", pySpec(money(b.Payout), ">7"), "net", pySpec(money(b.Net), ">8"),
					"per", pySpec(b.Per/100, ".2f"), "listed", b.Writs)+" "+b.Note)
		}
		if t.Sealed != 0 {
			fmt.Fprintln(out, "    "+i18n.T("Tip: you have {n}x sealed writ of this tier. Open it first, it may be cheaper.",
				"n", t.Sealed))
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, i18n.T("Per day (the best of each tier): {total}, net after coin pouches {net} for {rep} reputation",
		"total", money(r.DailyTotal), "net", money(r.DailyNet), "rep", r.DailyRep))
	seen := map[string]bool{}
	var src []string
	for _, t := range r.Tiers {
		for _, b := range t.Best {
			if !seen[b.PayoutSource] {
				seen[b.PayoutSource] = true
				src = append(src, b.PayoutSource)
			}
		}
	}
	sort.Strings(src)
	fmt.Fprintln(out, i18n.T("Coin pouches: {sources}", "sources", strings.Join(src, " | "))+"\n")

	have := ""
	if r.Favor != nil {
		have = i18n.T(", you have {n}", "n", r.Favor)
	}
	fmt.Fprintln(out, "== "+i18n.T("Trade crates (Merchant's Favor{have})", "have", have)+" ==")
	for _, t := range r.Crates.Tiers {
		low := ""
		if t.TooLow {
			low = "  " + i18n.T("(from level {level})", "level", t.Level)
		}
		fmt.Fprintln(out, "-- "+i18n.T("{tier}: {favor} favor, green {green} ({source})", "tier", t.Name,
			"favor", pySpec(t.Favor, "g"), "green", pySpec(t.FavorGreen, "g"), "source", t.FavorSource)+low)
		for _, c := range t.Rows[:pySliceEnd(len(t.Rows), top)] {
			bundle := "-"
			if b := c.Best; b != nil {
				bundle = fmt.Sprintf("%dx %s %s", b.Qty, b.Name, money(*b.Cost))
			}
			per := i18n.T("incomplete")
			if c.Complete {
				per = i18n.T("{per} s/favor", "per", pySpec(*c.Per/100, ".2f"))
			}
			crate := "?"
			if c.CrateCost != nil {
				crate = money(*c.CrateCost)
			}
			fmt.Fprintln(out, "    "+pySpec(c.Kind, "<18")+" "+i18n.T("crate {crate} + {bundle} = {total}  {per}",
				"crate", pySpec(crate, ">8"), "bundle", pySpec(bundle, "<40"), "total", pySpec(money(c.Cost), ">8"),
				"per", per)+"  "+c.Note)
		}
	}
}
