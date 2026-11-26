package enchants

import (
	"os"
	"path/filepath"
	"testing"

	dbench "github.com/kejith/wow-tools-forever/sim/enchants"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

func TestComputeEnchantRecommendations(t *testing.T) {
	i18n.SetLanguage("en", "")
	wow := t.TempDir()
	svDir := filepath.Join(wow, "WTF", "Account", "12345#1", "Realm", "Brakk", "SavedVariables")
	if err := os.MkdirAll(svDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Character wearing a 2H staff enchanted with +30 SP (2504) and an unenchanted cloak (slot 15).
	lua := `WoWToolsForever_Char = {
		updated = 1763400000,
		character = {
			name = "Brakk",
			level = 60,
			class = "Warlock",
			classFile = "WARLOCK",
			stats = {
				intellect = 200,
				stamina = 200,
				spirit = 150,
				healthMax = 3500,
				powerMax = 4500,
				spellDamage = { shadow = 250, fire = 200 },
				spellCrit = { shadow = 10, fire = 10 },
				spellHit = 5,
			},
		},
		equipment = {
			{ slot = 15, id = 18208, name = "Drape of Benediction", quality = 4, ilvl = 65, equipLoc = "INVTYPE_CLOAK", classID = 4, subclassID = 1, stats = { ITEM_MOD_INTELLECT_SHORT = 10 } },
			{ slot = 16, id = 19356, name = "Staff of the Shadow Flame", quality = 4, ilvl = 81, equipLoc = "INVTYPE_2HWEAPON", classID = 2, subclassID = 10, link = "|cffa335ee|Hitem:19356:2504:0:0|h[Staff of the Shadow Flame]|h|r", stats = { ITEM_MOD_SPELL_POWER_SHORT = 84 } },
		},
	}`
	if err := os.WriteFile(filepath.Join(svDir, "WoWToolsForever.lua"), []byte(lua), 0o644); err != nil {
		t.Fatal(err)
	}

	cat := dbench.Empty()
	eSP30 := &dbench.Enchant{ID: 2504, Name: "Spell Power +30", Stats: map[string]float64{"sp": 30}}
	eSP55 := &dbench.Enchant{ID: 8211, Name: "Spell Power +55", Stats: map[string]float64{"sp": 55}}
	eInt9 := &dbench.Enchant{ID: 1904, Name: "Intellect +9", Stats: map[string]float64{"int": 9}}
	eCrus := &dbench.Enchant{ID: 1900, Name: "Crusader", IsProc: true}
	eCloakInt := &dbench.Enchant{ID: 9001, Name: "Intellect +5", Stats: map[string]float64{"int": 5}}
	for _, e := range []*dbench.Enchant{eSP30, eSP55, eInt9, eCrus, eCloakInt} {
		cat.ByID[e.ID] = e
	}
	cat.ItemNames[14344] = "Large Brilliant Shard"
	opts := []*dbench.Option{
		{ID: "spell:22749", EnchantID: 2504, Enchant: eSP30, Source: "enchanting", SpellID: 22749, SpellName: "Enchant Weapon - Spell Power", Slots: []int64{16, 17}, EquipClass: 2, Reagents: []dbench.Reagent{{ItemID: 14344, Count: 4}}},
		{ID: "spell:1248607", EnchantID: 8211, Enchant: eSP55, Source: "enchanting", SpellID: 1248607, SpellName: "Enchant 2H Weapon - Mighty Spell Power", Slots: []int64{16}, EquipClass: 2, TwoHandOnly: true, Reagents: []dbench.Reagent{{ItemID: 14344, Count: 8}}},
		{ID: "spell:20036", EnchantID: 1904, Enchant: eInt9, Source: "enchanting", SpellID: 20036, SpellName: "Enchant 2H Weapon - Major Intellect", Slots: []int64{16}, EquipClass: 2, TwoHandOnly: true, Reagents: []dbench.Reagent{{ItemID: 14344, Count: 2}}},
		{ID: "spell:20034", EnchantID: 1900, Enchant: eCrus, Source: "enchanting", SpellID: 20034, SpellName: "Enchant Weapon - Crusader", Slots: []int64{16, 17}, EquipClass: 2, Reagents: []dbench.Reagent{{ItemID: 14344, Count: 4}}},
		{ID: "spell:99001", EnchantID: 9001, Enchant: eCloakInt, Source: "enchanting", SpellID: 99001, SpellName: "Enchant Cloak - Intellect", Slots: []int64{15}, EquipClass: 4, Reagents: []dbench.Reagent{{ItemID: 14344, Count: 1}}},
	}
	for _, opt := range opts {
		cat.Options = append(cat.Options, opt)
		for _, s := range opt.Slots {
			cat.BySlot[s] = append(cat.BySlot[s], opt)
		}
	}

	prices := sv.NewTable()
	shard := sv.NewTable()
	shard.Set("m", int64(50000)) // 5g each
	a := sv.NewTable()
	a.Set("10", int64(5))
	shard.Set("a", a)
	prices.Set("14344", shard)

	out, err := Compute(wow, "Brakk", Options{Engine: "off", Catalog: cat, Prices: prices})
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	if got := out.Get("enchantedSlots"); got != 1 {
		t.Fatalf("enchantedSlots = %v, want 1", got)
	}
	slots := out.Get("slots").([]any)
	if len(slots) != 2 {
		t.Fatalf("slots count = %d, want 2", len(slots))
	}
	// Slot 16 (Main Hand) should recognize current +30 SP and recommend +55 SP (net +25 SP) as best.
	mh := slots[1].(*pyjson.Obj)
	cur := mh.Get("current").(*pyjson.Obj)
	if cur == nil || cur.Get("id") != 2504 {
		t.Fatalf("main hand current enchant = %v, want 2504", cur)
	}
	cands := mh.Get("candidates").([]any)
	if len(cands) < 2 {
		t.Fatalf("main hand candidates = %d, want >= 2", len(cands))
	}
	top := cands[0].(*pyjson.Obj)
	if top.Get("enchantId") != 8211 || top.Get("best") != true || top.Get("cost") != int64(400000) {
		t.Fatalf("top main hand enchant = %v (best=%v cost=%v)", top.Get("enchantId"), top.Get("best"), top.Get("cost"))
	}
	if g, ok := top.Get("gain").(float64); !ok || g <= 20 || g >= 30 {
		t.Fatalf("net gain for +55 SP over +30 SP = %v, want ~25", top.Get("gain"))
	}
	specials := mh.Get("specials").([]any)
	if len(specials) != 1 || specials[0].(*pyjson.Obj).Get("enchantId") != 1900 {
		t.Fatalf("expected Crusader in specials, got %v", specials)
	}
}
