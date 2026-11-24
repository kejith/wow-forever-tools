package cli

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/tools/ingame"
)

// ingame is scripts/ingame.py: what the WoW Tools Forever addon shows in the game, per character; with --write it goes
// into Interface/AddOns/WoWToolsForever/AppData.lua.
func init() {
	Register(Command{Name: "ingame", Summary: "the lists, tooltip lines and vendor hints for the WoW Tools Forever addon",
		Run: func(args []string, out io.Writer) error {
			f := NewFlags("ingame")
			char := f.String("char", "", "only characters whose folder name contains this")
			write := f.Bool("write", false, "write "+ingame.DataFile+" into the installed addon")
			asJSON := f.Bool("json", false, "print the payload as JSON")
			if err := f.Parse(args); err != nil {
				return err
			}
			p := ingame.Compute(f.Wow, *char)
			var result *pyjson.Obj
			if *write {
				var err error
				if result, err = ingame.Write(f.Wow, p); err != nil {
					return err
				}
			}
			if *asJSON {
				var res any
				if result != nil {
					res = result
				}
				return JSON(out, pyjson.New("payload", p.Obj(), "result", res))
			}
			printPayload(out, p)
			scan := any("?")
			if p.ScanDate != "" {
				scan = p.ScanDate
			}
			when := time.Unix(p.Generated, 0).Local().Format("15:04")
			fmt.Fprintf(out, "\n%s\n", i18n.T("AH scan {date}, computed {time}. * = goes into the Auctionator shopping list.",
				"date", scan, "time", when))
			if pyTruthy(result) {
				switch {
				case pyTruthy(result.Get("written")):
					fmt.Fprintln(out, i18n.T("Written to {file}. Type /reload in the game.", "file", result.Get("file")))
				case pyTruthy(result.Get("unchanged")):
					fmt.Fprintln(out, i18n.T("Nothing changed, the file in the addon folder is current."))
				default:
					fmt.Fprintln(out, pyfmt.Str(result.Get("reason")))
				}
			}
			return nil
		}})
}

func printPayload(out io.Writer, p *ingame.Payload) {
	text := func(v any) string { // `v or ''`, as text
		if !pyTruthy(v) {
			return ""
		}
		return pyfmt.Str(v)
	}
	for _, name := range p.Names {
		c := p.Chars[name]
		fmt.Fprintf(out, "\n== %s (%s) ==\n", name, i18n.T("Level {n}", "n", c.Level))
		if c.Errors != nil {
			for _, k := range c.Errors.Keys() {
				fmt.Fprintf(out, "  ! %s: %s\n", k, pyfmt.Str(c.Errors.Get(k)))
			}
		}
		for _, one := range c.Lists {
			fmt.Fprintf(out, "\n[%s] %s\n", pyfmt.Str(one.Get("title")), plainMoney(text(one.Get("note"))))
			for _, s := range pyList(one.Get("sections")) {
				fmt.Fprintf(out, "  %s  %s\n", pyfmt.Str(pyGet(s, "title")), plainMoney(text(pyGet(s, "note"))))
				for _, r := range pyList(pyGet(s, "rows")) {
					count, price, mark, sub := "", "", " ", ""
					if n := pyGet(r, "n"); pyTruthy(n) {
						count = " x" + pyfmt.Str(n)
					}
					if v := pyGet(r, "price"); pyTruthy(v) {
						price = plainMoney(fmt.Sprintf("{m:%d}", pyRoundAny(v)))
					}
					if pyTruthy(pyGet(r, "shop")) {
						mark = "*"
					}
					if pyTruthy(pyGet(r, "sub")) {
						sub = "  "
					}
					fmt.Fprintf(out, "   %s%s%s%s  %s  %s\n", mark, sub, pyfmt.Str(pyGet(r, "name")), count,
						plainMoney(text(pyGet(r, "info"))), price)
				}
			}
		}
		fmt.Fprintf(out, "\n  %s\n", i18n.T("{n} items with a tooltip line, {v} vendor hints", "n", c.Tips.Len(),
			"v", c.Vendor.Len()))
	}
}

// pyRoundAny is round(v) of a number: a float half to even, an int as it is.
func pyRoundAny(v any) int64 {
	switch x := v.(type) {
	case float64:
		return pyfmt.RoundInt(x)
	case bool:
		if x {
			return 1
		}
		return 0
	}
	n, _ := sv.Num(v)
	return int64(n)
}

var moneyMark = regexp.MustCompile(`\{m:(-?\p{Nd}+)\}`)

// plainMoney is ingame.plain: the {m:<copper>} placeholders as "1g 5s 3c", and "||" as "|".
func plainMoney(s string) string {
	s = moneyMark.ReplaceAllStringFunc(s, func(m string) string {
		digits := moneyMark.FindStringSubmatch(m)[1]
		c, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			return m
		}
		sign := ""
		if c < 0 {
			sign, c = "-", -c
		}
		g, s, k := c/10000, c/100%100, c%100
		var parts []string
		if g != 0 {
			parts = append(parts, fmt.Sprintf("%dg", g))
		}
		if s != 0 {
			parts = append(parts, fmt.Sprintf("%ds", s))
		}
		if k != 0 || (g == 0 && s == 0) {
			parts = append(parts, fmt.Sprintf("%dc", k))
		}
		return sign + strings.Join(parts, " ")
	})
	return strings.ReplaceAll(s, "||", "|")
}
