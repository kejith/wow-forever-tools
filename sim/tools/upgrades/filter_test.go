package upgrades

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

func makeSVItem(fields map[string]any, stats map[string]any, tip []string) *sv.Table {
	t := sv.NewTable()
	for k, v := range fields {
		t.Set(k, v)
	}
	if len(stats) > 0 {
		st := sv.NewTable()
		for k, v := range stats {
			st.Set(k, v)
		}
		t.Set("stats", st)
	}
	if len(tip) > 0 {
		lines := sv.NewList()
		for i, l := range tip {
			lines.Set(int64(i+1), l)
		}
		t.Set("tooltip", lines)
	}
	return t
}

func TestIsPvPItemAndCategories(t *testing.T) {
	cls := NewPvPClassifier([]int64{12584}, []int64{9901})

	cases := []struct {
		name string
		id   int64
		it   *sv.Table
		cls  *PvPClassifier
		want bool
	}{
		{
			name: "classifier DB2 PvP ID",
			id:   12584,
			it:   makeSVItem(map[string]any{"name": "Grand Marshal's Longsword"}, nil, nil),
			cls:  cls,
			want: true,
		},
		{
			name: "direct requiredHonorRank",
			id:   90001,
			it:   makeSVItem(map[string]any{"requiredHonorRank": int64(10)}, nil, nil),
			cls:  cls,
			want: true,
		},
		{
			name: "direct requiredPvPRank",
			id:   90002,
			it:   makeSVItem(map[string]any{"requiredPvPRank": int64(7)}, nil, nil),
			cls:  cls,
			want: true,
		},
		{
			name: "direct PvP faction Warsong Outriders",
			id:   90003,
			it:   makeSVItem(map[string]any{"requiredReputationFaction": int64(889)}, nil, nil),
			cls:  cls,
			want: true,
		},
		{
			name: "direct PvP faction Forever Darkspear Raiders",
			id:   90004,
			it:   makeSVItem(map[string]any{"minFactionID": int64(2798)}, nil, nil),
			cls:  cls,
			want: true,
		},
		{
			name: "non-PvP faction Argent Dawn",
			id:   90005,
			it:   makeSVItem(map[string]any{"requiredReputationFaction": int64(529)}, nil, nil),
			cls:  cls,
			want: false,
		},
		{
			name: "PvP extendedCost ID",
			id:   90006,
			it:   makeSVItem(map[string]any{"extendedCost": int64(9901)}, nil, nil),
			cls:  cls,
			want: true,
		},
		{
			name: "Resilience stat",
			id:   90007,
			it:   makeSVItem(nil, map[string]any{"ITEM_MOD_RESILIENCE_RATING_SHORT": int64(15)}, nil),
			cls:  cls,
			want: true,
		},
		{
			name: "English rank tooltip",
			id:   90008,
			it:   makeSVItem(nil, nil, []string{"Binds when picked up", "Requires Knight-Lieutenant", "+20 Stamina"}),
			cls:  cls,
			want: true,
		},
		{
			name: "German rank tooltip",
			id:   90009,
			it:   makeSVItem(nil, nil, []string{"Wird beim Aufheben gebunden", "Benötigt Oberster Kriegsfürst", "+20 Ausdauer"}),
			cls:  cls,
			want: true,
		},
		{
			name: "English BG reputation tooltip",
			id:   90010,
			it:   makeSVItem(nil, nil, []string{"Requires Warsong Outriders - Revered"}),
			cls:  cls,
			want: true,
		},
		{
			name: "German BG reputation tooltip",
			id:   90011,
			it:   makeSVItem(nil, nil, []string{"Benötigt Der Bund von Arathor - Ehrfürchtig"}),
			cls:  cls,
			want: true,
		},
		{
			name: "German resilience tooltip",
			id:   90012,
			it:   makeSVItem(nil, nil, []string{"Anlegen: Erhöht Eure Abhärtungswertung um 12."}),
			cls:  cls,
			want: true,
		},
		{
			name: "Bundled Forever custom Honor item without rank flag",
			id:   272453, // Premier Magus Footwraps
			it:   nil,
			cls:  nil,
			want: true,
		},
		{
			name: "Regular PvE item",
			id:   90099,
			it:   makeSVItem(map[string]any{"name": "Robe of the Archmage"}, map[string]any{"ITEM_MOD_INTELLECT_SHORT": int64(12)}, []string{"Binds when equipped", "+12 Intellect"}),
			cls:  cls,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsPvPItem(tc.id, tc.it, tc.cls)
			if got != tc.want {
				t.Errorf("IsPvPItem(%d) = %v, want %v", tc.id, got, tc.want)
			}
			cats := ItemCategories(tc.id, tc.it, tc.cls)
			if tc.want && (len(cats) != 1 || cats[0] != CategoryPvP) {
				t.Errorf("ItemCategories(%d) = %v, want [%s]", tc.id, cats, CategoryPvP)
			}
			if !tc.want && len(cats) != 0 {
				t.Errorf("ItemCategories(%d) = %v, want empty", tc.id, cats)
			}
		})
	}
}

func TestIsPvPExtendedCost(t *testing.T) {
	if !IsPvPExtendedCost([]int64{1792}, nil, 0, 0) {
		t.Error("Honor Points (1792) must be PvP extended cost")
	}
	if !IsPvPExtendedCost([]int64{3468}, nil, 0, 0) {
		t.Error("Forever Rank Points (3468) must be PvP extended cost")
	}
	if !IsPvPExtendedCost(nil, []int64{20558}, 0, 0) {
		t.Error("WSG Mark of Honor (20558) must be PvP extended cost")
	}
	if !IsPvPExtendedCost(nil, []int64{20559}, 0, 0) {
		t.Error("AB Mark of Honor (20559) must be PvP extended cost")
	}
	if !IsPvPExtendedCost(nil, []int64{20560}, 0, 0) {
		t.Error("AV Mark of Honor (20560) must be PvP extended cost")
	}
	if !IsPvPExtendedCost(nil, nil, 1500, 0) {
		t.Error("ArenaRating > 0 must be PvP extended cost")
	}
	if !IsPvPExtendedCost(nil, nil, 0, 509) {
		t.Error("League of Arathor (509) must be PvP extended cost")
	}
	if IsPvPExtendedCost([]int64{3402}, []int64{2447}, 0, 529) {
		t.Error("Merchant's Favor / Argent Dawn must not be PvP extended cost")
	}
}

func writeFilterFixture(t *testing.T) string {
	t.Helper()
	wow := t.TempDir()
	accSV := filepath.Join(wow, "WTF", "Account", "TEST", "SavedVariables")
	charSV := filepath.Join(wow, "WTF", "Account", "TEST", "Realm", "Tester", "SavedVariables")
	if err := os.MkdirAll(accSV, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(charSV, 0o755); err != nil {
		t.Fatal(err)
	}

	charLua := `WoWToolsForever_Char = {
  client = { locale = "enUS", build = "" },
  character = { name = "Tester", class = "Warlock", classFile = "WARLOCK", level = 60, faction = "Horde", money = 500000 },
  equipment = {
    { slot = 5, id = 1000, name = "Starter Robe", quality = 2, equipLoc = "INVTYPE_CHEST", classID = 4, subclassID = 1,
      stats = { ITEM_MOD_SPELL_POWER_SHORT = 5 }, tooltip = { "Starter Robe", "Chest", "+5 Spell Power" } },
  },
  skills = { skills = {} },
}
`
	itemsLua := `WoWToolsForever_Items = {
  updated = 1700000000,
  items = {
    [91001] = { id = 91001, name = "Warlord's Dreadweave Robe", quality = 4, reqLevel = 60, equipLoc = "INVTYPE_CHEST",
      classID = 4, subclassID = 1,
      stats = { ITEM_MOD_SPELL_POWER_SHORT = 40 },
      tooltip = { "Warlord's Dreadweave Robe", "Requires Warlord", "+40 Spell Power" } },
    [91002] = { id = 91002, name = "Robe of the Void", quality = 4, reqLevel = 60, equipLoc = "INVTYPE_CHEST",
      classID = 4, subclassID = 1,
      stats = { ITEM_MOD_SPELL_POWER_SHORT = 30 },
      tooltip = { "Robe of the Void", "Binds when equipped", "+30 Spell Power" } },
    [91003] = { id = 91003, name = "Robe of Winter Night", quality = 3, reqLevel = 52, equipLoc = "INVTYPE_CHEST",
      classID = 4, subclassID = 1,
      stats = { ITEM_MOD_SPELL_POWER_SHORT = 20 },
      tooltip = { "Robe of Winter Night", "Binds when equipped", "+20 Spell Power" } },
  },
}
`
	if err := os.WriteFile(filepath.Join(charSV, "WoWToolsForever.lua"), []byte(charLua), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(accSV, "WoWToolsForever.lua"), []byte(itemsLua), 0o644); err != nil {
		t.Fatal(err)
	}
	return wow
}

func makeTestPrices(entries map[string]int64) *sv.Table {
	p := sv.NewTable()
	for id, price := range entries {
		a := sv.NewTable()
		a.Set("2000", int64(2))
		e := sv.NewTable()
		e.Set("m", price)
		e.Set("a", a)
		p.Set(id, e)
	}
	return p
}

func firstCandidateAndPackageID(t *testing.T, res *pyjson.Obj) (candID, pkgID int64) {
	t.Helper()
	slots, _ := res.Get("slots").([]any)
	if len(slots) > 0 {
		cands, _ := slots[0].(*pyjson.Obj).Get("candidates").([]any)
		if len(cands) > 0 {
			candID, _ = cands[0].(*pyjson.Obj).Get("id").(int64)
		}
	}
	pkg, _ := res.Get("package").(*pyjson.Obj)
	if pkg != nil {
		items, _ := pkg.Get("items").([]any)
		if len(items) > 0 {
			pkgID, _ = items[0].(*pyjson.Obj).Get("id").(int64)
		}
	}
	return candID, pkgID
}

func TestComputeFilterPromotesNextBestAndPackage(t *testing.T) {
	wow := writeFilterFixture(t)
	prices := makeTestPrices(map[string]int64{
		"91001": 50000,
		"91002": 60000,
		"91003": 40000,
	})
	baseOpts := Options{
		Top:           1,
		Engine:        "off",
		Profile:       "dungeon",
		Sort:          "gain",
		PvPClassifier: NewPvPClassifier(nil, nil),
	}

	// 1. Unfiltered: PvP robe (91001, +40 SP) is #1 and picked in the budget package.
	res, err := Compute(wow, "Tester", prices, baseOpts)
	if err != nil {
		t.Fatalf("Compute unfiltered: %v", err)
	}
	candID, pkgID := firstCandidateAndPackageID(t, res)
	if candID != 91001 || pkgID != 91001 {
		t.Fatalf("unfiltered got cand=%d pkg=%d, want 91001/91001", candID, pkgID)
	}

	// 2. Exclude PvP category: 91001 disappears, 91002 (+30 SP) moves up to #1 and package.
	optsPvP := baseOpts
	optsPvP.Filter = NewFilter(nil, []string{CategoryPvP})
	res, err = Compute(wow, "Tester", prices, optsPvP)
	if err != nil {
		t.Fatalf("Compute pvp filter: %v", err)
	}
	candID, pkgID = firstCandidateAndPackageID(t, res)
	if candID != 91002 || pkgID != 91002 {
		t.Fatalf("exclude pvp got cand=%d pkg=%d, want 91002/91002", candID, pkgID)
	}

	// 3. Exclude PvP + explicit item 91002: 91003 (+20 SP) moves up to #1 and package.
	optsBoth := baseOpts
	optsBoth.Filter = NewFilter([]int64{91002}, []string{CategoryPvP})
	res, err = Compute(wow, "Tester", prices, optsBoth)
	if err != nil {
		t.Fatalf("Compute combined filter: %v", err)
	}
	candID, pkgID = firstCandidateAndPackageID(t, res)
	if candID != 91003 || pkgID != 91003 {
		t.Fatalf("exclude pvp+91002 got cand=%d pkg=%d, want 91003/91003", candID, pkgID)
	}

	// 4. Exclude only explicit item 91001: 91002 moves up.
	optsID := baseOpts
	optsID.Filter = NewFilter([]int64{91001}, nil)
	res, err = Compute(wow, "Tester", prices, optsID)
	if err != nil {
		t.Fatalf("Compute explicit ID filter: %v", err)
	}
	candID, pkgID = firstCandidateAndPackageID(t, res)
	if candID != 91002 || pkgID != 91002 {
		t.Fatalf("exclude 91001 got cand=%d pkg=%d, want 91002/91002", candID, pkgID)
	}
}

func TestComputeBiSFilterExcludesIDAndPvP(t *testing.T) {
	wow := writeFilterFixture(t)
	cat := &BiSCatalog{
		Items: map[int64]*BiSCatalogItem{
			92001: {
				ID: 92001, Name: "Premier PvP Robe", Quality: 4, ItemLvl: 65, ReqLvl: 60,
				ClassID: 4, SubID: 1, InvType: 5,
				RawStats: map[string]float64{"ITEM_MOD_SPELL_POWER_SHORT": 45},
				Sources:  []BiSSource{{Kind: SrcPvP, Label: "PvP Rank 13"}},
			},
			92002: {
				ID: 92002, Name: "Raid Robe", Quality: 4, ItemLvl: 66, ReqLvl: 60,
				ClassID: 4, SubID: 1, InvType: 5,
				RawStats: map[string]float64{"ITEM_MOD_SPELL_POWER_SHORT": 35},
				Sources:  []BiSSource{{Kind: SrcRaid, Label: "Molten Core"}},
			},
			92003: {
				ID: 92003, Name: "Dungeon Robe", Quality: 3, ItemLvl: 60, ReqLvl: 58,
				ClassID: 4, SubID: 1, InvType: 5,
				RawStats: map[string]float64{"ITEM_MOD_SPELL_POWER_SHORT": 25},
				Sources:  []BiSSource{{Kind: SrcDungeon, Label: "Stratholme"}},
			},
		},
	}
	opts := BiSOptions{
		LevelMode:     "60",
		Top:           1,
		Sources:       []string{SrcPvP, SrcRaid, SrcDungeon},
		Engine:        "off",
		Profile:       "dungeon",
		Catalog:       cat,
		PvPClassifier: NewPvPClassifier(nil, nil),
	}

	res, err := ComputeBiS(wow, "Tester", nil, opts)
	if err != nil {
		t.Fatalf("ComputeBiS unfiltered: %v", err)
	}
	candID, pkgID := firstCandidateAndPackageID(t, res)
	if candID != 92001 || pkgID != 92001 {
		t.Fatalf("BiS unfiltered got cand=%d pkg=%d, want 92001/92001", candID, pkgID)
	}

	opts.Filter = NewFilter([]int64{92002}, []string{CategoryPvP})
	res, err = ComputeBiS(wow, "Tester", nil, opts)
	if err != nil {
		t.Fatalf("ComputeBiS filtered: %v", err)
	}
	candID, pkgID = firstCandidateAndPackageID(t, res)
	if candID != 92003 || pkgID != 92003 {
		t.Fatalf("BiS filtered got cand=%d pkg=%d, want 92003/92003", candID, pkgID)
	}
}
