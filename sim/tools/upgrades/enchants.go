package upgrades

import (
	"fmt"

	"github.com/kejith/wow-tools-forever/sim/enchants"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/internal/tables"
)

// LoadEnchantCatalog ensures the enchant DB2 tables are cached for the snapshot's client build
// and loads the decoded enchant catalog. Returns an empty catalog offline or without build info.
func LoadEnchantCatalog(c *sv.Table, cacheDir string) *enchants.Catalog {
	if c == nil {
		return enchants.Empty()
	}
	client := c.Tab("client")
	if !sv.Truthy(client.Get("version")) || !sv.Truthy(client.Get("build")) {
		return enchants.Empty()
	}
	build := pyfmt.Str(client.Get("version")) + "." + pyfmt.Str(client.Get("build"))
	locale := client.Str("locale")
	if locale == "" {
		locale = "deDE"
	}
	if cacheDir == "" {
		cacheDir = tables.CacheDir()
	}
	enchants.EnsureTables(cacheDir, build, locale)
	return enchants.Load(cacheDir, build, locale)
}

// EquippedEnchantJSON returns the decoded permanent enchant info for an equipped item record,
// or nil if the item has no permanent enchant.
func EquippedEnchantJSON(e *sv.Table, cat *enchants.Catalog) *pyjson.Obj {
	eid := enchants.ParseEnchantID(e)
	if eid <= 0 {
		return nil
	}
	name := fmt.Sprintf("#%d", eid)
	proc := false
	if cat != nil {
		if ench := cat.ByID[eid]; ench != nil {
			if ench.Name != "" {
				name = ench.Name
			}
			proc = ench.IsProc
		}
	}
	obj := pyjson.New("id", eid, "name", name)
	if proc {
		obj.Set("proc", true)
	}
	return obj
}

// WeaponEnchantsJSON returns active temporary weapon enchants (oils, sharpening stones, poisons,
// shaman imbues) from character.weaponEnchants, or nil if none are active.
func WeaponEnchantsJSON(c *sv.Table, cat *enchants.Catalog) []any {
	if c == nil {
		return nil
	}
	we := c.Tab("character").Tab("weaponEnchants")
	if we == nil {
		return nil
	}
	var out []any
	for _, hand := range []string{"mainHand", "offHand"} {
		h := we.Tab(hand)
		if h == nil {
			continue
		}
		id, ok := sv.Num(h.Get("id"))
		if !ok || int(id) <= 0 {
			continue
		}
		eid := int(id)
		name := fmt.Sprintf("#%d", eid)
		if cat != nil {
			if ench := cat.ByID[eid]; ench != nil && ench.Name != "" {
				name = ench.Name
			}
		}
		obj := pyjson.New("hand", hand, "id", eid, "name", name)
		if rem, ok := sv.Num(h.Get("remainingSec")); ok && rem > 0 {
			obj.Set("remainingSec", int64(rem))
		}
		if chg, ok := sv.Num(h.Get("charges")); ok && chg > 0 {
			obj.Set("charges", int64(chg))
		}
		out = append(out, obj)
	}
	return out
}
