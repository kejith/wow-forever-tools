package bridge

import (
	"errors"
	"sort"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/tools/enginebundle"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

// orList is `x or []`.
func orList(v any) any {
	if sv.Truthy(v) {
		return v
	}
	return []any{}
}

// character is app_bridge.character: the Character page without its sections that load later (healing,
// damage, consumables): equipment with points on the weight scale, stats, professions, talents.
func character(a *Args) (*pyjson.Obj, error) {
	files := game.FindChars(a.Wow, a.Char)
	if len(files) == 0 {
		return nil, errors.New(i18n.T("No WoW Tools Forever export found for \"{char}\".", "char", a.Char))
	}
	c, err := loadChar(files[0])
	if err != nil {
		return nil, err
	}
	skills, talents := c.Tab("skills"), c.Tab("talents")
	// Points per item: what the set loses without it, on the same scale as the AH upgrades.
	eq, totals := upgrades.EquippedStats(c, nil)
	scale := upgrades.BuildScale(c, "", totals["hit"], totals["crit"])
	full := upgrades.SetValue(totals, scale)
	type item struct {
		slot float64
		obj  *pyjson.Obj
	}
	var equipment []item
	for _, e := range c.Tab("equipment").Tables() {
		rest := totals.Copy()
		slotV := e.Get("slot")
		slot, _ := slotV.(int64)
		rest.Add(eq[slot], -1)
		var label *string
		if _, ok := upgrades.SlotName[slot]; ok && slotV != nil {
			l := upgrades.SlotLabel(slot)
			label = &l
		}
		slotName := pyfmt.Str(slotV)
		if label != nil && *label != "" {
			slotName = *label
		}
		order, _ := sv.Num(slotV)
		equipment = append(equipment, item{order, pyjson.New(
			"slot", slotV, "slotName", slotName, "id", e.Get("id"), "name", e.Get("name"),
			"quality", e.Get("quality"), "ilvl", e.Get("ilvl"), "stats", upgrades.Short(e.Tab("stats")),
			"durability", e.Get("durability"), "score", pyfmt.Round(full-upgrades.SetValue(rest, scale), 1),
			"tip", upgrades.Tooltip(e, label))})
	}
	sort.SliceStable(equipment, func(i, j int) bool { return equipment[i].slot < equipment[j].slot })
	list := make([]any, len(equipment))
	for i, e := range equipment {
		list[i] = e.obj
	}
	classFile := c.Tab("character").Str("classFile")
	return pyjson.New(
		"key", game.CharKey(files[0]), "updated", c.Get("updated"), "character", orEmpty(c.Get("character")),
		"equipment", list, "weights", scale.Text, "profileLabel", scale.Label, "setValue", pyfmt.Round(full, 1),
		"professions", orList(skills.Get("professions")), "talents", orList(talents.Get("class")),
		"legacy", orList(talents.Get("legacy")), "errors", orEmpty(c.Get("errors")),
		// Whether the window should ask for the heal table (command "healing") and for the damage analysis
		// (command "damage"), and which of the two is the spec's own.
		"heals", enginebundle.HasMetric(classFile, "hps"), "deals", enginebundle.HasMetric(classFile, "dps"),
		"metric", enginebundle.DefaultMetric(classFile, enginebundle.SpecOf(c))), nil
}
