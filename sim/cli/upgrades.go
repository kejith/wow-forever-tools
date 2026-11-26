package cli

import (
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/tools/consumables"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

// upgrades is scripts/upgrades.py: equipment upgrades from the auction house and from the gear the account
// already has, per slot and as the best package for the budget.
func init() {
	Register(Command{Name: "upgrades", Summary: "equipment upgrades from the auction house and your own gear",
		Run: runUpgrades})
}

// pdChoice checks an option against the values argparse allowed, as its choices= did.
func pdChoice(cmd, name, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	quoted := make([]string, len(allowed))
	for i, a := range allowed {
		quoted[i] = "'" + a + "'"
	}
	fmt.Fprintf(os.Stderr, "wowtools %s: argument --%s: invalid choice: '%s' (choose from %s)\n", cmd, name, value,
		strings.Join(quoted, ", "))
	return ErrUsage
}

type int64ListFlag []int64

func (f *int64ListFlag) String() string {
	parts := make([]string, len(*f))
	for i, v := range *f {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return strings.Join(parts, ",")
}

func (f *int64ListFlag) Set(s string) error {
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil || v <= 0 {
			return fmt.Errorf("invalid item ID %q", p)
		}
		*f = append(*f, v)
	}
	return nil
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }

func (f *stringListFlag) Set(s string) error {
	for _, part := range strings.Split(s, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p != "" {
			*f = append(*f, p)
		}
	}
	return nil
}

func buildItemFilter(cmd string, ids []int64, cats []string) (*upgrades.Filter, error) {
	for _, c := range cats {
		if err := pdChoice(cmd, "exclude-category", c, upgrades.KnownCategories...); err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 && len(cats) == 0 {
		return nil, nil
	}
	return upgrades.NewFilter(ids, cats), nil
}

func runUpgrades(args []string, out io.Writer) error {
	f := NewFlags("upgrades")
	char := f.String("char", "", "part of the character name")
	level := f.Int("level", 0, "override the level (default: from the export)")
	preview := f.Int("preview", 4, "also show items up to level + N")
	maxPrice := f.Float64("max-price", 0, "budget in gold, per item and for the package (default: your own gold)")
	top := f.Int("top", 3, "suggestions per slot")
	profile := f.String("profile", "", "dungeon or leveling (default: leveling below level 60)")
	sortBy := f.String("sort", "gain", "gain, or price = points per gold")
	engineOpt := f.String("engine", "auto", "rate with the combat engine instead of the weight scale: auto, on or off")
	fight := f.Float64("fight-seconds", 60, "fight length for the combat model")
	target := f.Int("target-delta", -1, "enemy levels above you for the combat model, 0 to 3 (default: from the profile)")
	kills := f.Float64("kills-per-minute", -1, "your killing blows per minute, for buffs like Spirit Tap")
	metric := f.String("metric", "", "metric of the combat model: dps, hps or ttoom (default: per class and spec)")
	method := f.String("method", "", "expectation, or montecarlo: plays the rotation with real rolls")
	iterations := f.Int("iterations", 0, "montecarlo: simulated fights (default 10000)")
	withConsumables := f.Bool("consumables", false, "combat model: rate every swap with the best set of consumables")
	var excludeItems int64ListFlag
	var excludeCats stringListFlag
	f.Var(&excludeItems, "exclude-item", "item ID to exclude (repeatable)")
	f.Var(&excludeCats, "exclude-category", "item category to exclude, e.g. pvp (repeatable)")
	asJSON := f.Bool("json", false, "print the result as JSON (for the app)")
	if err := f.Parse(args); err != nil {
		return err
	}
	filter, err := buildItemFilter("upgrades", excludeItems, excludeCats)
	if err != nil {
		return err
	}
	set := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if set["profile"] { // argparse checks a value given, not the default ""
		if err := pdChoice("upgrades", "profile", *profile, "dungeon", "leveling"); err != nil {
			return err
		}
	}
	for _, c := range []struct {
		name, value string
		allowed     []string
	}{
		{"sort", *sortBy, []string{"gain", "price"}},
		{"engine", *engineOpt, []string{"auto", "on", "off"}},
		{"target-delta", strconv.Itoa(*target), []string{"-1", "0", "1", "2", "3"}},
		{"metric", *metric, []string{"", "dps", "hps", "ttoom"}},
		{"method", *method, []string{"", "expectation", "montecarlo"}},
	} {
		if err := pdChoice("upgrades", c.name, c.value, c.allowed...); err != nil {
			return err
		}
	}
	o := upgrades.Options{Level: int64(*level), Preview: int64(*preview), MaxPrice: *maxPrice, Top: *top,
		Profile: *profile, Sort: *sortBy, Engine: *engineOpt, FightSeconds: *fight, TargetDelta: *target,
		KillsPerMinute: *kills, Metric: *metric, Method: *method, Iterations: *iterations, Filter: filter}
	if *withConsumables {
		wow, name := f.Wow, *char
		o.FightBuffs = func(metric string, seconds float64, targetDelta int) (*upgrades.Consumed, error) {
			c, err := consumables.FightBuffs(wow, name, metric, seconds, targetDelta)
			if err != nil && !consumables.IsLookup(err) {
				return nil, &upgrades.Fatal{Err: err}
			}
			return c, err
		}
	}
	obj, err := upgrades.Compute(f.Wow, *char, nil, o)
	if err != nil {
		return err
	}
	if *asJSON {
		engineNumbers(obj)
		return JSON(out, obj)
	}
	r, err := pdData(obj)
	if err != nil {
		return err
	}
	var b strings.Builder
	printUpgrades(&b, r)
	_, err = io.WriteString(out, b.String())
	return err
}

// upgradeLine is one candidate, as line() in upgrades.py's main.
func upgradeLine(c any) string {
	pct := pdSpec(i18n.T("new"), ">5")
	if g := pdGet(c, "gainPct"); g != nil {
		pct = pdSpec(g, "+4d") + "%"
	}
	flag, soon, both := "", "", ""
	if pdTruthy(pdGet(c, "isNew")) {
		flag = " " + i18n.T("[NEW in Forever]")
	}
	if pdTruthy(pdGet(c, "soon")) {
		soon = " " + i18n.T("(from level {level})", "level", pdGet(c, "reqLevel"))
	}
	if pdTruthy(pdGet(c, "twoHand")) {
		both = " " + i18n.T("[two-handed]")
	}
	cost := pdSpec(i18n.T("owned"), ">9") + "  " + strings.Repeat(" ", 8)
	if pdTruthy(pdGet(c, "price")) {
		cost = pdSpec(pdMoney(pdGet(c, "price")), ">9") + "  " + pdSpec(pdGet(c, "perGold"), "6.1f") + "/g"
	}
	where := ""
	if w := pdGet(c, "where"); pdTruthy(w) {
		where = " (" + pdStr(w) + ")"
	}
	setNote := ""
	if scs := pdList(pdGet(c, "setChanges")); len(scs) > 0 {
		parts := make([]string, 0, len(scs))
		for _, sc := range scs {
			sign := "+"
			if pdStr(pdGet(sc, "kind")) == "loss" {
				sign = "-"
			}
			parts = append(parts, fmt.Sprintf("%s%s (%v)", sign, pdStr(pdGet(sc, "set")), pdGet(sc, "threshold")))
		}
		setNote = " [" + strings.Join(parts, ", ") + "]"
	}
	return pdSpec(pdGet(c, "gain"), "+6.1f") + " " + pct + "  " + cost + "  " + pdStr(pdGet(c, "name")) + where +
		flag + soon + both + setNote + "  [" + pdStr(pdGet(c, "stats")) + "]  id " + pdStr(pdGet(c, "id"))
}

func printUpgrades(w *strings.Builder, r any) {
	p := func(s string) { w.WriteString(s + "\n") }
	when := time.Unix(int64(pdNum(pdOr(pdGet(r, "exportUpdated"), int64(0)))), 0).Local()
	p(i18n.T("AH scan: {date} | item export: {when} | level {level} | budget {budget} | {count} items, "+
		"{missing} not loaded", "date", pdGet(r, "scanDate"), "when", when.Format("2006-01-02 15:04"),
		"level", pdGet(r, "level"), "budget", pdMoney(pdGet(r, "budget")), "count", pdGet(r, "exportCount"),
		"missing", pdGet(r, "exportMissing")))
	eng := pdGet(r, "engine")
	if pdTruthy(eng) {
		p(pdStr(pdGet(r, "weights"))) // the combat model's summary; gains below are the metric change
		if t := pdGet(eng, "downrank", "text"); pdTruthy(t) {
			p(pdStr(t))
		}
		for _, o := range pdList(pdGet(eng, "healOptions")) {
			text := i18n.T("{hps:6.1f} HPS in the fight · {hpm:.2f} healing per mana · {heal:.0f} healing for "+
				"{mana:.0f} mana over {cast:.1f}s · {spell}", "hps", pdOr(pdGet(o, "hpsSustained"), int64(0)),
				"hpm", pdOr(pdGet(o, "healPerMana"), int64(0)), "heal", pdOr(pdGet(o, "healPerCast"), int64(0)),
				"mana", pdOr(pdGet(o, "manaPerCast"), int64(0)), "cast", pdOr(pdGet(o, "castSec"), int64(0)),
				"spell", pdGet(o, "spell"))
			if l := pdGet(o, "bonusLossPct"); pdTruthy(l) {
				text += " " + i18n.T("(outgrown: {pct:.0f}% less healing bonus)", "pct", l)
			}
			if pdTruthy(pdGet(o, "chosen")) {
				text += " <=="
			}
			p("   " + text)
		}
	} else {
		p(i18n.T("Scale \"{label}\" (1 point = 1 shadow spell power), value of your gear now: {value}",
			"label", pdGet(r, "profileLabel"), "value", pdGet(r, "setValue")))
	}
	for _, row := range pdList(pdGet(r, "scale")) {
		p("   " + pdSpec(pdGet(row, "label"), "24") + " " + pdSpec(pdGet(row, "weight"), "5.2f") + "  " +
			pdStr(pdGet(row, "note")))
	}
	if cov := pdGet(eng, "coverage"); pdTruthy(cov) {
		p("== " + i18n.T("What the model does with your spells and talents ({n} rotations compared)",
			"n", pdGet(cov, "candidates")))
		if !pdTruthy(pdGet(cov, "exactTalents")) {
			p("   " + i18n.T("The talent tables of the game are not in the cache: talent values are estimates."))
		}
		parts := []struct {
			title string
			rows  []any
		}{{i18n.T("Spells"), pdList(pdGet(cov, "spells"))}, {i18n.T("Talents"), pdList(pdGet(cov, "talents"))}}
		if sb := pdList(pdGet(cov, "setBonuses")); len(sb) > 0 {
			parts = append(parts, struct {
				title string
				rows  []any
			}{i18n.T("Set bonuses"), sb})
		}
		for _, part := range parts {
			p("   " + part.title)
			var quiet []any
			for _, e := range part.rows {
				if pdStr(pdGet(e, "status")) == "none" {
					quiet = append(quiet, pdGet(e, "label"))
					continue
				}
				flag := ""
				if pdTruthy(pdGet(e, "unverified")) {
					flag = " [" + i18n.T("assumption") + "]"
				}
				p("      " + pdSpec(pdGet(e, "statusLabel"), "17") + " " + pdStr(pdGet(e, "label")) + ": " +
					pdStr(pdGet(e, "text")) + flag)
			}
			if len(quiet) > 0 {
				p("      " + pdSpec(i18n.T("no influence"), "17") + " " + pdJoin(", ", quiet))
			}
		}
	}
	if pdTruthy(pdGet(eng, "buffs")) {
		p("   " + pdStr(pdGet(eng, "buffs", "text")))
	}
	if pdTruthy(pdGet(eng, "consumables")) {
		p("   " + pdStr(pdGet(eng, "consumables", "text")))
	}
	if check := pdGet(eng, "tooltipCheck"); pdTruthy(check) {
		p("   " + pdStr(pdGet(check, "text")))
		for _, o := range pdList(pdGet(check, "off")) {
			p("      " + pdStr(pdGet(o, "label")) + ": " + pdStr(pdGet(o, "model")) + " | " + pdCut(pdGet(o, "text"), 160))
		}
	}
	p(i18n.T("Prices are the lowest buyout at the scan."))
	if unknown := pdList(pdGet(r, "owned", "unknown")); len(unknown) > 0 {
		var names []string
		for i, u := range unknown {
			if i == 6 {
				break
			}
			names = append(names, pdStr(pdGet(u, "name"))+" "+pdStr(pdGet(u, "where")))
		}
		joined := strings.Join(names, ", ")
		if len(unknown) > 6 {
			joined += ", ..."
		}
		p(i18n.T("{n} items you own are only known by name ({names}): open the bank once with WoW Tools Forever 1.12 or "+
			"newer, then /reload.", "n", len(unknown), "names", joined))
	}
	p("")
	pkg := pdGet(r, "package")
	unit := i18n.T("points")
	switch pdStr(pdOr(pdGet(eng, "metric"), "")) {
	case "dps":
		unit = "DPS"
	case "hps":
		unit = "HPS"
	case "ttoom":
		unit = "s"
	}
	if items := pdList(pdGet(pkg, "items")); len(items) > 0 {
		pct := ""
		if g := pdGet(pkg, "gainPct"); g != nil {
			pct = " (" + pdSpec(g, "+d") + "%)"
		}
		p("== " + i18n.T("Best package for {budget}: {gain} {unit}{pct} for {cost}", "budget",
			pdMoney(pdGet(r, "budget")), "gain", pdSpec(pdGet(pkg, "gain"), "+.1f"), "unit", unit, "pct", pct,
			"cost", pdMoney(pdGet(pkg, "cost"))))
		for _, c := range items {
			p("   " + pdSpec(pdGet(c, "slotName"), "10") + " " + upgradeLine(c))
			tail := ""
			if rep := pdList(pdGet(c, "replaces")); len(rep) > 0 {
				tail = i18n.T(", replaces {items}", "items", pdJoin(" + ", rep))
			}
			p("   " + strings.Repeat(" ", 10) + " " + i18n.T("up to here {cost} for {gain} {unit}", "cost",
				pdMoney(pdGet(c, "cumCost")), "gain", pdSpec(pdGet(c, "cumGain"), "+.1f"), "unit", unit) + tail)
		}
		p("")
	}
	for _, s := range pdList(pdGet(r, "slots")) {
		cur := pdGet(s, "current")
		name, stats := any("-"), any("")
		if pdTruthy(cur) {
			name, stats = pdGet(cur, "name"), pdGet(cur, "stats")
		}
		p("== " + i18n.T("{slot}: now {name} ({stats})", "slot", pdGet(s, "slotName"), "name", name, "stats", stats))
		for _, c := range pdList(pdGet(s, "candidates")) {
			p("   " + upgradeLine(c))
			for _, t := range pdList(pdGet(c, "effects")) {
				p("            " + pdStr(t))
			}
		}
	}
	if review := pdList(pdGet(r, "review")); len(review) > 0 {
		p("\n== " + i18n.T("With a special effect, please look for yourself"))
		for _, x := range review {
			p("   " + pdSpec(pdGet(x, "slot"), "10") + " " + pdSpec(pdMoney(pdGet(x, "price")), ">9") + "  " +
				pdStr(pdGet(x, "name")) + ": " + pdCut(pdJoin(" / ", pdList(pdGet(x, "effects"))), 160))
		}
	}
}

// engineNumbers writes the numbers upgrades.py copied from the engine's answer as the script printed them.
// The script read that answer as JSON written by Go's encoding/json, which writes a whole float without a
// point, so json.loads made it an int: a 60 s fight was "seconds": 60, a breakdown row worth nothing
// "weight": 0. upgrades.Compute keeps them as floats (the bridge's parity check counts 60 and 60.0 as
// equal); the text does not depend on it, only --json does.
func engineNumbers(r *pyjson.Obj) {
	eng, ok := r.Get("engine").(*pyjson.Obj)
	if !ok || eng == nil {
		return
	}
	for _, k := range []string{"value", "ttoomSeconds", "manaSpentPs", "manaBudgetPs", "selfHealPs", "fight", "mc",
		"downrank", "healOptions"} {
		if v := eng.Get(k); v != nil {
			eng.Set(k, wholeInts(v))
		}
	}
	// The breakdown's weight is round(row["value"] or 0, 1): an int only where the engine's value was whole, which
	// a rotation's share of the damage is only when it is none (0 for a curse that deals nothing itself); a
	// value that rounds to a whole number stays a float.
	if rows, ok := r.Get("scale").([]any); ok {
		for _, row := range rows {
			if o, ok := row.(*pyjson.Obj); ok {
				if w, ok := o.Get("weight").(float64); ok && w == 0 {
					o.Set("weight", int64(0))
				}
			}
		}
	}
}

// wholeInts is a value as json.loads reads what encoding/json wrote of it: whole floats become ints, through
// objects and lists.
func wholeInts(v any) any {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e21 {
			return int64(x)
		}
	case *pyjson.Obj:
		if x != nil {
			for _, k := range x.Keys() {
				x.Set(k, wholeInts(x.Get(k)))
			}
		}
	case []any:
		for i := range x {
			x[i] = wholeInts(x[i])
		}
	case []*pyjson.Obj:
		for _, o := range x {
			wholeInts(o)
		}
	case map[string]any:
		for k, e := range x {
			x[k] = wholeInts(e)
		}
	}
	return v
}
