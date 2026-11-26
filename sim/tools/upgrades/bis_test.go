package upgrades

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
)

func TestParseCompactStats(t *testing.T) {
	stats, unique, twoHand, hasProc := parseCompactStats("sp44hi20cr14w61.8s1.9u1t1x1a565")
	if !unique || !twoHand || !hasProc {
		t.Fatalf("flags: unique=%v twoHand=%v hasProc=%v", unique, twoHand, hasProc)
	}
	if stats["ITEM_MOD_SPELL_POWER_SHORT"] != 44 {
		t.Fatalf("sp = %v, want 44", stats["ITEM_MOD_SPELL_POWER_SHORT"])
	}
	if stats["ITEM_MOD_HIT_RATING_SHORT"] != 20 {
		t.Fatalf("hit = %v, want 20", stats["ITEM_MOD_HIT_RATING_SHORT"])
	}
	if stats["ITEM_MOD_CRIT_RATING_SHORT"] != 14 {
		t.Fatalf("crit = %v, want 14", stats["ITEM_MOD_CRIT_RATING_SHORT"])
	}
	if stats["ITEM_MOD_DAMAGE_PER_SECOND_SHORT"] != 61.8 {
		t.Fatalf("dps = %v, want 61.8", stats["ITEM_MOD_DAMAGE_PER_SECOND_SHORT"])
	}
	if stats["RESISTANCE0_NAME"] != 565 {
		t.Fatalf("armor = %v, want 565", stats["RESISTANCE0_NAME"])
	}
}

func TestBundledCatalogLoads(t *testing.T) {
	t.Setenv("WOW_TOOLS_CACHE", t.TempDir())
	cat := LoadBiSCatalog("", "", "enUS")
	if len(cat.Items) < 5000 {
		t.Fatalf("expected >=5000 bundled BiS items, got %d", len(cat.Items))
	}
	// Verify Lionheart Helm (12640) is present as a crafted item with stats.
	lh := cat.Items[12640]
	if lh == nil {
		t.Fatalf("missing Lionheart Helm (12640) in bundled catalog")
	}
	if !hasSourceKind(lh.Sources, SrcCrafted) {
		t.Fatalf("expected Lionheart Helm to have crafted source, got %+v", lh.Sources)
	}
	if lh.RawStats["ITEM_MOD_STRENGTH_SHORT"] != 18 {
		t.Fatalf("Lionheart str = %v, want 18", lh.RawStats["ITEM_MOD_STRENGTH_SHORT"])
	}
}

func TestParseAtlasLootRaidOverlay(t *testing.T) {
	cat := &BiSCatalog{
		PlacesDE: map[string]string{},
		Items:    map[int64]*BiSCatalogItem{},
	}
	sample := `
data["MoltenCore"] = {
	ContentType = RAID40_CONTENT,
	items = {
		{
			name = AL["Ragnaros"],
			[NORMAL_DIFF] = {
				{ 1, 18814 },
				{ 2, 17103 },
			},
		},
	},
}
`
	if !parseAtlasLootFile(cat, sample, false) {
		t.Fatal("expected parseAtlasLootFile to return true")
	}
	it := cat.Items[18814]
	if it == nil || !hasSourceKind(it.Sources, SrcRaid) {
		t.Fatalf("expected item 18814 to have raid source, got %+v", it)
	}
	if it.Sources[0].Label != "Ragnaros, Molten Core" {
		t.Fatalf("label = %q, want %q", it.Sources[0].Label, "Ragnaros, Molten Core")
	}
}

func TestComputeBiSFilteringRankingAndLevelMode(t *testing.T) {
	i18n.SetLanguage("en", "")
	// Synthetic character snapshot: level 45 Horde Warlock with completed quest 1001.
	wow := t.TempDir()
	charDir := filepath.Join(wow, "WTF", "Account", "TEST", "Realm", "Testlock", "SavedVariables")
	if err := os.MkdirAll(charDir, 0o755); err != nil {
		t.Fatal(err)
	}
	svLua := `WoWToolsForever_Char = {
	["character"] = {
		["name"] = "Testlock",
		["class"] = "Warlock",
		["classFile"] = "WARLOCK",
		["faction"] = "Horde",
		["level"] = 45,
		["money"] = 100000,
		["stats"] = { ["intellect"] = 180, ["stamina"] = 150, ["spirit"] = 120 },
	},
	["equipment"] = {
		{
			["slot"] = 1,
			["id"] = 90001,
			["name"] = "Equipped Mid Hat",
			["quality"] = 3,
			["equipLoc"] = "INVTYPE_HEAD",
			["stats"] = { ["ITEM_MOD_SPELL_POWER_SHORT"] = 25, ["ITEM_MOD_INTELLECT_SHORT"] = 10 },
		},
	},
	["completedQuests"] = { [1001] = true },
	["skills"] = {
		["skills"] = {
			{ ["name"] = "Tailoring", ["rank"] = 300, ["max"] = 300 },
		},
	},
}
`
	if err := os.WriteFile(filepath.Join(charDir, "WoWToolsForever.lua"), []byte(svLua), 0o644); err != nil {
		t.Fatal(err)
	}

	// Build a small deterministic synthetic catalog.
	mk := func(id int64, name string, q, ilvl, req, cls, sub, inv int64, faction string, cl []string, st string, srcs []BiSSource) *BiSCatalogItem {
		stats, u, th, hp := parseCompactStats(st)
		return &BiSCatalogItem{
			ID: id, Name: name, Quality: q, ItemLvl: ilvl, ReqLvl: req,
			ClassID: cls, SubID: sub, InvType: inv, Faction: faction, Classes: cl,
			StatStr: st, RawStats: stats, Unique: u, TwoHand: th, HasProc: hp, Sources: srcs,
		}
	}
	cat := &BiSCatalog{
		PlacesDE: map[string]string{},
		Items: map[int64]*BiSCatalogItem{
			// 1. Equipped item (Dungeon)
			90001: mk(90001, "Equipped Mid Hat", 3, 48, 43, 4, 1, 1, "", nil, "sp25in10", []BiSSource{{Kind: SrcDungeon, Label: "Boss A, Zul'Farrak", Place: "Zul'Farrak"}}),
			// 2. Better Quest hat (already completed quest 1001!)
			90002: mk(90002, "Done Quest Hat", 3, 50, 45, 4, 1, 1, "", nil, "sp35in12", []BiSSource{{Kind: SrcQuest, Label: "Old Quest", QuestID: 1001}}),
			// 3. Level 60 Raid hat (only visible in levelMode=60)
			90003: mk(90003, "Endgame Raid Crown", 4, 80, 60, 4, 1, 1, "", nil, "sp60in20", []BiSSource{{Kind: SrcRaid, Label: "Nefarian, Blackwing Lair", Place: "Blackwing Lair"}}),
			// 4. Alliance-only hat (must be filtered out for Horde)
			90004: mk(90004, "Alliance Crown", 4, 50, 45, 4, 1, 1, "alliance", nil, "sp99", []BiSSource{{Kind: SrcQuest, Label: "Alliance Quest", Faction: "alliance"}}),
			// 5. Plate hat (must be filtered out for Warlock)
			90005: mk(90005, "Plate Spell Helm", 4, 50, 45, 4, 4, 1, "", nil, "sp99", []BiSSource{{Kind: SrcDungeon, Label: "Boss B, Uldaman"}}),
			// 6. Mage-only hat (must be filtered out for Warlock)
			90006: mk(90006, "Mage Only Hat", 4, 50, 45, 4, 1, 1, "", []string{"MAGE"}, "sp99", []BiSSource{{Kind: SrcDungeon, Label: "Boss C, Uldaman"}}),
			// 7. Weaker Crafted hat (negative delta vs equipped, must STILL appear in BiS!)
			90007: mk(90007, "Weaker Crafted Hood", 2, 44, 40, 4, 1, 1, "", nil, "sp15in5", []BiSSource{{Kind: SrcCrafted, Label: "Tailoring 250", Skill: 197, SkillRank: 250}}),
		},
		DataSources: []string{"Test"},
	}

	// Case 1: Current level mode, quest+dungeon+raid+crafted.
	res, err := ComputeBiS(wow, "Testlock", nil, BiSOptions{
		LevelMode: "current",
		Preview:   4,
		Top:       10,
		Sources:   []string{SrcQuest, SrcDungeon, SrcRaid, SrcCrafted},
		Engine:    "off",
		Catalog:   cat,
	})
	if err != nil {
		t.Fatalf("ComputeBiS current: %v", err)
	}
	slots := res.Get("slots").([]any)
	if len(slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(slots))
	}
	head := slots[0].(*pyjson.Obj)
	cands := head.Get("candidates").([]any)
	// Should include 90002 (#1), 90001 (#2 equipped), 90007 (#3 weaker crafted).
	// Excluded: 90003 (lvl 60), 90004 (Alliance), 90005 (Plate), 90006 (Mage-only).
	if len(cands) != 3 {
		t.Fatalf("current mode expected 3 candidates, got %d", len(cands))
	}
	c1 := cands[0].(*pyjson.Obj)
	if c1.Get("id").(int64) != 90002 {
		t.Fatalf("rank #1 id = %v, want 90002", c1.Get("id"))
	}
	c1Src := c1.Get("sources").([]any)[0].(*pyjson.Obj)
	if c1Src.Get("questDone") != true {
		t.Fatalf("expected questDone=true on completed quest 1001")
	}
	c3 := cands[2].(*pyjson.Obj)
	if c3.Get("id").(int64) != 90007 || c3.Get("gain").(float64) >= 0 {
		t.Fatalf("expected negative-delta weaker crafted item at #3, got id=%v gain=%v", c3.Get("id"), c3.Get("gain"))
	}
	c3Src := c3.Get("sources").([]any)[0].(*pyjson.Obj)
	if c3Src.Get("canCraft") != true {
		t.Fatalf("expected canCraft=true for Tailoring 250 with rank 300")
	}

	// Case 2: Level 60 mode + Raid-only filter -> only 90003!
	res60, err := ComputeBiS(wow, "Testlock", nil, BiSOptions{
		LevelMode: "60",
		Top:       10,
		Sources:   []string{SrcRaid},
		Engine:    "off",
		Catalog:   cat,
	})
	if err != nil {
		t.Fatalf("ComputeBiS 60 raid: %v", err)
	}
	slots60 := res60.Get("slots").([]any)
	if len(slots60) != 1 {
		t.Fatalf("expected 1 slot in raid-only lvl60 mode, got %d", len(slots60))
	}
	cands60 := slots60[0].(*pyjson.Obj).Get("candidates").([]any)
	if len(cands60) != 1 || cands60[0].(*pyjson.Obj).Get("id").(int64) != 90003 {
		t.Fatalf("expected only raid item 90003, got %d items", len(cands60))
	}
}
