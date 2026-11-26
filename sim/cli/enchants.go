package cli

import (
	"fmt"
	"io"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/tools/enchants"
)

func init() {
	Register(Command{
		Name:    "enchants",
		Summary: "best permanent enchants per equipped slot with engine gain and reagent cost",
		Run:     runEnchants,
	})
}

func runEnchants(args []string, out io.Writer) error {
	f := NewFlags("enchants")
	var char, metric, engineMode, profile string
	var top, targetDelta int
	var fightSec, killsPerMin float64
	f.StringVar(&char, "char", "", "character name")
	f.IntVar(&top, "top", 3, "suggestions per slot")
	f.StringVar(&engineMode, "engine", "auto", "rating engine: auto, on, or off")
	f.Float64Var(&fightSec, "fight-seconds", 60, "fight length in seconds")
	f.IntVar(&targetDelta, "target-delta", -1, "enemy levels above character (-1 = automatic)")
	f.Float64Var(&killsPerMin, "kills-per-minute", -1, "killing blows per minute (-1 = automatic)")
	f.StringVar(&metric, "metric", "", "metric to optimize (dps, hps, ttoom)")
	f.StringVar(&profile, "profile", "", "weight scale profile (leveling, dungeon)")
	if err := f.Parse(args); err != nil {
		return err
	}
	r, err := enchants.Compute(f.Wow, char, enchants.Options{
		Top:            top,
		Engine:         engineMode,
		FightSeconds:   fightSec,
		TargetDelta:    targetDelta,
		KillsPerMinute: killsPerMin,
		Metric:         metric,
		Profile:        profile,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s (Lv %v) · %v -> %v %s (+%v %s)\n",
		r.Get("character"), r.Get("level"), r.Get("baseValue"), r.Get("totalValue"),
		r.Get("unit"), r.Get("totalGain"), r.Get("unit"))
	fmt.Fprintf(out, "%s: %v/%v · %s: %s\n",
		i18n.T("Enchanted slots"), r.Get("enchantedSlots"), r.Get("totalSlots"),
		i18n.T("Cost"), fmtCopper(r.Get("totalCost")))
	for _, raw := range r.Get("slots").([]any) {
		s := raw.(*pyjson.Obj)
		item := s.Get("item").(*pyjson.Obj)
		curName := i18n.T("none")
		if cur, ok := s.Get("current").(*pyjson.Obj); ok && cur != nil {
			curName = fmt.Sprint(cur.Get("name"))
		}
		fmt.Fprintf(out, "\n%s: %s [%s: %s]\n",
			s.Get("slotName"), item.Get("name"), i18n.T("Current"), curName)
		for _, craw := range s.Get("candidates").([]any) {
			c := craw.(*pyjson.Obj)
			tag := ""
			if c.Get("current") == true {
				tag = " [" + i18n.T("equipped") + "]"
			} else if c.Get("best") == true {
				tag = " [" + i18n.T("best") + "]"
			} else if c.Get("bestValue") == true {
				tag = " [" + i18n.T("best value") + "]"
			}
			costStr := "-"
			if c.Get("cost") != nil {
				costStr = fmtCopper(c.Get("cost"))
			}
			fmt.Fprintf(out, "  +%v %s  %-38s  %s  (%s)%s\n",
				c.Get("gain"), r.Get("unit"), c.Get("name"), costStr, c.Get("effect"), tag)
		}
	}
	return nil
}

func fmtCopper(v any) string {
	var c int64
	switch x := v.(type) {
	case int64:
		c = x
	case float64:
		c = int64(x)
	case int:
		c = int64(x)
	default:
		return "-"
	}
	if c <= 0 {
		return "0c"
	}
	g, s, r := c/10000, (c/100)%100, c%100
	if g > 0 {
		return fmt.Sprintf("%dg %ds", g, s)
	}
	if s > 0 {
		return fmt.Sprintf("%ds %dc", s, r)
	}
	return fmt.Sprintf("%dc", r)
}
