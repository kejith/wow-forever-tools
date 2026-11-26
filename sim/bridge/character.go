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
	// Points per item: what the set loses without it (including any broken item set bonus), on the same scale as the AH upgrades.
	eq, totals := upgrades.EquippedStats(c, nil)
	tracker := upgrades.NewSetTracker(c, upgrades.LoadSetCatalog(c, ""))
	enchCat := upgrades.LoadEnchantCatalog(c, "")
	totalsWithSets := totals.Copy()
	totalsWithSets.Add(tracker.BaseScale, 1)
	scale := upgrades.BuildScale(c, "", totalsWithSets["hit"], totalsWithSets["crit"])
	full := upgrades.SetValue(totalsWithSets, scale)
	type item struct {
		slot float64
		obj  *pyjson.Obj
	}
	var equipment []item
	classFile := c.Tab("character").Str("classFile")
	for _, e := range c.Tab("equipment").Tables() {
		rest := totalsWithSets.Copy()
		slotV := e.Get("slot")
		slot, _ := slotV.(int64)
		rest.Add(eq[slot], -1)
		if sd := tracker.ScaleDelta([]int64{slot}, nil); len(sd) > 0 {
			rest.Add(sd, 1)
		}
		var label *string
		if _, ok := upgrades.SlotName[slot]; ok && slotV != nil {
			l := upgrades.SlotLabelFor(slot, classFile)
			label = &l
		}
		slotName := pyfmt.Str(slotV)
		if label != nil && *label != "" {
			slotName = *label
		}
		order, _ := sv.Num(slotV)
		itemObj := pyjson.New(
			"slot", slotV, "slotName", slotName, "id", e.Get("id"), "name", e.Get("name"),
			"quality", e.Get("quality"), "ilvl", e.Get("ilvl"), "stats", upgrades.Short(e.Tab("stats")),
			"durability", e.Get("durability"), "score", pyfmt.Round(full-upgrades.SetValue(rest, scale), 1),
			"tip", upgrades.Tooltip(e, label))
		if ench := upgrades.EquippedEnchantJSON(e, enchCat); ench != nil {
			itemObj.Set("enchant", ench)
		}
		equipment = append(equipment, item{order, itemObj})
	}
	sort.SliceStable(equipment, func(i, j int) bool { return equipment[i].slot < equipment[j].slot })
	list := make([]any, len(equipment))
	for i, e := range equipment {
		list[i] = e.obj
	}
	spec := enginebundle.SpecOf(c)
	out := pyjson.New(
		"key", game.CharKey(files[0]), "updated", c.Get("updated"), "character", orEmpty(c.Get("character")),
		"equipment", list, "weights", scale.Text, "profileLabel", scale.Label, "setValue", pyfmt.Round(full, 1),
		"professions", orList(skills.Get("professions")), "talents", orList(talents.Get("class")),
		"legacy", orList(talents.Get("legacy")), "errors", orEmpty(c.Get("errors")),
		"spec", spec, "specName", enginebundle.SpecName(c), "role", enginebundle.RoleForChar(c),
		// Whether the window should ask for the heal table (command "healing") and for the damage analysis
		// (command "damage"), and which of the two is the spec's own.
		"heals", enginebundle.HasMetric(classFile, "hps"), "deals", enginebundle.HasMetric(classFile, "dps"),
		"metric", enginebundle.DefaultMetric(classFile, spec))
	if sets := upgrades.SummariesJSON(tracker.ActiveSummaries()); len(sets) > 0 {
		out.Set("sets", sets)
	}
	if we := upgrades.WeaponEnchantsJSON(c, enchCat); len(we) > 0 {
		out.Set("weaponEnchants", we)
	}
	return out, nil
}
