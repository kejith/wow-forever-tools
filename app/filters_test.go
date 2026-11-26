package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func testAppWithTempData(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	wow := filepath.Join(dir, "wow")
	accSV := filepath.Join(wow, "WTF", "Account", "TEST", "SavedVariables")
	if err := os.MkdirAll(accSV, 0o755); err != nil {
		t.Fatal(err)
	}
	itemsLua := `WoWToolsForever_Items = {
  updated = 1700000000,
  items = {
    [91001] = { id = 91001, name = "Custom Test Robe", quality = 4 },
  },
}
`
	if err := os.WriteFile(filepath.Join(accSV, "WoWToolsForever.lua"), []byte(itemsLua), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{
		userData: filepath.Join(dir, "userdata"),
		root:     filepath.Join(dir, "root"),
	}
	a.settings = map[string]any{
		"wowDir":     wow,
		"sendToGame": true,
	}
	return a
}

func TestItemFiltersPersistenceAndSync(t *testing.T) {
	a := testAppWithTempData(t)
	syncCalls := 0
	a.onFilterSync = func(write bool) {
		if !write {
			t.Error("expected write=true on filter sync")
		}
		syncCalls++
	}

	// Exclude an item from WoWToolsForever_Items and an unknown fallback item.
	f := a.ExcludeItem(91001)
	if !reflect.DeepEqual(f.ExcludedItemIDs, []int{91001}) {
		t.Fatalf("ExcludedItemIDs = %v, want [91001]", f.ExcludedItemIDs)
	}
	if len(f.ExcludedItems) != 1 || f.ExcludedItems[0].Name != "Custom Test Robe" || f.ExcludedItems[0].Quality != 4 {
		t.Fatalf("unexpected resolved item: %+v", f.ExcludedItems)
	}
	if syncCalls != 1 {
		t.Fatalf("syncCalls = %d, want 1", syncCalls)
	}

	// Duplicate exclude is a no-op (does not re-trigger sync).
	a.ExcludeItem(91001)
	if syncCalls != 1 {
		t.Fatalf("duplicate exclude triggered sync: syncCalls=%d", syncCalls)
	}

	// Exclude PvP category.
	f = a.SetCategoryExcluded("pvp", true)
	if !reflect.DeepEqual(f.ExcludedCategories, []string{"pvp"}) {
		t.Fatalf("ExcludedCategories = %v, want [pvp]", f.ExcludedCategories)
	}
	if syncCalls != 2 {
		t.Fatalf("syncCalls = %d, want 2", syncCalls)
	}

	// Reload from settings.json on a fresh App instance (simulating restart).
	b := &App{userData: a.userData, root: a.root}
	b.loadSettings()
	reloaded := b.GetItemFilters()
	if !reflect.DeepEqual(reloaded.ExcludedItemIDs, []int{91001}) {
		t.Fatalf("reloaded ExcludedItemIDs = %v, want [91001]", reloaded.ExcludedItemIDs)
	}
	if !reflect.DeepEqual(reloaded.ExcludedCategories, []string{"pvp"}) {
		t.Fatalf("reloaded ExcludedCategories = %v, want [pvp]", reloaded.ExcludedCategories)
	}

	// Include item & reset.
	a.ExcludeItem(999999)
	f = a.IncludeItem(91001)
	if !reflect.DeepEqual(f.ExcludedItemIDs, []int{999999}) {
		t.Fatalf("after IncludeItem: %v", f.ExcludedItemIDs)
	}
	if f.ExcludedItems[0].Name != "Item #999999" {
		t.Fatalf("fallback name = %q, want Item #999999", f.ExcludedItems[0].Name)
	}

	f = a.ResetExcludedItems()
	if len(f.ExcludedItemIDs) != 0 || len(f.ExcludedItems) != 0 {
		t.Fatalf("after ResetExcludedItems: %+v", f)
	}

	// Disable sendToGame: filter mutation must still persist, without triggering sync.
	before := syncCalls
	a.mu.Lock()
	a.settings["sendToGame"] = false
	a.mu.Unlock()
	f = a.SetCategoryExcluded("pvp", false)
	if len(f.ExcludedCategories) != 0 {
		t.Fatalf("after disabling pvp: %v", f.ExcludedCategories)
	}
	if syncCalls != before {
		t.Fatalf("sync triggered while sendToGame=false: %d -> %d", before, syncCalls)
	}
}

func TestBridgeArgsIncludesExcludeFlags(t *testing.T) {
	args := bridgeArgs("upgrades", map[string]any{
		"char":              "Malgruk",
		"excludeItems":      []int{19514, 12584},
		"excludeCategories": []string{"pvp"},
	}, "/wow", "en", false)
	want := []string{
		"bridge", "upgrades", "--wow", "/wow", "--lang", "en",
		"--char", "Malgruk",
		"--exclude-item", "12584",
		"--exclude-item", "19514",
		"--exclude-category", "pvp",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("bridgeArgs = %v\nwant:        %v", args, want)
	}
}
