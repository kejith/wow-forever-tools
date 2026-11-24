package cli

import (
	"io"
	"strconv"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/tools/consumables"
)

// consumables is scripts/consumables.py: the strongest potions, food, drinks, bandages and buffs for the
// character's level, and with the combat engine the consumables for a fight: the strongest set, or with
// --set value the one that adds the most per gold.
func init() {
	Register(Command{Name: "consumables", Summary: "potions, food, bandages and buffs for your level, and the best set for a fight",
		Run: runConsumables})
}

func runConsumables(args []string, out io.Writer) error {
	f := NewFlags("consumables")
	char := f.String("char", "", "part of the character name")
	top := f.Int("top", 5, "items per category (0 = all)")
	allBuffs := f.Bool("all-buffs", false, "also buffs that do not help the class")
	engineOpt := f.String("engine", "auto", "rate the consumables for a fight with the combat engine: auto, on or off")
	fight := f.Float64("fight-seconds", 60, "length of the fight the engine plays")
	target := f.Int("target-delta", -1, "enemy levels above you, 0 to 3 (default: +2 while leveling, +3 at 60)")
	setOpt := f.String("set", "best", "the set for a fight: best = the strongest, value = the most gain per gold")
	budget := f.Float64("budget", 0, "with --set value: gold per hour of fighting the set may cost (0 = no limit)")
	asJSON := f.Bool("json", false, "print the result as JSON")
	if err := f.Parse(args); err != nil {
		return err
	}
	if err := pdChoice("consumables", "engine", *engineOpt, "auto", "on", "off"); err != nil {
		return err
	}
	if err := pdChoice("consumables", "target-delta", strconv.Itoa(*target), "-1", "0", "1", "2", "3"); err != nil {
		return err
	}
	if err := pdChoice("consumables", "set", *setOpt, "best", "value"); err != nil {
		return err
	}
	set := consumables.Set{Value: *setOpt == "value", Budget: *budget}
	obj, err := consumables.Compute(f.Wow, *char, *top, *allBuffs, *engineOpt, *fight, *target, nil, set)
	if err != nil {
		return err
	}
	if *asJSON {
		return JSON(out, obj)
	}
	r, err := pdData(obj)
	if err != nil {
		return err
	}
	var b strings.Builder
	printConsumables(&b, r)
	_, err = io.WriteString(out, b.String())
	return err
}

// consMoney is g() of consumables.py's main: "–" for no price.
func consMoney(v any) string {
	if v == nil {
		return "–"
	}
	return pdMoney(v)
}

// consHave is have() of consumables.py.
func consHave(x any) string {
	if n := pdGet(x, "owned"); pdTruthy(n) {
		return i18n.T("{n} in bags", "n", n)
	}
	return ""
}

func printConsumables(w *strings.Builder, r any) {
	p := func(s string) { w.WriteString(s + "\n") }
	p(i18n.T("{char}  |  level {level}  |  AH scan {date}", "char", pdGet(r, "character"), "level",
		pdGet(r, "level"), "date", pdOr(pdGet(r, "scanDate"), "–")))
	groups := append(append([]any{}, pdList(pdGet(r, "categories"))...), pdList(pdGet(r, "buffs"))...)
	for _, group := range groups {
		p("\n== " + pdStr(pdGet(group, "label")) + " ==")
		for _, x := range pdList(pdGet(group, "items")) {
			flag := " "
			if pdTruthy(pdGet(x, "best")) {
				flag = "*"
			}
			craft := pdStr(pdOr(pdGet(x, "craft", "profession"), ""))
			p(" " + flag + pdSpec(pdCut(pdGet(x, "name"), 36), "<36") + " " + pdSpec(pdCut(pdGet(x, "effect"), 44), "<44") +
				" " + pdSpec(consMoney(pdGet(x, "price")), ">10") + "  " + consHave(x) + "  " + craft)
		}
		if n := pdGet(group, "next"); pdTruthy(n) {
			if skill := pdGet(n, "reqSkill"); pdTruthy(skill) && !pdTruthy(pdGet(skill, "ok")) {
				p("   " + i18n.T("next: {name} with {skill} {rank}", "name", pdGet(n, "name"), "skill",
					pdGet(skill, "name"), "rank", pdGet(skill, "rank")))
			} else {
				p("   " + i18n.T("next: {name} from level {level}", "name", pdGet(n, "name"), "level",
					pdGet(n, "reqLevel")))
			}
		}
	}
	if note := pdGet(r, "planNote"); pdTruthy(note) {
		p("\n" + i18n.T("No consumables for a fight: {why}", "why", note))
	}
	for _, plan := range pdList(pdGet(r, "plans")) {
		printPlan(p, plan)
	}
}

// consPerGold is a gain per gold as the set by value shows it; "–" for an item that costs nothing or has no
// price.
func consPerGold(v any) string {
	if v == nil {
		return "–"
	}
	return i18n.T("{n:.1f}/g", "n", v)
}

// printPlan is show_plan of consumables.py: the best set, then every kind with its rows. The set by value
// (--set value) also has what an hour of fighting costs and the gain per gold of that.
func printPlan(p func(string), plan any) {
	unit := pdStr(pdGet(plan, "unit"))
	if pdStr(pdGet(plan, "mode")) == "value" {
		printValuePlan(p, plan, unit)
		return
	}
	p("\n" + i18n.T("=== Consumables for a fight of {seconds:.0f} s: {title} ===", "seconds", pdGet(plan, "seconds"),
		"title", pdGet(plan, "title")))
	p(i18n.T("{base:.1f} {unit} without, {value:.1f} {unit} with the best set ({gain:+.1f}, {pct:+.1f} %), "+
		"to buy: {cost}", "base", pdGet(plan, "base"), "value", pdGet(plan, "value"), "unit", unit, "gain",
		pdGet(plan, "gain"), "pct", pdGet(plan, "gainPct"), "cost", consMoney(pdGet(plan, "cost"))))
	if active := pdList(pdGet(plan, "active")); len(active) > 0 {
		p(i18n.T("On you at the snapshot, so already in the numbers: {names}", "names", pdJoin(", ", active)))
	}
	labels := map[string]string{}
	for _, k := range pdList(pdGet(plan, "kinds")) {
		labels[pdStr(pdGet(k, "key"))] = pdStr(pdGet(k, "label"))
	}
	for _, x := range pdList(pdGet(plan, "chosen")) {
		p(" " + pdSpec(pdGet(x, "chosen"), ">2") + ". " + pdSpec(pdCut(pdGet(x, "name"), 36), "<36") + " " +
			pdSpec(pdCut(labels[pdStr(pdGet(x, "kind"))], 20), "<20") + " " + pdSpec(pdGet(x, "adds"), ">+7.1f") + " " +
			unit + "  " + pdSpec(consMoney(pdGet(x, "price")), ">10") + "  " + consHave(x))
	}
	printKinds(p, plan, unit, false)
}

// printValuePlan is the set by value: per item what it adds, what an hour of fighting with it costs and the
// gain per gold of that, in the order of that value.
func printValuePlan(p func(string), plan any, unit string) {
	p("\n" + i18n.T("=== Consumables for a fight of {seconds:.0f} s: {title} ===", "seconds", pdGet(plan, "seconds"),
		"title", pdGet(plan, "title")))
	p(i18n.T("{base:.1f} {unit} without, {value:.1f} {unit} with the set by value ({gain:+.1f}, {pct:+.1f} %), "+
		"{hour} per hour of fighting, to buy: {cost}", "base", pdGet(plan, "base"), "value", pdGet(plan, "value"),
		"unit", unit, "gain", pdGet(plan, "gain"), "pct", pdGet(plan, "gainPct"), "hour",
		pdMoney(pdGet(plan, "costPerHour")), "cost", consMoney(pdGet(plan, "cost"))))
	if b := pdGet(plan, "budget"); pdTruthy(b) {
		p(i18n.T("Budget: {budget} per hour of fighting", "budget", pdMoney(b)))
	}
	p(i18n.T("In buying order: what a step adds, what an hour of fighting costs more with it (a buff for the "+
		"time it lasts, a potion in every fight), the gain per gold of that, then {unit} and cost so far.",
		"unit", unit))
	if active := pdList(pdGet(plan, "active")); len(active) > 0 {
		p(i18n.T("On you at the snapshot, so already in the numbers: {names}", "names", pdJoin(", ", active)))
	}
	labels := map[string]string{}
	for _, k := range pdList(pdGet(plan, "kinds")) {
		labels[pdStr(pdGet(k, "key"))] = pdStr(pdGet(k, "label"))
	}
	for _, s := range pdList(pdGet(plan, "steps")) {
		x := pdGet(s, "item")
		hour := "–"
		if c := pdNum(pdGet(s, "costPerHour")); c > 0 {
			hour = i18n.T("{cost}/h", "cost", pdMoney(c))
		} else if c < 0 {
			hour = "−" + i18n.T("{cost}/h", "cost", pdMoney(-c))
		}
		note := consHave(x)
		if out := pdList(pdGet(s, "replaces")); len(out) > 0 {
			names := make([]any, len(out))
			for i, o := range out {
				names[i] = pdGet(o, "name")
			}
			note = i18n.T("replaces {names}", "names", pdJoin(", ", names))
		}
		p(" " + pdSpec(pdGet(s, "n"), ">2") + ". " + pdSpec(pdCut(pdGet(x, "name"), 36), "<36") + " " +
			pdSpec(pdCut(labels[pdStr(pdGet(x, "kind"))], 20), "<20") + " " + pdSpec(pdGet(s, "adds"), ">+7.1f") + " " +
			unit + "  " + pdSpec(hour, ">12") + "  " + pdSpec(consPerGold(pdGet(s, "perGold")), ">9") + "  " +
			pdSpec(pdGet(s, "value"), ">7.1f") + " " + unit + "  " +
			pdSpec(i18n.T("{cost}/h", "cost", pdMoney(pdGet(s, "spent"))), ">12") + "  " + note)
	}
	printKinds(p, plan, unit, true)
}

// printKinds prints every kind of a plan with its rows; value adds the gain per gold.
func printKinds(p func(string), plan any, unit string, value bool) {
	for _, kind := range pdList(pdGet(plan, "kinds")) {
		head := "\n-- " + pdStr(pdGet(kind, "label")) + ": " + pdStr(pdGet(kind, "rule"))
		if a := pdGet(kind, "assumed"); pdTruthy(a) {
			head += " (" + pdStr(a) + ")"
		}
		p(head)
		for _, x := range pdList(pdGet(kind, "items")) {
			flag := " "
			if pdTruthy(pdGet(x, "chosen")) {
				flag = "*"
			}
			note := ""
			switch {
			case pdTruthy(pdGet(x, "active")):
				note = i18n.T("on you")
			case pdTruthy(pdGet(x, "onYou")):
				note = i18n.T("{name} is on you already", "name", pdGet(x, "onYou"))
			case !pdTruthy(pdGet(x, "available")):
				note = i18n.T("not in the AH")
			case pdTruthy(pdGet(x, "tooCostly")):
				note = i18n.T("costs more than half your health")
			case pdTruthy(pdGet(x, "noPrice")):
				note = i18n.T("no price known")
			case pdTruthy(pdGet(x, "overBudget")):
				note = i18n.T("over the budget")
			case pdTruthy(pdGet(x, "blockedBy")):
				note = i18n.T("instead: {name}", "name", pdGet(x, "blockedBy"))
			}
			worth := ""
			if value {
				worth = pdSpec(consPerGold(pdGet(x, "perGold")), ">9") + "  "
			}
			p(" " + flag + pdSpec(pdCut(pdGet(x, "name"), 36), "<36") + " " + pdSpec(pdCut(pdGet(x, "effect"), 40), "<40") +
				" " + pdSpec(pdGet(x, "gain"), ">+7.1f") + " " + unit + "  " + worth +
				pdSpec(consMoney(pdGet(x, "price")), ">10") + "  " + consHave(x) + "  " + note)
		}
		if more := pdGet(kind, "more"); pdTruthy(more) {
			p("   " + i18n.T("… and {n} more", "n", more))
		}
	}
}
