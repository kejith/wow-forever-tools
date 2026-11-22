package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/tools/pricehistory"
)

// pricehistory is scripts/pricehistory.py: the price history of the items in Auctionator's database.
func init() {
	Register(Command{Name: "pricehistory", Summary: "price history of the items in Auctionator's database",
		Run: func(args []string, out io.Writer) error {
			f := NewFlags("pricehistory")
			item := f.Int64("item", 0, "only this item ID")
			asJSON := f.Bool("json", false, "print the result as JSON")
			if err := f.Parse(args); err != nil {
				return err
			}
			r, err := pricehistory.Compute(f.Wow, nil)
			if err != nil {
				// The script did not catch these: Python printed a traceback ending in the exception's line.
				switch err.Error() {
				case i18n.T("Auctionator.lua not found under {wow}", "wow", f.Wow):
					return errors.New("FileNotFoundError: " + err.Error())
				case i18n.T("No price data found in Auctionator.lua."):
					return errors.New("LookupError: " + err.Error())
				}
				return err
			}
			items := pyList(r.Get("items"))
			if *item != 0 {
				var only []any
				for _, it := range items {
					if id, _ := sv.Num(pyGet(it, "id")); int64(id) == *item {
						only = append(only, it)
					}
				}
				items = only
				if items == nil {
					items = []any{}
				}
				r.Set("items", items)
			}
			if *asJSON {
				return JSON(out, r)
			}
			fmt.Fprintln(out, i18n.T("{n} items, {unnamed} without a name, last scan day {date}", "n", r.Get("count"),
				"unnamed", r.Get("unnamed"), "date", r.Get("lastScanDate")))
			if *item == 0 {
				items = items[:min(50, len(items))]
			}
			for _, it := range items {
				name := pyGet(it, "name")
				if !pyTruthy(name) {
					name = "?"
				}
				fmt.Fprintf(out, "%s  %s\n", pySpec(pyGet(it, "id"), ">7"), pyfmt.Str(name))
				if *item == 0 {
					continue
				}
				for _, d := range pyList(pyGet(it, "days")) {
					day := pyList(d)
					n, _ := sv.Num(day[0])
					listed := ""
					if day[3] != nil {
						listed = pyfmt.Str(day[3])
					}
					fmt.Fprintf(out, "         %s  %s  %s  %s\n", pricehistory.DayDate(int64(n)), pySpec(day[1], ">9"),
						pySpec(day[2], ">9"), listed)
				}
			}
			return nil
		}})
}
