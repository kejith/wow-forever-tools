package bridge

import (
	"os"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/tools/marketsync"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

// orNull is a value as Python's `x or None` would leave it for JSON: nil stays nil.
func orNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// orEmpty is `x or {}`: the value if it is truthy, else an empty object.
func orEmpty(v any) any {
	if sv.Truthy(v) {
		return v
	}
	return pyjson.New()
}

// status is app_bridge.status: the game folder, the characters with a snapshot, Auctionator's scan, the item
// export and the addon's last request.
func status(a *Args) (*pyjson.Obj, error) {
	wow := a.Wow
	info, err := os.Stat(wow)
	found := err == nil && info.IsDir()
	out := pyjson.New("wowDir", wow, "wowFound", found, "characters", []any{}, "auctionator", nil,
		"itemExport", nil, "lastRequest", nil, "clientLocale", orNil(i18n.ClientLocale(wow)),
		"language", i18n.Language())
	if !found {
		return out, nil
	}

	if prices, srcInfo, err := marketsync.LoadPrices(wow); err != nil {
		out.Set("auctionator", pyjson.New("error", err.Error()))
	} else {
		var modified any
		if srcInfo.LocalMtime > 0 {
			modified = srcInfo.LocalMtime
		} else if srcInfo.SyncUpdatedAt > 0 {
			modified = float64(srcInfo.SyncUpdatedAt)
		}
		var scan any
		if day, ok := sv.ScanDate(prices); ok {
			scan = day.Format("2006-01-02")
		}
		au := pyjson.New("file", srcInfo.LocalPath, "modified", modified, "items", prices.Len(),
			"scanDate", scan)
		if srcInfo.Synced {
			au.Set("synced", true)
			au.Set("market", srcInfo.Market.Display())
			au.Set("syncUpdatedAt", srcInfo.SyncUpdatedAt)
		}
		out.Set("auctionator", au)
	}

	var chars []any
	for _, path := range game.FindChars(wow, "") {
		modified, _ := game.Mtime(path)
		c, err := loadChar(path)
		if err != nil { // a half-written file while the game is saving
			chars = append(chars, pyjson.New("key", game.CharKey(path), "error", err.Error(), "modified", modified))
			continue
		}
		ch := c.Tab("character")
		chars = append(chars, pyjson.New(
			"key", game.CharKey(path), "name", ch.Get("name"), "surname", ch.Get("surname"),
			"level", ch.Get("level"), "class", ch.Get("class"), "classFile", ch.Get("classFile"),
			"race", ch.Get("race"), "money", ch.Get("money"), "zone", ch.Get("zone"), "updated", c.Get("updated"),
			"modified", modified, "addonVersion", c.Tab("addon").Get("version"), "client", c.Get("client"),
			"errors", orEmpty(c.Get("errors")), "bankScanned", c.Tab("bank").Get("updated")))
	}
	if chars == nil {
		chars = []any{}
	}
	out.Set("characters", chars)

	for _, path := range game.AccountFiles(wow, "WoWToolsForever.lua") {
		f, err := sv.ReadFile(path)
		if err != nil {
			return nil, err
		}
		items, account := tableOr(f.Get("WoWToolsForever_Items")), tableOr(f.Get("WoWToolsForever_Account"))
		out.Set("itemExport", pyjson.New("updated", items.Get("updated"), "count", lenOf(items.Get("items")),
			"missing", lenOf(items.Get("missing")), "build", items.Get("build"), "lastRun", items.Get("lastRun"),
			"auto", account.Get("autoItems")))
		out.Set("lastRequest", account.Get("lastRequest"))
	}
	if out.Get("itemExport") == nil {
		// Addon 1.0 kept the export per character.
		for _, path := range game.FindChars(wow, "") {
			c, err := loadChar(path)
			if err != nil {
				return nil, err
			}
			info := c.Tab("itemInfo")
			if sv.Truthy(info.Get("items")) {
				out.Set("itemExport", pyjson.New("updated", info.Get("updated"), "count", lenOf(info.Get("items")),
					"missing", lenOf(info.Get("missing")), "legacy", true))
				break
			}
		}
	}
	return out, nil
}

// loadChar is upgrades.load_char.
func loadChar(path string) (*sv.Table, error) { return upgrades.LoadChar(path) }

// tableOr is `x or {}` for a value that should be a table.
func tableOr(v any) *sv.Table {
	if t, ok := v.(*sv.Table); ok {
		return t
	}
	return sv.NewTable()
}

// lenOf is len(x or []) of a table: its number of fields.
func lenOf(v any) int {
	if t, ok := v.(*sv.Table); ok {
		return t.Len()
	}
	if s, ok := v.(string); ok {
		return len([]rune(s))
	}
	return 0
}
