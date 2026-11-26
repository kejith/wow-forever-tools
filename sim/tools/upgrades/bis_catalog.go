package upgrades

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kejith/wow-tools-forever/sim/data"
	"github.com/kejith/wow-tools-forever/sim/db2"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/internal/tables"
)

// BiS source category keys (canonical English values shared between Go and JS).
const (
	SrcQuest      = "quest"
	SrcDungeon    = "dungeon"
	SrcRaid       = "raid"
	SrcCrafted    = "crafted"
	SrcVendor     = "vendor"
	SrcReputation = "reputation"
	SrcPvP        = "pvp"
	SrcWorld      = "world"
)

// AllSourceCategories is the canonical display order of source filters.
var AllSourceCategories = []string{
	SrcQuest, SrcDungeon, SrcRaid, SrcCrafted, SrcVendor, SrcReputation, SrcPvP, SrcWorld,
}

// DefaultBiSSources are the primary categories enabled by default.
var DefaultBiSSources = []string{SrcQuest, SrcDungeon, SrcRaid, SrcCrafted}

// BiSSource describes one way to obtain an item.
type BiSSource struct {
	Kind      string `json:"k"`
	Label     string `json:"l"`
	Place     string `json:"p,omitempty"`
	QuestID   int64  `json:"qid,omitempty"`
	QuestLvl  int64  `json:"qlv,omitempty"`
	QuestType string `json:"qt,omitempty"`
	Skill     int64  `json:"sk,omitempty"`
	SkillRank int64  `json:"sr,omitempty"`
	Faction   string `json:"f,omitempty"`
}

// BiSCatalogItem is one equippable item in the merged BiS catalog.
type BiSCatalogItem struct {
	ID       int64       `json:"-"`
	Name     string      `json:"n"`
	NameDE   string      `json:"nd,omitempty"`
	Quality  int64       `json:"q"`
	ItemLvl  int64       `json:"i"`
	ReqLvl   int64       `json:"r"`
	ClassID  int64       `json:"c"`
	SubID    int64       `json:"sc"`
	InvType  int64       `json:"inv"`
	Classes  []string    `json:"cl,omitempty"`
	Faction  string      `json:"f,omitempty"`
	Bind     string      `json:"b,omitempty"`
	Unique   bool        `json:"u,omitempty"`
	StatStr  string      `json:"st,omitempty"`
	Sources  []BiSSource `json:"so,omitempty"`
	Procs    []string    `json:"pr,omitempty"`
	HasProc  bool        `json:"-"`
	TwoHand  bool        `json:"-"`
	RawStats map[string]float64 `json:"-"` // populated from StatStr or DB2
}

// BiSCatalog holds all merged equippable items, localized place names, and active data sources.
type BiSCatalog struct {
	Build       string
	PlacesDE    map[string]string
	Items       map[int64]*BiSCatalogItem
	DataSources []string // e.g. ["Bundled", "ForeverMyBis", "AtlasLoot", "DB2"]
}

type bundledCatalogJSON struct {
	Version  int                        `json:"version"`
	Build    string                     `json:"build"`
	PlacesDE map[string]string          `json:"placesDe"`
	Items    map[string]*BiSCatalogItem `json:"items"`
}

var (
	bundledOnce sync.Once
	bundledCat  *bundledCatalogJSON
)

func loadBundledCatalog() *bundledCatalogJSON {
	bundledOnce.Do(func() {
		if len(data.BiSSources) == 0 {
			return
		}
		gz, err := gzip.NewReader(bytes.NewReader(data.BiSSources))
		if err != nil {
			return
		}
		defer gz.Close()
		var c bundledCatalogJSON
		if err := json.NewDecoder(gz).Decode(&c); err == nil {
			bundledCat = &c
		}
	})
	return bundledCat
}

// invTypeToEquipLoc converts DB2 InventoryType integers to WoW INVTYPE_* strings.
var invTypeToEquipLoc = map[int64]string{
	1:  "INVTYPE_HEAD",
	2:  "INVTYPE_NECK",
	3:  "INVTYPE_SHOULDER",
	5:  "INVTYPE_CHEST",
	6:  "INVTYPE_WAIST",
	7:  "INVTYPE_LEGS",
	8:  "INVTYPE_FEET",
	9:  "INVTYPE_WRIST",
	10: "INVTYPE_HAND",
	11: "INVTYPE_FINGER",
	12: "INVTYPE_TRINKET",
	13: "INVTYPE_WEAPON",
	14: "INVTYPE_SHIELD",
	15: "INVTYPE_RANGED",
	16: "INVTYPE_CLOAK",
	17: "INVTYPE_2HWEAPON",
	20: "INVTYPE_ROBE",
	21: "INVTYPE_WEAPONMAINHAND",
	22: "INVTYPE_WEAPONOFFHAND",
	23: "INVTYPE_HOLDABLE",
	25: "INVTYPE_THROWN",
	26: "INVTYPE_RANGEDRIGHT",
	28: "INVTYPE_RELIC",
}

// randPropBucket maps InventoryType to RandPropPoints column index (0..3).
var randPropBucket = map[int64]int{
	1: 0, 5: 0, 7: 0, 17: 0, 20: 0,
	3: 1, 6: 1, 8: 1, 10: 1, 12: 1,
	2: 2, 9: 2, 11: 2, 14: 2, 16: 2, 23: 2,
	13: 3, 15: 3, 21: 3, 22: 3, 25: 3, 26: 3, 28: 3,
}

// DB2 ItemModType (StatModifier_bonusStat_*) -> WoW ITEM_MOD_*_SHORT key used by NormStats / NormItem.
var db2BonusStatKey = map[int]string{
	3:  "ITEM_MOD_AGILITY_SHORT",
	4:  "ITEM_MOD_STRENGTH_SHORT",
	5:  "ITEM_MOD_INTELLECT_SHORT",
	6:  "ITEM_MOD_SPIRIT_SHORT",
	7:  "ITEM_MOD_STAMINA_SHORT",
	12: "ITEM_MOD_DEFENSE_SKILL_RATING_SHORT",
	13: "ITEM_MOD_DODGE_RATING_SHORT",
	14: "ITEM_MOD_PARRY_RATING_SHORT",
	15: "ITEM_MOD_BLOCK_RATING_SHORT",
	31: "ITEM_MOD_HIT_RATING_SHORT",
	32: "ITEM_MOD_CRIT_RATING_SHORT",
	36: "ITEM_MOD_HASTE_RATING_SHORT",
	38: "ITEM_MOD_ATTACK_POWER_SHORT",
	39: "ITEM_MOD_RANGED_ATTACK_POWER_SHORT",
	41: "ITEM_MOD_SPELL_HEALING_DONE_SHORT",
	42: "ITEM_MOD_SPELL_DAMAGE_DONE_SHORT",
	43: "ITEM_MOD_MANA_REGENERATION_SHORT",
	45: "ITEM_MOD_SPELL_POWER_SHORT",
	46: "ITEM_MOD_HEALTH_REGEN_SHORT",
	48: "ITEM_MOD_BLOCK_VALUE_SHORT",
	50: "RESISTANCE0_NAME",
}

// compactStatToItemMod maps ForeverMyBis compact stat codes (Stats.lua) to ITEM_MOD_*_SHORT keys.
var compactStatToItemMod = map[string]string{
	"st":  "ITEM_MOD_STAMINA_SHORT",
	"sr":  "ITEM_MOD_STRENGTH_SHORT",
	"ag":  "ITEM_MOD_AGILITY_SHORT",
	"in":  "ITEM_MOD_INTELLECT_SHORT",
	"si":  "ITEM_MOD_SPIRIT_SHORT",
	"ap":  "ITEM_MOD_ATTACK_POWER_SHORT",
	"rap": "ITEM_MOD_RANGED_ATTACK_POWER_SHORT",
	"fap": "ITEM_MOD_FERAL_ATTACK_POWER_SHORT",
	"sp":  "ITEM_MOD_SPELL_POWER_SHORT",
	"he":  "ITEM_MOD_SPELL_HEALING_DONE_SHORT",
	"sd":  "ITEM_MOD_SPELL_DAMAGE_DONE_SHORT",
	"hd":  "ITEM_MOD_HOLY_DAMAGE_DONE_SHORT",
	"fd":  "ITEM_MOD_FIRE_DAMAGE_DONE_SHORT",
	"nd":  "ITEM_MOD_NATURE_DAMAGE_DONE_SHORT",
	"fzd": "ITEM_MOD_FROST_DAMAGE_DONE_SHORT",
	"shd": "ITEM_MOD_SHADOW_DAMAGE_DONE_SHORT",
	"ad":  "ITEM_MOD_ARCANE_DAMAGE_DONE_SHORT",
	"cr":  "ITEM_MOD_CRIT_RATING_SHORT",
	"hi":  "ITEM_MOD_HIT_RATING_SHORT",
	"ha":  "ITEM_MOD_HASTE_RATING_SHORT",
	"de":  "ITEM_MOD_DEFENSE_SKILL_RATING_SHORT",
	"do":  "ITEM_MOD_DODGE_RATING_SHORT",
	"pa":  "ITEM_MOD_PARRY_RATING_SHORT",
	"bl":  "ITEM_MOD_BLOCK_RATING_SHORT",
	"bv":  "ITEM_MOD_BLOCK_VALUE_SHORT",
	"mp":  "ITEM_MOD_MANA_REGENERATION_SHORT",
	"hp":  "ITEM_MOD_HEALTH_REGEN_SHORT",
	"a":   "RESISTANCE0_NAME",
	"w":   "ITEM_MOD_DAMAGE_PER_SECOND_SHORT",
}

var compactStatRe = regexp.MustCompile(`([a-z]+)(-?\d+(?:\.\d+)?)`)

// parseCompactStats decodes a ForeverMyBis stat string (e.g. "sp44hi20u1") into ITEM_MOD_* stats + flags.
func parseCompactStats(s string) (stats map[string]float64, unique, twoHand, hasProc bool) {
	stats = map[string]float64{}
	if s == "" {
		return
	}
	for _, m := range compactStatRe.FindAllStringSubmatch(s, -1) {
		code := m[1]
		val, err := strconv.ParseFloat(m[2], 64)
		if err != nil || val == 0 {
			continue
		}
		switch code {
		case "u":
			unique = true
		case "t":
			twoHand = true
		case "x":
			hasProc = true
		case "ba":
			stats["RESISTANCE0_NAME"] += val
		default:
			if key, ok := compactStatToItemMod[code]; ok {
				stats[key] += val
			}
		}
	}
	return
}

// skillLineNames are crafting profession names by WoW SkillLine ID.
var skillLineNames = map[int64]string{
	164: "Blacksmithing",
	165: "Leatherworking",
	171: "Alchemy",
	182: "Herbalism",
	185: "Cooking",
	197: "Tailoring",
	202: "Engineering",
	333: "Enchanting",
}

// dungeonPlaces are non-raid instances in Classic and WoW: Forever.
var dungeonPlaces = map[string]bool{
	"Ragefire Chasm": true, "Wailing Caverns": true,
	"The Deadmines": true, "Deadmines": true,
	"Shadowfang Keep": true, "Blackfathom Deeps": true,
	"The Stockade": true, "Stormwind Stockade": true,
	"Gnomeregan": true, "Razorfen Kraul": true,
	"Scarlet Monastery": true, "Scarlet Monastery Graveyard": true,
	"Scarlet Monastery Library": true, "Scarlet Monastery Armory": true,
	"Scarlet Monastery Cathedral": true,
	"Razorfen Downs": true, "Uldaman": true, "Zul'Farrak": true, "Maraudon": true,
	"The Temple of Atal'Hakkar": true, "Sunken Temple": true,
	"Blackrock Depths": true,
	"Blackrock Spire": true, "Lower Blackrock Spire": true, "Upper Blackrock Spire": true,
	"Dire Maul": true, "Dire Maul East": true, "Dire Maul West": true, "Dire Maul North": true,
	"Scholomance": true,
	"Stratholme": true, "Stratholme Live": true, "Stratholme Undead": true,
	// Forever new dungeons
	"Ruins of Lordaeron": true, "Excavation Site: Wetlands": true,
	"The Hall of Thanes": true, "The Drowned City": true, "City of Dalaran": true,
	"Krol'dok Stronghold": true, "Alcaz Prison": true,
	"The Shaper's Terrace": true, "Shaper's Terrace": true, "Blackmaw Hold": true,
}

// raidPlaces are Classic and WoW: Forever raid instances and world bosses.
var raidPlaces = map[string]bool{
	"Molten Core": true, "Onyxia's Lair": true, "Onyxia": true,
	"Blackwing Lair": true, "Zul'Gurub": true,
	"Ruins of Ahn'Qiraj": true, "The Ruins of Ahn'Qiraj": true,
	"Temple of Ahn'Qiraj": true, "The Temple of Ahn'Qiraj": true,
	"Ahn'Qiraj": true, "Ahn'Qiraj Temple": true,
	"Naxxramas": true, "World Bosses": true,
	"Mount Hyjal": true, "Barrow Deeps": true,
}

var bisCatCache struct {
	mu      sync.Mutex
	key     string
	mtime   int64
	catalog *BiSCatalog
}

// LoadBiSCatalog builds (or returns a cached) merged BiS catalog from:
//  1. Bundled CC BY-SA 4.0 fallback catalog (sim/data/bis_sources.json.gz)
//  2. Live local ForeverMyBis addon data (if installed in <wow>/Interface/AddOns/ForeverMyBis)
//  3. Live local AtlasLootContinued raid & dungeon tables (if installed in <wow>/Interface/AddOns/AtlasLootContinued_DungeonsAndRaids)
//  4. DB2 tables (Item, ItemSparse, RandPropPoints, ItemDamage*, ItemEffect, SpellEffect, SkillLineAbility)
func LoadBiSCatalog(wow, build, locale string) *BiSCatalog {
	if locale == "" {
		locale = "enUS"
	}
	mtime := addonMtime(wow)
	cacheKey := wow + "|" + build + "|" + locale
	bisCatCache.mu.Lock()
	if bisCatCache.catalog != nil && bisCatCache.key == cacheKey && bisCatCache.mtime == mtime {
		cat := bisCatCache.catalog
		bisCatCache.mu.Unlock()
		return cat
	}
	bisCatCache.mu.Unlock()

	cat := &BiSCatalog{
		Build:    build,
		PlacesDE: map[string]string{},
		Items:    map[int64]*BiSCatalogItem{},
	}

	// Layer 1: Bundled CC BY-SA 4.0 catalog (works out of the box with zero third-party addons).
	if bc := loadBundledCatalog(); bc != nil {
		if cat.Build == "" {
			cat.Build = bc.Build
		}
		for k, v := range bc.PlacesDE {
			cat.PlacesDE[k] = v
		}
		for idStr, src := range bc.Items {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || id <= 0 || src == nil {
				continue
			}
			cp := *src
			cp.ID = id
			cp.Sources = append([]BiSSource(nil), src.Sources...)
			cp.Classes = append([]string(nil), src.Classes...)
			cp.Procs = append([]string(nil), src.Procs...)
			stats, u, th, hp := parseCompactStats(cp.StatStr)
			cp.RawStats = stats
			cp.Unique = cp.Unique || u
			cp.TwoHand = th || cp.InvType == 17
			cp.HasProc = hp || len(cp.Procs) > 0
			cat.Items[id] = &cp
		}
		cat.DataSources = append(cat.DataSources, "Bundled")
	}

	// Layer 2: Live ForeverMyBis addon (if installed locally, keeps items/sources/stats up-to-date).
	if wow != "" && mergeLiveForeverMyBis(cat, wow, locale) {
		cat.DataSources = append(cat.DataSources, "ForeverMyBis")
	}

	// Layer 3: Live AtlasLootContinued (if installed locally, adds Classic & Forever Raid loot + dungeons).
	if wow != "" && mergeLiveAtlasLoot(cat, wow) {
		cat.DataSources = append(cat.DataSources, "AtlasLoot")
	}

	// Layer 4: DB2 enrichment (authoritative Item class/subclass/invType, ItemSparse + RandPropPoints stats
	// for raid items missing from Stats.lua, localized names, and DB2 SkillLineAbility crafted detection).
	if (wow != "" || build != "") && enrichCatalogFromDB2(cat, build, locale) {
		cat.DataSources = append(cat.DataSources, "DB2")
	}

	bisCatCache.mu.Lock()
	bisCatCache.key = cacheKey
	bisCatCache.mtime = mtime
	bisCatCache.catalog = cat
	bisCatCache.mu.Unlock()
	return cat
}

func addonMtime(wow string) int64 {
	if wow == "" {
		return 0
	}
	var sum int64
	paths := []string{
		filepath.Join(wow, "Interface", "AddOns", "ForeverMyBis", "Data", "Items.lua"),
		filepath.Join(wow, "Interface", "AddOns", "ForeverMyBis", "Data", "Stats.lua"),
		filepath.Join(wow, "Interface", "AddOns", "AtlasLootContinued_DungeonsAndRaids", "data.lua"),
		filepath.Join(wow, "Interface", "AddOns", "AtlasLootContinued_DungeonsAndRaids", "data-forever.lua"),
	}
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil {
			sum ^= st.ModTime().UnixNano()
		}
	}
	return sum
}

// ---------- Live ForeverMyBis parser ----------

func parseLuaTableLiteral(s string) (*sv.Table, error) {
	f, err := sv.Parse([]byte("t = " + s))
	if err != nil {
		return nil, err
	}
	return f.Tab("t"), nil
}

func parseLuaMergeBlocks(raw []byte, prefix string) []*sv.Table {
	var out []*sv.Table
	s := string(raw)
	pos := 0
	for {
		idx := strings.Index(s[pos:], prefix)
		if idx < 0 {
			break
		}
		start := pos + idx + len(prefix)
		brace := strings.IndexByte(s[start:], '{')
		if brace < 0 {
			break
		}
		tStart := start + brace
		tEnd := findMatchingBrace(s, tStart)
		if tEnd <= tStart {
			break
		}
		if tab, err := parseLuaTableLiteral(s[tStart:tEnd]); err == nil && tab != nil {
			out = append(out, tab)
		}
		pos = tEnd
	}
	return out
}

func parseLuaNamedTable(s, name string) *sv.Table {
	needle := name + " = (function() return "
	idx := strings.Index(s, needle)
	if idx < 0 {
		return nil
	}
	start := idx + len(needle)
	brace := strings.IndexByte(s[start:], '{')
	if brace < 0 {
		return nil
	}
	tStart := start + brace
	tEnd := findMatchingBrace(s, tStart)
	if tEnd <= tStart {
		return nil
	}
	tab, err := parseLuaTableLiteral(s[tStart:tEnd])
	if err != nil {
		return nil
	}
	return tab
}

func findMatchingBrace(s string, start int) int {
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

var fmbClassMap = map[string]string{
	"warrior": "WARRIOR", "paladin": "PALADIN", "hunter": "HUNTER",
	"rogue": "ROGUE", "priest": "PRIEST", "shaman": "SHAMAN",
	"mage": "MAGE", "warlock": "WARLOCK", "druid": "DRUID",
}

var fmbSlotInv = map[string]int64{
	"head": 1, "neck": 2, "shoulder": 3, "chest": 5, "waist": 6, "legs": 7,
	"feet": 8, "wrist": 9, "hands": 10, "finger": 11, "trinket": 12,
	"one-hand": 13, "shield": 14, "ranged": 15, "back": 16, "two-hand": 17,
	"main-hand": 21, "off-hand": 22, "held-in-off-hand": 23, "thrown": 25, "wand": 26, "relic": 28,
}

func mergeLiveForeverMyBis(cat *BiSCatalog, wow, locale string) bool {
	dir := filepath.Join(wow, "Interface", "AddOns", "ForeverMyBis", "Data")
	itemsRaw, err := os.ReadFile(filepath.Join(dir, "Items.lua"))
	if err != nil {
		return false
	}
	itemsStr := string(itemsRaw)

	// Parse lookup tables at bottom of Items.lua.
	npcs := parseLuaNamedTable(itemsStr, "ns.npcs")
	quests := parseLuaNamedTable(itemsStr, "ns.quests")
	placesTab := parseLuaNamedTable(itemsStr, "ns.places")
	placeMaps := parseLuaNamedTable(itemsStr, "ns.placeMaps")
	skillsTab := parseLuaNamedTable(itemsStr, "ns.skills")
	factionsTab := parseLuaNamedTable(itemsStr, "ns.factions")

	var places []string
	if placesTab != nil {
		for _, v := range placesTab.List() {
			if s, ok := v.(string); ok {
				places = append(places, s)
			}
		}
	}

	// Parse Stats.lua (if present).
	liveStats := map[int64]string{}
	if statsRaw, err := os.ReadFile(filepath.Join(dir, "Stats.lua")); err == nil {
		for _, blk := range parseLuaMergeBlocks(statsRaw, "merge(") {
			for i, k := range blk.Keys() {
				if id, ok := intKey(k); ok && id > 0 {
					if s, ok := blk.Values()[i].(string); ok {
						liveStats[id] = s
					}
				}
			}
		}
	}

	// Parse Procs.lua (if present).
	liveProcs := map[int64][]string{}
	if procsRaw, err := os.ReadFile(filepath.Join(dir, "Procs.lua")); err == nil {
		for _, blk := range parseLuaMergeBlocks(procsRaw, "merge(") {
			for i, k := range blk.Keys() {
				id, ok := intKey(k)
				if !ok || id <= 0 {
					continue
				}
				arr, ok := blk.Values()[i].(*sv.Table)
				if !ok {
					continue
				}
				var list []string
				for _, pt := range arr.Tables() {
					n := pt.Str("n")
					if n == "" {
						continue
					}
					if r := pt.Str("r"); r != "" {
						list = append(list, fmt.Sprintf("%s (%s)", n, r))
					} else {
						list = append(list, n)
					}
				}
				if len(list) > 0 {
					liveProcs[id] = list
				}
			}
		}
	}

	// Parse Names_deDE.lua when present (for German names & dungeon names).
	liveDE := map[int64]string{}
	if deRaw, err := os.ReadFile(filepath.Join(dir, "Names_deDE.lua")); err == nil {
		deStr := string(deRaw)
		if iIdx := strings.Index(deStr, "N.i = {}"); iIdx >= 0 {
			endIdx := strings.Index(deStr[iIdx:], "N.s = {}")
			seg := deStr[iIdx:]
			if endIdx > 0 {
				seg = deStr[iIdx : iIdx+endIdx]
			}
			for _, blk := range parseLuaMergeBlocks([]byte(seg), "merge(N.i,") {
				for i, k := range blk.Keys() {
					if id, ok := intKey(k); ok && id > 0 {
						if s, ok := blk.Values()[i].(string); ok {
							liveDE[id] = s
						}
					}
				}
			}
		}
		if dIdx := strings.Index(deStr, "N.d = {}"); dIdx >= 0 {
			endIdx := strings.Index(deStr[dIdx:], "N.e = {}")
			seg := deStr[dIdx:]
			if endIdx > 0 {
				seg = deStr[dIdx : dIdx+endIdx]
			}
			for _, blk := range parseLuaMergeBlocks([]byte(seg), "merge(N.d,") {
				for i, k := range blk.Keys() {
					ks, ok1 := k.(string)
					vs, ok2 := blk.Values()[i].(string)
					if ok1 && ok2 {
						cat.PlacesDE[ks] = vs
					}
				}
			}
		}
	}

	// Parse Items.lua merge blocks.
	parsed := 0
	for _, blk := range parseLuaMergeBlocks(itemsRaw, "merge(") {
		for i, k := range blk.Keys() {
			id, ok := intKey(k)
			if !ok || id <= 0 {
				continue
			}
			it, ok := blk.Values()[i].(*sv.Table)
			if !ok || sv.Truthy(it.Get("bi")) || sv.Truthy(it.Get("u")) {
				continue
			}
			q, _ := it.Num("q")
			if q < 2 {
				continue
			}
			sources := fmbSources(it, npcs, quests, places, placeMaps, skillsTab, factionsTab)
			if len(sources) == 0 {
				continue
			}
			existing := cat.Items[id]
			if existing == nil {
				existing = &BiSCatalogItem{ID: id}
				cat.Items[id] = existing
			}
			if n := it.Str("n"); n != "" {
				existing.Name = n
			}
			if dn := liveDE[id]; dn != "" {
				existing.NameDE = dn
			}
			existing.Quality = int64(q)
			if il, ok := it.Num("i"); ok && il > 0 {
				existing.ItemLvl = int64(il)
			}
			if rl, ok := it.Num("r"); ok && rl > 0 {
				existing.ReqLvl = int64(rl)
			}
			if existing.InvType == 0 {
				if inv, ok := fmbSlotInv[it.Str("s")]; ok {
					existing.InvType = inv
				}
			}
			if b := it.Str("b"); b != "" {
				existing.Bind = b
			}
			if f := it.Str("f"); f == "alliance" || f == "horde" {
				existing.Faction = f
			}
			if clTab := it.Tab("cl"); clTab.Len() > 0 {
				var cls []string
				for _, v := range clTab.List() {
					if cs, ok := v.(string); ok {
						if mapped, ok := fmbClassMap[strings.ToLower(cs)]; ok {
							cls = append(cls, mapped)
						}
					}
				}
				if len(cls) > 0 {
					existing.Classes = cls
				}
			}
			existing.Sources = mergeBiSSources(existing.Sources, sources)
			if stStr := liveStats[id]; stStr != "" {
				existing.StatStr = stStr
				stats, u, th, hp := parseCompactStats(stStr)
				existing.RawStats = stats
				existing.Unique = existing.Unique || u
				existing.TwoHand = th || existing.InvType == 17
				existing.HasProc = existing.HasProc || hp
			}
			if pr := liveProcs[id]; len(pr) > 0 {
				existing.Procs = pr
				existing.HasProc = true
			}
			parsed++
		}
	}
	// Also apply live Stats.lua to any catalog item not in Items.lua (e.g. raid items).
	for id, stStr := range liveStats {
		if it := cat.Items[id]; it != nil && len(it.RawStats) == 0 {
			it.StatStr = stStr
			stats, u, th, hp := parseCompactStats(stStr)
			it.RawStats = stats
			it.Unique = it.Unique || u
			it.TwoHand = th || it.InvType == 17
			it.HasProc = it.HasProc || hp
		}
	}
	return parsed > 0
}

func fmbLookup(tab *sv.Table, key any) string {
	if tab == nil || key == nil {
		return ""
	}
	if s, ok := key.(string); ok {
		return s
	}
	if id, ok := intKey(key); ok {
		if s, ok := tab.Get(id).(string); ok {
			return s
		}
	}
	return ""
}

func fmbSources(it, npcs, quests *sv.Table, places []string, placeMaps, skillsTab, factionsTab *sv.Table) []BiSSource {
	var out []BiSSource
	dzSet := map[string]bool{}
	var dzFirst string
	for _, v := range it.Tab("dz").List() {
		if s, ok := v.(string); ok && s != "" {
			dzSet[s] = true
			if dzFirst == "" {
				dzFirst = s
			}
		}
	}
	for _, rec := range it.Tab("so").Tables() {
		k := rec.Str("k")
		rf := ""
		switch rec.Str("f") {
		case "a":
			rf = "alliance"
		case "h":
			rf = "horde"
		}
		switch k {
		case "d":
			wVal := rec.Get("w")
			wIdx, hasWIdx := intKey(wVal)
			place := ""
			if hasWIdx && wIdx >= 1 && int(wIdx) <= len(places) {
				place = places[wIdx-1]
			} else if s, ok := wVal.(string); ok {
				place = s
			}
			who := fmbLookup(npcs, rec.Get("n"))
			if who == "World Drop" && place == "" {
				out = append(out, BiSSource{Kind: SrcWorld, Label: "World Drop", Faction: rf})
				continue
			}
			cat := SrcWorld
			switch {
			case raidPlaces[place]:
				cat = SrcRaid
			case dungeonPlaces[place]:
				cat = SrcDungeon
			case len(dzSet) > 0:
				cat = SrcDungeon
				if place == "" {
					place = dzFirst
				}
			case hasWIdx && place != "" && (placeMaps == nil || placeMaps.Get(wIdx) == nil):
				cat = SrcDungeon
			}
			label := "Drop"
			if who != "" && place != "" {
				label = who + ", " + place
			} else if who != "" {
				label = who
			} else if place != "" {
				label = place
			}
			out = append(out, BiSSource{Kind: cat, Label: label, Place: place, Faction: rf})
		case "q":
			qVal := rec.Get("q")
			qid, _ := intKey(qVal)
			qtitle := fmbLookup(quests, qVal)
			if qtitle == "" {
				qtitle = "Quest"
			}
			qt := rec.Str("t")
			cat := SrcQuest
			if qt == "r" {
				cat = SrcRaid
			}
			qlv, _ := rec.Num("lv")
			out = append(out, BiSSource{Kind: cat, Label: qtitle, QuestID: qid, QuestLvl: int64(qlv), QuestType: qt, Faction: rf})
		case "c":
			sk, _ := rec.Num("sk")
			sr, _ := rec.Num("r")
			sname := fmbLookup(skillsTab, int64(sk))
			if sname == "" {
				sname = skillLineNames[int64(sk)]
			}
			if sname == "" {
				sname = "Crafted"
			}
			label := sname
			if sr > 0 {
				label = fmt.Sprintf("%s %d", sname, int64(sr))
			}
			out = append(out, BiSSource{Kind: SrcCrafted, Label: label, Skill: int64(sk), SkillRank: int64(sr)})
		case "v":
			who := fmbLookup(npcs, rec.Get("n"))
			if who == "" {
				who = "Vendor"
			}
			place := ""
			if wIdx, ok := intKey(rec.Get("w")); ok && wIdx >= 1 && int(wIdx) <= len(places) {
				place = places[wIdx-1]
			}
			label := who
			if place != "" {
				label = who + ", " + place
			}
			out = append(out, BiSSource{Kind: SrcVendor, Label: label, Place: place, Faction: rf})
		case "r":
			label := "Reputation"
			if rep := it.Tab("rep"); rep.Len() > 0 {
				if fn := fmbLookup(factionsTab, rep.Get("f")); fn != "" {
					label = fn
				} else if n := rep.Str("n"); n != "" {
					label = n
				}
			}
			out = append(out, BiSSource{Kind: SrcReputation, Label: label, Faction: rf})
		case "h", "hm":
			label := "PvP / Honor"
			if rk, ok := rec.Num("rk"); ok && rk > 0 {
				label = fmt.Sprintf("PvP Rank %d", int64(rk))
			}
			out = append(out, BiSSource{Kind: SrcPvP, Label: label, Faction: rf})
		case "w":
			out = append(out, BiSSource{Kind: SrcWorld, Label: "World Drop"})
		}
	}
	return dedupeBiSSources(out)
}

func dedupeBiSSources(in []BiSSource) []BiSSource {
	seen := map[string]bool{}
	var out []BiSSource
	for _, s := range in {
		key := fmt.Sprintf("%s|%s|%s|%d", s.Kind, s.Label, s.Faction, s.QuestID)
		if !seen[key] {
			seen[key] = true
			out = append(out, s)
		}
	}
	return out
}

func mergeBiSSources(base, extra []BiSSource) []BiSSource {
	return dedupeBiSSources(append(append([]BiSSource(nil), base...), extra...))
}

// ---------- Live AtlasLootContinued parser (runtime-only, never bundled) ----------

var (
	alDataStartRe = regexp.MustCompile(`(?m)^data\["([^"]+)"\]\s*=\s*\{`)
	alContentRe   = regexp.MustCompile(`ContentType\s*=\s*([A-Z0-9_]+)`)
	alBossNameRe  = regexp.MustCompile(`name\s*=\s*(?:AL\["([^"]+)"\]|"([^"]+)")`)
	alItemRowRe   = regexp.MustCompile(`\{\s*\d+\s*,\s*(\d{3,6})\b`)
)

// atlasInstanceNames maps AtlasLoot table keys to clean English instance names.
var atlasInstanceNames = map[string]string{
	"MoltenCore":         "Molten Core",
	"Onyxia":             "Onyxia's Lair",
	"OnyxiasLair":        "Onyxia's Lair",
	"BlackwingLair":      "Blackwing Lair",
	"Zul'Gurub":          "Zul'Gurub",
	"TheRuinsofAhnQiraj": "Ruins of Ahn'Qiraj",
	"TheTempleofAhnQiraj": "Temple of Ahn'Qiraj",
	"Naxxramas":          "Naxxramas",
	"WorldBosses":        "World Bosses",
	"MountHyjal":         "Mount Hyjal",
	"BarrowDeeps":        "Barrow Deeps",
	// Forever dungeons
	"TheHallOfThanes":        "The Hall of Thanes",
	"RuinsOfLordaeron":       "Ruins of Lordaeron",
	"ExcavationSiteWetlands": "Excavation Site: Wetlands",
	"CityOfDalaran":          "City of Dalaran",
	"TheDrownedCity":         "The Drowned City",
	"KroldokStronghold":      "Krol'dok Stronghold",
	"AlcazPrison":            "Alcaz Prison",
	"BlackmawHold":           "Blackmaw Hold",
	"ShapersTerrace":         "The Shaper's Terrace",
}

func mergeLiveAtlasLoot(cat *BiSCatalog, wow string) bool {
	dir := filepath.Join(wow, "Interface", "AddOns", "AtlasLootContinued_DungeonsAndRaids")
	loaded := false
	for _, fname := range []string{"data.lua", "data-forever.lua"} {
		raw, err := os.ReadFile(filepath.Join(dir, fname))
		if err != nil {
			continue
		}
		if parseAtlasLootFile(cat, string(raw), fname == "data-forever.lua") {
			loaded = true
		}
	}
	return loaded
}

func parseAtlasLootFile(cat *BiSCatalog, txt string, foreverFile bool) bool {
	locs := alDataStartRe.FindAllStringSubmatchIndex(txt, -1)
	if len(locs) == 0 {
		return false
	}
	added := 0
	for i, loc := range locs {
		key := txt[loc[2]:loc[3]]
		end := len(txt)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		block := txt[loc[0]:end]
		header := block
		if len(header) > 600 {
			header = header[:600]
		}
		isRaid := false
		if m := alContentRe.FindStringSubmatch(header); m != nil {
			ct := m[1]
			isRaid = strings.Contains(ct, "RAID")
		}
		if !isRaid && !foreverFile {
			// From Classic data.lua, only import Raid tables (dungeons are already in ForeverMyBis/bundled).
			continue
		}
		instName := atlasInstanceNames[key]
		if instName == "" {
			instName = strings.TrimSuffix(key, "Forever")
		}
		srcKind := SrcDungeon
		if isRaid || raidPlaces[instName] {
			srcKind = SrcRaid
		}
		// Walk line by line inside items = { ... } tracking current boss name.
		itemsIdx := strings.Index(block, "items = {")
		if itemsIdx < 0 {
			itemsIdx = strings.Index(block, "items={")
		}
		if itemsIdx < 0 {
			continue
		}
		boss := ""
		for _, line := range strings.Split(block[itemsIdx:], "\n") {
			if bm := alBossNameRe.FindStringSubmatch(line); bm != nil {
				if bm[1] != "" {
					boss = bm[1]
				} else if bm[2] != "" {
					boss = bm[2]
				}
				continue
			}
			for _, im := range alItemRowRe.FindAllStringSubmatch(line, -1) {
				iid, err := strconv.ParseInt(im[1], 10, 64)
				if err != nil || iid <= 0 {
					continue
				}
				label := instName
				if boss != "" && boss != instName {
					label = boss + ", " + instName
				}
				it := cat.Items[iid]
				if it == nil {
					it = &BiSCatalogItem{ID: iid}
					cat.Items[iid] = it
				}
				// Don't duplicate dungeon sources if ForeverMyBis already gave a dungeon boss source.
				if srcKind == SrcDungeon && hasSourceKind(it.Sources, SrcDungeon) {
					continue
				}
				it.Sources = mergeBiSSources(it.Sources, []BiSSource{{
					Kind:  srcKind,
					Label: label,
					Place: instName,
				}})
				added++
			}
		}
	}
	return added > 0
}

func hasSourceKind(srcs []BiSSource, kind string) bool {
	for _, s := range srcs {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

// ---------- DB2 Enrichment (Item, ItemSparse, RandPropPoints, ItemDamage*, ItemEffect, SpellEffect, SkillLineAbility) ----------

// classMaskToFiles decodes WoW AllowableClass bitmask into classFile strings.
var classBitFiles = []struct {
	bit  int
	file string
}{
	{1 << 0, "WARRIOR"},
	{1 << 1, "PALADIN"},
	{1 << 2, "HUNTER"},
	{1 << 3, "ROGUE"},
	{1 << 4, "PRIEST"},
	{1 << 6, "SHAMAN"},
	{1 << 7, "MAGE"},
	{1 << 8, "WARLOCK"},
	{1 << 10, "DRUID"},
}

func decodeClassMask(mask int) []string {
	if mask <= 0 {
		return nil
	}
	var out []string
	for _, cb := range classBitFiles {
		if mask&cb.bit != 0 {
			out = append(out, cb.file)
		}
	}
	// If all 9 Classic classes are set, treat as unrestricted.
	if len(out) >= 9 {
		return nil
	}
	return out
}

// Classic Alliance/Horde race masks (Human=1, Dwarf=4, NightElf=8, Gnome=64; Orc=2, Undead=16, Tauren=32, Troll=128).
const (
	allianceRaceMask = 1 | 4 | 8 | 64
	hordeRaceMask    = 2 | 16 | 32 | 128
)

func decodeRaceFaction(mask int) string {
	if mask <= 0 {
		return ""
	}
	a := (mask & allianceRaceMask) != 0
	h := (mask & hordeRaceMask) != 0
	if a && !h {
		return "alliance"
	}
	if h && !a {
		return "horde"
	}
	return ""
}

func enrichCatalogFromDB2(cat *BiSCatalog, build, locale string) bool {
	cache := tables.CacheDir()
	if build == "" {
		build = cat.Build
	}
	if build == "" {
		return false
	}
	// Ensure essential tables exist (gracefully no-ops when offline).
	for _, name := range []string{"Item", "ItemSparse", "RandPropPoints", "ItemDamageOneHand", "ItemDamageOneHandCaster", "ItemDamageTwoHand", "ItemDamageTwoHandCaster", "ItemEffect", "ItemXItemEffect", "SpellEffect", "SkillLineAbility"} {
		tables.Ensure(cache, name, build, locale)
	}
	itemTab, err := db2.Load(cache, "Item", build, locale)
	if err != nil {
		return false
	}

	// 1. Item.db2: authoritative ClassID, SubclassID, InventoryType for every item.
	type itemMeta struct {
		cls, sub, inv int64
	}
	metaMap := make(map[int64]itemMeta, itemTab.Len())
	cID := itemTab.Col("ID")
	cCls := itemTab.Col("ClassID")
	cSub := itemTab.Col("SubclassID")
	cInv := itemTab.Col("InventoryType")
	for i := 0; i < itemTab.Len(); i++ {
		r := itemTab.Row(i)
		id := int64(r.IntAt(cID))
		if id <= 0 {
			continue
		}
		m := itemMeta{int64(r.IntAt(cCls)), int64(r.IntAt(cSub)), int64(r.IntAt(cInv))}
		metaMap[id] = m
		if it := cat.Items[id]; it != nil {
			it.ClassID = m.cls
			it.SubID = m.sub
			if m.inv > 0 {
				it.InvType = m.inv
				it.TwoHand = m.inv == 17
			}
		}
	}

	// 2. SkillLineAbility + SpellEffect(CreateItem=24): detect all DB2 crafted equippable items.
	skillTab, _ := db2.Load(cache, "SkillLineAbility", build, locale)
	spellEffTab, _ := db2.Load(cache, "SpellEffect", build, locale)
	if skillTab != nil && spellEffTab != nil {
		spellToSkill := map[int]struct{ sk, sr int64 }{}
		cSkLine := skillTab.Col("SkillLine")
		cSkSpell := skillTab.Col("Spell")
		cSkMin := skillTab.Col("MinSkillLineRank")
		for i := 0; i < skillTab.Len(); i++ {
			r := skillTab.Row(i)
			sk := int64(r.IntAt(cSkLine))
			if _, ok := skillLineNames[sk]; !ok {
				continue
			}
			sp := r.IntAt(cSkSpell)
			if sp > 0 {
				spellToSkill[sp] = struct{ sk, sr int64 }{sk, int64(r.IntAt(cSkMin))}
			}
		}
		cESpell := spellEffTab.Col("SpellID")
		cEEff := spellEffTab.Col("Effect")
		cEItem := spellEffTab.Col("EffectItemType")
		if cESpell >= 0 && cEEff >= 0 && cEItem >= 0 {
			for i := 0; i < spellEffTab.Len(); i++ {
				r := spellEffTab.Row(i)
				if r.IntAt(cEEff) != 24 { // SPELL_EFFECT_CREATE_ITEM
					continue
				}
				skInfo, ok := spellToSkill[r.IntAt(cESpell)]
				if !ok {
					continue
				}
				iid := int64(r.IntAt(cEItem))
				m, ok := metaMap[iid]
				if !ok || invTypeToEquipLoc[m.inv] == "" {
					continue
				}
				it := cat.Items[iid]
				if it == nil {
					it = &BiSCatalogItem{ID: iid, ClassID: m.cls, SubID: m.sub, InvType: m.inv, TwoHand: m.inv == 17}
					cat.Items[iid] = it
				}
				if !hasSourceKind(it.Sources, SrcCrafted) {
					sname := skillLineNames[skInfo.sk]
					rank := skInfo.sr
					if rank <= 1 {
						rank = 0
					}
					label := sname
					if rank > 0 {
						label = fmt.Sprintf("%s %d", sname, rank)
					}
					it.Sources = mergeBiSSources(it.Sources, []BiSSource{{
						Kind:      SrcCrafted,
						Label:     label,
						Skill:     skInfo.sk,
						SkillRank: rank,
					}})
				}
			}
		}
	}

	// 3. RandPropPoints & ItemDamage tables for reconstructing stats of items not in Stats.lua (e.g. Raid items!).
	rppTab, _ := db2.Load(cache, "RandPropPoints", build, locale)
	rppRow := map[int]db2.Row{}
	if rppTab != nil {
		cRID := rppTab.Col("ID")
		for i := 0; i < rppTab.Len(); i++ {
			r := rppTab.Row(i)
			rppRow[r.IntAt(cRID)] = r
		}
	}
	loadDmgTable := func(name string) map[int][7]float64 {
		out := map[int][7]float64{}
		t, err := db2.Load(cache, name, build, locale)
		if err != nil {
			return out
		}
		cIL := t.Col("ItemLevel")
		var qCols [7]int
		for q := 0; q < 7; q++ {
			qCols[q] = t.Col(fmt.Sprintf("Quality_%d", q))
		}
		for i := 0; i < t.Len(); i++ {
			r := t.Row(i)
			il := r.IntAt(cIL)
			var vals [7]float64
			for q := 0; q < 7; q++ {
				if qCols[q] >= 0 {
					vals[q] = r.FloatAt(qCols[q])
				}
			}
			out[il] = vals
		}
		return out
	}
	dmg1H := loadDmgTable("ItemDamageOneHand")
	dmg1HCaster := loadDmgTable("ItemDamageOneHandCaster")
	dmg2H := loadDmgTable("ItemDamageTwoHand")
	dmg2HCaster := loadDmgTable("ItemDamageTwoHandCaster")

	// 4. ItemSparse (in requested locale, plus fallback enUS): names, quality, reqLevel, class/race masks, stats.
	sparse, err := db2.Load(cache, "ItemSparse", build, locale)
	if err != nil && locale != "enUS" {
		sparse, err = db2.Load(cache, "ItemSparse", build, "enUS")
	}
	if sparse != nil {
		csID := sparse.Col("ID")
		csName := sparse.Col("Display_lang")
		csQ := sparse.Col("OverallQualityID")
		csIL := sparse.Col("ItemLevel")
		csRL := sparse.Col("RequiredLevel")
		csInv := sparse.Col("InventoryType")
		csClass := sparse.Col("AllowableClass")
		csRace := sparse.Col("AllowableRace_0")
		csBond := sparse.Col("Bonding")
		csMax := sparse.Col("MaxCount")
		csFlags1 := sparse.Col("Flags_1")
		var statTypeCols, statEdCols [10]int
		for k := 0; k < 10; k++ {
			statTypeCols[k] = sparse.Col(fmt.Sprintf("StatModifier_bonusStat_%d", k))
			statEdCols[k] = sparse.Col(fmt.Sprintf("StatPercentEditor_%d", k))
		}

		isDE := strings.HasPrefix(strings.ToLower(locale), "de")
		for i := 0; i < sparse.Len(); i++ {
			r := sparse.Row(i)
			id := int64(r.IntAt(csID))
			it := cat.Items[id]
			if it == nil {
				continue
			}
			q := int64(r.IntAt(csQ))
			if q < 2 {
				continue
			}
			name := r.StrAt(csName)
			if name != "" {
				if isDE {
					it.NameDE = name
					if it.Name == "" {
						it.Name = name
					}
				} else if it.Name == "" {
					it.Name = name
				}
			}
			if it.Quality == 0 {
				it.Quality = q
			}
			il := int64(r.IntAt(csIL))
			if it.ItemLvl == 0 && il > 0 {
				it.ItemLvl = il
			}
			rl := int64(r.IntAt(csRL))
			if it.ReqLvl == 0 && rl > 0 {
				it.ReqLvl = rl
			}
			inv := int64(r.IntAt(csInv))
			if it.InvType == 0 && inv > 0 {
				it.InvType = inv
				it.TwoHand = inv == 17
			}
			if len(it.Classes) == 0 && csClass >= 0 {
				it.Classes = decodeClassMask(r.IntAt(csClass))
			}
			if it.Faction == "" && csRace >= 0 {
				it.Faction = decodeRaceFaction(r.IntAt(csRace))
			}
			if it.Bind == "" && csBond >= 0 {
				switch r.IntAt(csBond) {
				case 1:
					it.Bind = "bop"
				case 2:
					it.Bind = "boe"
				}
			}
			if csMax >= 0 && r.IntAt(csMax) == 1 {
				it.Unique = true
			}
			if csFlags1 >= 0 && (r.IntAt(csFlags1)&524288) != 0 { // Unique-Equipped
				it.Unique = true
			}

			// Reconstruct stats from RandPropPoints + ItemDamage if not already populated by Stats.lua.
			if len(it.RawStats) == 0 && il > 0 {
				stats := map[string]float64{}
				if rr, ok := rppRow[int(il)]; ok {
					b, okB := randPropBucket[it.InvType]
					if !okB {
						b = 2
					}
					pref := "Superior"
					switch q {
					case 2:
						pref = "Good"
					case 4, 5:
						pref = "Epic"
					}
					pts := float64(rr.IntAt(rppTab.Col(fmt.Sprintf("%s_%d", pref, b))))
					for k := 0; k < 10; k++ {
						if statTypeCols[k] < 0 || statEdCols[k] < 0 {
							continue
						}
						st := r.IntAt(statTypeCols[k])
						ed := r.IntAt(statEdCols[k])
						if st < 0 || ed == 0 {
							continue
						}
						if modKey, ok := db2BonusStatKey[st]; ok {
							val := math.Round(pts * float64(ed) / 10000.0)
							if val != 0 {
								stats[modKey] += val
							}
						}
					}
				}
				// Weapon DPS from ItemDamage*.db2 when weapon.
				if it.ClassID == 2 && q >= 0 && q < 7 {
					caster := (csFlags1 >= 0 && (r.IntAt(csFlags1)&512) != 0) || it.SubID == 10 || it.SubID == 19
					var tbl map[int][7]float64
					if it.InvType == 17 {
						if caster {
							tbl = dmg2HCaster
						} else {
							tbl = dmg2H
						}
					} else {
						if caster {
							tbl = dmg1HCaster
						} else {
							tbl = dmg1H
						}
					}
					if row, ok := tbl[int(il)]; ok && row[q] > 0 {
						stats["ITEM_MOD_DAMAGE_PER_SECOND_SHORT"] = math.Round(row[q]*10) / 10
					}
				}
				if len(stats) > 0 {
					it.RawStats = stats
				}
			}
		}
	}

	// 5. ItemXItemEffect + ItemEffect: flag on-use / proc items (e.g. Raid trinkets like ToEP)
	// and decode passive Equip: stat auras for DB2-only items whose stats live in SpellEffect.
	ixTab, _ := db2.Load(cache, "ItemXItemEffect", build, locale)
	ieTab, _ := db2.Load(cache, "ItemEffect", build, locale)
	if ixTab != nil && ieTab != nil {
		type effInfo struct {
			trig  int
			spell int
		}
		ieMap := make(map[int]effInfo, ieTab.Len())
		cIEID := ieTab.Col("ID")
		cIETrig := ieTab.Col("TriggerType")
		cIESpell := ieTab.Col("SpellID")
		for i := 0; i < ieTab.Len(); i++ {
			r := ieTab.Row(i)
			ieMap[r.IntAt(cIEID)] = effInfo{r.IntAt(cIETrig), r.IntAt(cIESpell)}
		}
		spellNameTab, _ := db2.Load(cache, "SpellName", build, locale)
		spellNames := map[int]string{}
		if spellNameTab != nil {
			cSNID := spellNameTab.Col("ID")
			cSN := spellNameTab.Col("Name_lang")
			for i := 0; i < spellNameTab.Len(); i++ {
				r := spellNameTab.Row(i)
				spellNames[r.IntAt(cSNID)] = r.StrAt(cSN)
			}
		}
		cIXItem := ixTab.Col("ItemID")
		cIXEff := ixTab.Col("ItemEffectID")
		for i := 0; i < ixTab.Len(); i++ {
			r := ixTab.Row(i)
			iid := int64(r.IntAt(cIXItem))
			it := cat.Items[iid]
			if it == nil {
				continue
			}
			ef, ok := ieMap[r.IntAt(cIXEff)]
			if !ok || ef.spell <= 0 {
				continue
			}
			// TriggerType 0=OnUse, 2=ChanceOnHit
			if ef.trig == 0 || ef.trig == 2 {
				it.HasProc = true
				if len(it.Procs) == 0 {
					if sn := spellNames[ef.spell]; sn != "" {
						prefix := "Use"
						if ef.trig == 2 {
							prefix = "Proc"
						}
						it.Procs = append(it.Procs, prefix+": "+sn)
					}
				}
			}
		}
	}

	return true
}

// DisplayName returns the localized name of a BiS catalog item.
func (it *BiSCatalogItem) DisplayName(lang string) string {
	if lang == "de" && it.NameDE != "" {
		return it.NameDE
	}
	if it.Name != "" {
		return it.Name
	}
	if it.NameDE != "" {
		return it.NameDE
	}
	return fmt.Sprintf("Item #%d", it.ID)
}

// EquipLoc returns the WoW INVTYPE_* string for this item.
func (it *BiSCatalogItem) EquipLoc() string {
	return invTypeToEquipLoc[it.InvType]
}

// ToSVTable converts a BiSCatalogItem into a *sv.Table compatible with NormStats, NormItem, Short, and Tooltip.
// If an in-game exported item exists in exported, its authoritative stats & tooltip are preferred.
func (it *BiSCatalogItem) ToSVTable(exported *sv.Table, lang string) *sv.Table {
	if exported != nil {
		return exported
	}
	t := sv.NewTable()
	t.Set("id", it.ID)
	t.Set("name", it.DisplayName(lang))
	t.Set("quality", it.Quality)
	t.Set("ilvl", it.ItemLvl)
	t.Set("reqLevel", it.ReqLvl)
	t.Set("equipLoc", it.EquipLoc())
	t.Set("classID", it.ClassID)
	t.Set("subclassID", it.SubID)
	st := sv.NewTable()
	// Deterministic key order for reproducible stat summaries.
	keys := make([]string, 0, len(it.RawStats))
	for k := range it.RawStats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := it.RawStats[k]; v != 0 {
			st.Set(k, v)
		}
	}
	t.Set("stats", st)
	return t
}

var fallbackPlacesDE = map[string]string{
	"Molten Core":         "Geschmolzener Kern",
	"Onyxia's Lair":       "Onyxias Hort",
	"Blackwing Lair":      "Pechschwingenhort",
	"Zul'Gurub":           "Zul'Gurub",
	"Ruins of Ahn'Qiraj":  "Ruinen von Ahn'Qiraj",
	"Temple of Ahn'Qiraj": "Tempel von Ahn'Qiraj",
	"Naxxramas":           "Naxxramas",
	"World Bosses":        "Weltbosse",
	"Mount Hyjal":         "Berg Hyjal",
	"Barrow Deeps":        "Grabhügeltiefen",
}

// LocalizePlace translates a dungeon/raid/zone name when language is German.
func (c *BiSCatalog) LocalizePlace(place, lang string) string {
	if lang == "de" {
		if de, ok := c.PlacesDE[place]; ok && de != "" {
			return de
		}
		if de, ok := fallbackPlacesDE[place]; ok && de != "" {
			return de
		}
	}
	return place
}

// SourceKindLabel returns the localized short label of a source category.
func SourceKindLabel(kind string) string {
	switch kind {
	case SrcQuest:
		return i18n.T("Quest")
	case SrcDungeon:
		return i18n.T("Dungeon")
	case SrcRaid:
		return i18n.T("Raid")
	case SrcCrafted:
		return i18n.T("Profession")
	case SrcVendor:
		return i18n.T("Vendor")
	case SrcReputation:
		return i18n.T("Reputation")
	case SrcPvP:
		return i18n.T("PvP")
	case SrcWorld:
		return i18n.T("World drop")
	default:
		return kind
	}
}

// FormatSourceLabel localizes the place and profession parts of a source label.
func (c *BiSCatalog) FormatSourceLabel(s BiSSource, lang string) string {
	if s.Kind == SrcCrafted && s.Skill > 0 {
		name := skillLocalized(s.Skill, lang)
		if s.SkillRank > 0 {
			return fmt.Sprintf("%s %d", name, s.SkillRank)
		}
		return name
	}
	if s.Kind == SrcWorld && s.Label == "World Drop" {
		return i18n.T("World drop")
	}
	label := s.Label
	if lang == "de" && s.Place != "" {
		if de := c.LocalizePlace(s.Place, lang); de != "" && de != s.Place {
			label = strings.Replace(label, s.Place, de, 1)
		}
	}
	if s.Kind == SrcQuest && s.QuestType != "" {
		var tag string
		switch s.QuestType {
		case "d":
			tag = i18n.T("Dungeon")
		case "r":
			tag = i18n.T("Raid")
		case "e":
			tag = i18n.T("Elite")
		case "g":
			tag = i18n.T("Group")
		}
		if tag != "" {
			label = fmt.Sprintf("%s (%s)", label, tag)
		}
	}
	return label
}

func skillLocalized(skillID int64, lang string) string {
	if lang == "de" {
		switch skillID {
		case 164:
			return "Schmiedekunst"
		case 165:
			return "Lederverarbeitung"
		case 171:
			return "Alchemie"
		case 182:
			return "Kräuterkunde"
		case 185:
			return "Kochkunst"
		case 197:
			return "Schneiderei"
		case 202:
			return "Ingenieurskunst"
		case 333:
			return "Verzauberkunst"
		}
	}
	if s, ok := skillLineNames[skillID]; ok {
		return s
	}
	return i18n.T("Profession")
}

// BiSCatalogUpdated returns a short timestamp string when live addon data is loaded (used for UI diagnostics).
func BiSCatalogUpdated(wow string) string {
	p := filepath.Join(wow, "Interface", "AddOns", "ForeverMyBis", "Data", "Items.lua")
	if st, err := os.Stat(p); err == nil {
		return st.ModTime().UTC().Format(time.DateOnly)
	}
	return ""
}
