package goldperhour

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// fixture is a WoW folder with the SavedVariables file of GoldPerHour 0.5.0 (dumped by tests/test_addon.py).
var fixture = filepath.Join("..", "..", "..", "scripts", "fixtures", "goldperhour")

const peacebloom = "|cffffffff|Hitem:2447::::::::20:::::|h[Peacebloom]|h|r"

func plain(t *testing.T, v any) any {
	t.Helper()
	data, err := pyjson.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func parseDB(t *testing.T, lua string) *sv.Table {
	t.Helper()
	f, err := sv.Parse([]byte(lua))
	if err != nil {
		t.Fatal(err)
	}
	if db := f.Tab("GoldPerHourDB"); db != nil {
		return db
	}
	return sv.NewTable()
}

// Verifies the original legacy/edge cases (rounding, link parsing, sorting, invalid settings) against the v0.5 schema.
func TestLegacyEdgeCases(t *testing.T) {
	// Case 1: under 1 minute, link fallback for id/name/quality, rounding
	got1, err := FromDB(parseDB(t, fromDBCases[0].lua))
	if err != nil {
		t.Fatal(err)
	}
	o1 := plain(t, got1).(map[string]any)
	cur1 := o1["current"].(map[string]any)
	if cur1["elapsed"] != 59.0 || cur1["total"] != 6.0 || cur1["perHour"] != nil {
		t.Fatalf("case 0 current: %v", cur1)
	}
	herbItems := cur1["categories"].(map[string]any)["herb"].(map[string]any)["items"].([]any)
	if len(herbItems) != 1 || herbItems[0].(map[string]any)["id"] != 2589.0 || herbItems[0].(map[string]any)["quality"] != 2.0 {
		t.Fatalf("case 0 herb items: %v", herbItems)
	}

	// Case 2: 60 s exact, settings filtering, herbSpells sorting, sample filtering
	got2, err := FromDB(parseDB(t, fromDBCases[1].lua))
	if err != nil {
		t.Fatal(err)
	}
	o2 := plain(t, got2).(map[string]any)
	if o2["legacy"] != false {
		t.Fatalf("case 1 legacy: %v", o2["legacy"])
	}
	s2 := o2["settings"].(map[string]any)
	if !reflect.DeepEqual(s2["herbSpells"], []any{5.0, 7.0, 12.0}) || s2["scale"] != 1.5 || s2["priceSource"] != "vendor" {
		t.Fatalf("case 1 settings: %v", s2)
	}
	cur2 := o2["current"].(map[string]any)
	if len(cur2["samples"].([]any)) != 2 || len(cur2["zones"].([]any)) != 3 {
		t.Fatalf("case 1 samples/zones: %v %v", cur2["samples"], cur2["zones"])
	}

	// Case 3: history sorting & best session selection
	got3, err := FromDB(parseDB(t, fromDBCases[2].lua))
	if err != nil {
		t.Fatal(err)
	}
	o3 := plain(t, got3).(map[string]any)
	sum3 := o3["summary"].(map[string]any)
	if sum3["sessions"] != 4.0 || sum3["best"] != 20.0 || sum3["total"] != 407.0 {
		t.Fatalf("case 2 summary: %v", sum3)
	}

	// Case 4: empty DB
	got4, err := FromDB(parseDB(t, fromDBCases[3].lua))
	if err != nil {
		t.Fatal(err)
	}
	o4 := plain(t, got4).(map[string]any)
	if o4["found"] != true || o4["legacy"] != true || len(o4["history"].([]any)) != 0 {
		t.Fatalf("case 3 empty: %v", o4)
	}
}

// The simulated evening of the addon's tests: exercises sessions, gathering, dungeon split & roll,
// AH sales/purchases/pending, repairs, quests, multi-char wealth, account bank and alt mail transit.
func TestSavedByAddon(t *testing.T) {
	out, err := Compute(fixture)
	if err != nil {
		t.Fatal(err)
	}
	o := plain(t, out).(map[string]any)
	if o["found"] != true || o["legacy"] != false || o["version"] != "0.5.0" {
		t.Errorf("found %v legacy %v version %v", o["found"], o["legacy"], o["version"])
	}
	if !strings.HasSuffix(o["file"].(string), "GoldPerHour.lua") || o["updated"] == nil {
		t.Errorf("file %v updated %v", o["file"], o["updated"])
	}
	settings := o["settings"].(map[string]any)
	if settings["scale"] != 2.0 || settings["tooltipItems"] != 3.0 || settings["priceSource"] != "vendor" ||
		settings["deductAhCut"] != false || settings["wealthInterval"] != 120.0 ||
		!reflect.DeepEqual(settings["herbSpells"], []any{1234.0}) ||
		!reflect.DeepEqual(settings["miningSpells"], []any{9876.0}) {
		t.Errorf("settings %v", settings)
	}
	req := o["lastRequest"].(map[string]any)
	if req["id"] != 8.0 || !strings.Contains(req["error"].(string), "unknown action") {
		t.Errorf("lastRequest %v", req)
	}
	cur := o["current"].(map[string]any)
	if cur["total"] != 20.0 || cur["runningAtSave"] != true || cur["perHour"] != nil {
		t.Errorf("current %v", cur)
	}

	// History session includes herb + loot + dungeon (shared gold + epic roll)
	h := o["history"].([]any)[0].(map[string]any)
	wantTotal := 1900.0 + 3350.0 + 12500.0 + 95000.0
	if h["total"] != wantTotal {
		t.Errorf("history total %v want %v", h["total"], wantTotal)
	}
	cats := h["categories"].(map[string]any)
	if cats["dungeon"].(map[string]any)["coin"] != 12500.0 || cats["herb"].(map[string]any)["gold"] != 1900.0 {
		t.Errorf("history categories %v", cats)
	}

	// Wealth: Bankalt 399970 + Tester 130000 + accountBank 250000 + transit 50000 = 829970
	w := o["wealth"].(map[string]any)
	if w["total"] != 829970.0 || w["accountBank"] != 250000.0 || w["transitTotal"] != 50000.0 {
		t.Errorf("wealth %v", w)
	}
	if len(w["chars"].([]any)) != 2 || len(w["transit"].([]any)) != 1 || len(w["accountHistory"].([]any)) < 2 {
		t.Errorf("wealth chars/transit/history: %v", w)
	}

	// Auction house & cashflow
	ah := o["auctionHouse"].(map[string]any)
	ahAll := ah["summary"].(map[string]any)["allTime"].(map[string]any)
	if ahAll["sales"] != 1.0 || ahAll["gross"] != 20000.0 || ahAll["cut"] != 1000.0 || ahAll["net"] != 19000.0 || ahAll["buyTotal"] != 4000.0 {
		t.Errorf("ah summary %v", ahAll)
	}
	pending := ah["pending"].(map[string]any)
	if pending["pendingMailCopper"] != 28500.0 || pending["listedCopper"] != 25000.0 || pending["listedCount"] != 1.0 {
		t.Errorf("ah pending %v", pending)
	}
	if len(ah["topItems"].([]any)) != 1 || len(ah["sales"].([]any)) != 1 {
		t.Errorf("ah topItems/sales %v", ah)
	}

	// Runs & Drops
	runs := o["runs"].([]any)
	runStats := o["runStats"].([]any)
	if len(runs) != 1 || len(runStats) != 1 || runStats[0].(map[string]any)["instance"] != "Shadowfang Keep" {
		t.Errorf("runs/runStats %v %v", runs, runStats)
	}
	drops := o["drops"].([]any)
	if len(drops) != 1 || drops[0].(map[string]any)["name"] != "Epic BoE Ring" {
		t.Errorf("drops %v", drops)
	}

	// Daily & periods
	allPeriod := o["periods"].(map[string]any)["allTime"].(map[string]any)
	if allPeriod["income"] != 35500.0 || allPeriod["expense"] != 5530.0 || allPeriod["net"] != 29970.0 {
		t.Errorf("allTime cashflow %v", allPeriod)
	}
}

// Reconstructing account history when accountHistory is absent (step function across chars + account bank).
func TestWealthReconstruction(t *testing.T) {
	db := parseDB(t, `GoldPerHourDB = {
	  updated = 200000,
	  wealth = {
	    chars = {
	      ["A-R"] = { name = "A", realm = "R", class = "WARLOCK", money = 150, updated = 200000, history = { { 100000, 100 }, { 200000, 150 } } },
	      ["B-R"] = { name = "B", realm = "R", class = "MAGE", money = 300, updated = 150000, history = { { 150000, 300 } } },
	    },
	    accountBank = { money = 50, updated = 100000, history = { { 100000, 50 } } },
	  }
	}`)
	out, err := FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	w := plain(t, out).(map[string]any)["wealth"].(map[string]any)
	if w["total"] != 500.0 {
		t.Fatalf("total %v", w["total"])
	}
	hist := w["accountHistory"].([]any)
	// t=100000 -> 100+50=150; t=150000 -> 100+300+50=450; t=200000 -> 150+300+50=500
	want := []any{
		map[string]any{"t": 100000.0, "total": 150.0},
		map[string]any{"t": 150000.0, "total": 450.0},
		map[string]any{"t": 200000.0, "total": 500.0},
	}
	if !reflect.DeepEqual(hist, want) {
		t.Fatalf("reconstructed %v want %v", hist, want)
	}
}

// A 0.2 file: no settings, session or history; items only with a link.
func TestOldSavedVariables(t *testing.T) {
	db := parseDB(t, `GoldPerHourDB = { elapsed = 1800, gold = { herb = 0, loot = 9000 }, coin = { herb = 0, loot = 0 },
  items = { herb = {}, loot = { [2447] = { id = 2447, link = "`+peacebloom+`", count = 9, value = 9000 } } }, herbSpells = {} }`)
	out, err := FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	o := plain(t, out).(map[string]any)
	settings := o["settings"].(map[string]any)
	if o["legacy"] != true || len(o["history"].([]any)) != 0 || settings["shown"] != true || settings["scale"] != 1.0 {
		t.Errorf("legacy %v history %v settings %v", o["legacy"], o["history"], settings)
	}
	cur := o["current"].(map[string]any)
	item := cur["categories"].(map[string]any)["loot"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["name"] != "Peacebloom" || item["quality"] != 1.0 || cur["perHour"] != 18000.0 {
		t.Errorf("item %v perHour %v", item, cur["perHour"])
	}
	missing, err := Compute(filepath.Join("..", "..", "..", "scripts", "does-not-exist"))
	if err != nil || missing.Get("found") != false {
		t.Errorf("missing folder: %v %v", missing, err)
	}
}
