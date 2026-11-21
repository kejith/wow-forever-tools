package goldperhour

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
)

// fixture is a WoW folder with nothing but the SavedVariables file of GoldPerHour 0.3.x (the state the addon
// saves at the end of its Lua simulation, see scripts/test_goldperhour.py).
var fixture = filepath.Join("..", "..", "..", "scripts", "fixtures", "goldperhour")

const peacebloom = "|cffffffff|Hitem:2447::::::::20:::::|h[Peacebloom]|h|r"

// plain is a value as JSON sees it, for comparing answers.
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

func TestFromDBLikePython(t *testing.T) {
	for i, c := range fromDBCases {
		got, err := FromDB(parseDB(t, c.lua))
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		var want any
		if err := json.Unmarshal([]byte(c.want), &want); err != nil {
			t.Fatal(err)
		}
		if g := plain(t, got); !reflect.DeepEqual(g, want) {
			data, _ := pyjson.Encode(got)
			t.Errorf("case %d:\n got %s\nwant %s", i, data, c.want)
		}
	}
}

// The simulated evening of the addon's tests: a session with 3 herbs (1000 each) and 5 silver from a mob, two
// /reload, then a request that changes the settings and resets (the session goes to the history), then 2 herbs
// at vendor price in a new session that is running at the last save.
func TestSavedByAddon(t *testing.T) {
	out, err := Compute(fixture)
	if err != nil {
		t.Fatal(err)
	}
	o := plain(t, out).(map[string]any)
	if o["found"] != true || o["legacy"] != false || o["version"] != "0.3.1" {
		t.Errorf("found %v legacy %v version %v", o["found"], o["legacy"], o["version"])
	}
	if !strings.HasSuffix(o["file"].(string), "GoldPerHour.lua") || o["updated"] == nil {
		t.Errorf("file %v updated %v", o["file"], o["updated"])
	}
	want := map[string]any{"scale": 2.0, "tooltipItems": 3.0, "priceSource": "vendor", "resumeAfterReload": true,
		"autoStart": true, "shown": false, "herbSpells": []any{1234.0}}
	if !reflect.DeepEqual(o["settings"], want) {
		t.Errorf("settings %v", o["settings"])
	}
	req := o["lastRequest"].(map[string]any)
	if req["id"] != 8.0 || !strings.Contains(req["error"].(string), "unknown action") {
		t.Errorf("lastRequest %v", req)
	}
	cur := o["current"].(map[string]any)
	if cur["total"] != 20.0 || cur["runningAtSave"] != true || cur["perHour"] != nil {
		t.Errorf("current %v", cur)
	}
	h := o["history"].([]any)[0].(map[string]any)
	if h["total"] != 3500.0 || h["perHour"] != 3500.0/500*3600 ||
		!reflect.DeepEqual(h["zones"], []any{map[string]any{"zone": "The Barrens", "value": 3500.0}}) {
		t.Errorf("history %v", h)
	}
	loot := h["categories"].(map[string]any)["loot"].(map[string]any)
	if loot["itemCount"] != 3.0 || !reflect.DeepEqual(h["chars"], []any{"Tester"}) {
		t.Errorf("loot %v chars %v", loot, h["chars"])
	}
	items := []any{map[string]any{"id": 2447.0, "name": "Peacebloom", "quality": 1.0, "count": 3.0, "value": 3000.0}}
	if !reflect.DeepEqual(loot["items"], items) {
		t.Errorf("items %v", loot["items"])
	}
	samples := h["samples"].([]any)
	var ts []any
	for _, s := range samples {
		ts = append(ts, s.(map[string]any)["t"])
	}
	if !reflect.DeepEqual(ts, []any{0.0, 400.0, 500.0}) || samples[1].(map[string]any)["loot"] != 3500.0 {
		t.Errorf("samples %v", samples)
	}
	sum := o["summary"].(map[string]any)
	if sum["sessions"] != 1.0 || sum["best"] != h["started"] {
		t.Errorf("summary %v", sum)
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
