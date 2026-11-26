package main

// Item and category filters for upgrade suggestions and in-game lists.
// Stored in settings.json as:
//   - excludedItemIds: []int (item IDs only, consistent across EN/DE clients)
//   - excludedCategories: []string (e.g. ["pvp"])
// Every mutation persists settings.json and refreshes AppData.lua immediately when sendToGame is enabled.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/tools/pricehistory"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

// ExcludedItemInfo is one excluded item resolved for the UI with localized name, quality, and icon FileDataID.
type ExcludedItemInfo struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Quality int    `json:"quality"`
	Icon    any    `json:"icon"`
}

// ItemFilters is what GetItemFilters and the filter mutation methods return to the UI.
type ItemFilters struct {
	ExcludedItemIDs    []int              `json:"excludedItemIds"`
	ExcludedItems      []ExcludedItemInfo `json:"excludedItems"`
	ExcludedCategories []string           `json:"excludedCategories"`
}

func parseIntList(v any) []int {
	var raw []int
	switch xs := v.(type) {
	case []int:
		raw = append(raw, xs...)
	case []int64:
		for _, n := range xs {
			raw = append(raw, int(n))
		}
	case []float64:
		for _, n := range xs {
			raw = append(raw, int(n))
		}
	case []any:
		for _, item := range xs {
			if n, ok := jsNumber(item); ok {
				raw = append(raw, int(n))
			}
		}
	}
	seen := map[int]bool{}
	out := make([]int, 0, len(raw))
	for _, id := range raw {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Ints(out)
	return out
}

func parseCategoryList(v any) []string {
	var raw []string
	switch xs := v.(type) {
	case []string:
		raw = append(raw, xs...)
	case []any:
		for _, item := range xs {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, c := range raw {
		k := strings.ToLower(strings.TrimSpace(c))
		if k != "" && upgrades.IsKnownCategory(k) && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (a *App) excludedItemIDsLocked() []int {
	return parseIntList(a.settings["excludedItemIds"])
}

func (a *App) excludedCategoriesLocked() []string {
	return parseCategoryList(a.settings["excludedCategories"])
}

func (a *App) excludedItemIDs() []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.excludedItemIDsLocked()
}

func (a *App) excludedCategories() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.excludedCategoriesLocked()
}

func (a *App) filterSignature() string {
	ids := a.excludedItemIDs()
	cats := a.excludedCategories()
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",") + "|" + strings.Join(cats, ",")
}

func (a *App) notifyFiltersChanged() {
	if !a.sendsToGame() {
		return
	}
	if a.onFilterSync != nil {
		a.onFilterSync(true)
		return
	}
	if a.ctx != nil {
		a.syncGameData(true)
	}
}

func activeItemLocale(wowDir string) string {
	if language() == "de" {
		return "deDE"
	}
	if loc := clientLocale(wowDir); loc != "" && languageOf(loc) == language() {
		return loc
	}
	return "enUS"
}

func (a *App) resolveExcludedItemDetails(ids []int) []ExcludedItemInfo {
	out := make([]ExcludedItemInfo, len(ids))
	if len(ids) == 0 {
		return out
	}
	wow := a.wowDir()
	cache := a.cacheDir()
	loc := activeItemLocale(wow)
	lang := language()

	// 1. Primary source: cached itemnames-<build>-<locale>.json from ItemSparse.
	names, _ := pricehistory.LoadNames(cache, "", loc)
	// Fallback locale if active locale had no cache file.
	if names == nil || names.Len() == 0 {
		alt := "enUS"
		if loc == "enUS" {
			alt = "deDE"
		}
		names, _ = pricehistory.LoadNames(cache, "", alt)
	}

	// 2. Lazy fallback sources for custom Forever items not yet in itemnames cache.
	var exported map[int64]upgrades.ExportedItemSummary
	var exportedLoaded bool
	var bisCat *upgrades.BiSCatalog
	var bisLoaded bool

	for i, id := range ids {
		info := ExcludedItemInfo{ID: id, Quality: 1}
		if names != nil {
			entry := names.Get(strconv.Itoa(id))
			if s, ok := entry[0].(string); ok && s != "" {
				info.Name = s
			}
			if q, ok := entry[1].(int64); ok && q >= 0 {
				info.Quality = int(q)
			}
		}
		if info.Name == "" && wow != "" {
			if !exportedLoaded {
				exportedLoaded = true
				id64s := make([]int64, len(ids))
				for j, v := range ids {
					id64s[j] = int64(v)
				}
				exported = upgrades.LookupExportedItemNames(wow, id64s)
			}
			if sum, ok := exported[int64(id)]; ok {
				info.Name = sum.Name
				info.Quality = sum.Quality
			}
		}
		if info.Name == "" {
			if !bisLoaded {
				bisLoaded = true
				bisCat = upgrades.LoadBiSCatalog(wow, "", loc)
			}
			if bisCat != nil {
				if bi := bisCat.Items[int64(id)]; bi != nil {
					if n := bi.DisplayName(lang); n != "" {
						info.Name = n
					}
					if bi.Quality >= 0 {
						info.Quality = int(bi.Quality)
					}
				}
			}
		}
		if info.Name == "" {
			info.Name = fmt.Sprintf("Item #%d", id)
		}
		out[i] = info
	}

	// 3. Resolve icon FileDataIDs when running inside the app (avoid spawning bridge subprocess in unit tests).
	if a.ctx != nil {
		anyIDs := make([]any, len(ids))
		for i, id := range ids {
			anyIDs[i] = float64(id)
		}
		if res, ok := a.resolveIcons(anyIDs).(map[string]any); ok {
			if iconMap, ok := res["icons"].(map[string]any); ok {
				for i, id := range ids {
					out[i].Icon = iconMap[strconv.Itoa(id)]
				}
			}
		}
	} else {
		a.icons.mu.Lock()
		for i, id := range ids {
			if a.icons.of != nil {
				out[i].Icon = a.icons.of[int64(id)]
			}
		}
		a.icons.mu.Unlock()
	}

	return out
}

// GetItemFilters returns the active item exclusions (with localized names and icons) and excluded categories.
func (a *App) GetItemFilters() ItemFilters {
	ids := a.excludedItemIDs()
	cats := a.excludedCategories()
	return ItemFilters{
		ExcludedItemIDs:    ids,
		ExcludedItems:      a.resolveExcludedItemDetails(ids),
		ExcludedCategories: cats,
	}
}

// ExcludeItem adds itemID to the persisted exclusion list and refreshes AppData.lua when sendToGame is enabled.
func (a *App) ExcludeItem(itemID int) ItemFilters {
	if itemID <= 0 {
		return a.GetItemFilters()
	}
	a.mu.Lock()
	ids := a.excludedItemIDsLocked()
	changed := true
	for _, id := range ids {
		if id == itemID {
			changed = false
			break
		}
	}
	if changed {
		ids = append(ids, itemID)
		sort.Ints(ids)
		a.settings["excludedItemIds"] = ids
		a.saveSettingsLocked()
	}
	a.mu.Unlock()
	if changed {
		a.notifyFiltersChanged()
	}
	return a.GetItemFilters()
}

// IncludeItem removes itemID from the persisted exclusion list and refreshes AppData.lua when sendToGame is enabled.
func (a *App) IncludeItem(itemID int) ItemFilters {
	if itemID <= 0 {
		return a.GetItemFilters()
	}
	a.mu.Lock()
	ids := a.excludedItemIDsLocked()
	next := make([]int, 0, len(ids))
	changed := false
	for _, id := range ids {
		if id == itemID {
			changed = true
			continue
		}
		next = append(next, id)
	}
	if changed {
		a.settings["excludedItemIds"] = next
		a.saveSettingsLocked()
	}
	a.mu.Unlock()
	if changed {
		a.notifyFiltersChanged()
	}
	return a.GetItemFilters()
}

// ResetExcludedItems clears all explicitly excluded item IDs and refreshes AppData.lua when sendToGame is enabled.
func (a *App) ResetExcludedItems() ItemFilters {
	a.mu.Lock()
	ids := a.excludedItemIDsLocked()
	changed := len(ids) > 0
	if changed {
		a.settings["excludedItemIds"] = []int{}
		a.saveSettingsLocked()
	}
	a.mu.Unlock()
	if changed {
		a.notifyFiltersChanged()
	}
	return a.GetItemFilters()
}

// SetCategoryExcluded enables or disables exclusion of an item category (such as "pvp") and refreshes AppData.lua.
func (a *App) SetCategoryExcluded(category string, excluded bool) ItemFilters {
	cat := strings.ToLower(strings.TrimSpace(category))
	if !upgrades.IsKnownCategory(cat) {
		return a.GetItemFilters()
	}
	a.mu.Lock()
	cats := a.excludedCategoriesLocked()
	exists := false
	for _, c := range cats {
		if c == cat {
			exists = true
			break
		}
	}
	changed := false
	if excluded && !exists {
		cats = append(cats, cat)
		sort.Strings(cats)
		a.settings["excludedCategories"] = cats
		a.saveSettingsLocked()
		changed = true
	} else if !excluded && exists {
		next := make([]string, 0, len(cats)-1)
		for _, c := range cats {
			if c != cat {
				next = append(next, c)
			}
		}
		a.settings["excludedCategories"] = next
		a.saveSettingsLocked()
		changed = true
	}
	a.mu.Unlock()
	if changed {
		a.notifyFiltersChanged()
	}
	return a.GetItemFilters()
}
