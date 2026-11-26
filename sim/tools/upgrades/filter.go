package upgrades

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/kejith/wow-tools-forever/sim/db2"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/internal/tables"
)

// CategoryPvP identifies PvP, Honor, and Battleground reputation items.
const CategoryPvP = "pvp"

// KnownCategories lists every supported item filter category.
var KnownCategories = []string{CategoryPvP}

// IsKnownCategory reports whether cat is a supported item filter category (case-insensitive).
func IsKnownCategory(cat string) bool {
	c := strings.ToLower(strings.TrimSpace(cat))
	for _, k := range KnownCategories {
		if k == c {
			return true
		}
	}
	return false
}

// Filter excludes specific item IDs and/or item categories (such as "pvp") from upgrade suggestions
// and in-game exports.
type Filter struct {
	ExcludedIDs        map[int64]bool
	ExcludedCategories map[string]bool
}

// NewFilter builds a normalized Filter from item IDs and category names.
func NewFilter(ids []int64, categories []string) *Filter {
	f := &Filter{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if f.ExcludedIDs == nil {
			f.ExcludedIDs = map[int64]bool{}
		}
		f.ExcludedIDs[id] = true
	}
	for _, c := range categories {
		k := strings.ToLower(strings.TrimSpace(c))
		if k == "" {
			continue
		}
		if f.ExcludedCategories == nil {
			f.ExcludedCategories = map[string]bool{}
		}
		f.ExcludedCategories[k] = true
	}
	return f
}

// Empty reports whether the filter has no active exclusions.
func (f *Filter) Empty() bool {
	return f == nil || (len(f.ExcludedIDs) == 0 && len(f.ExcludedCategories) == 0)
}

// IsExcludedID reports whether item ID id is explicitly excluded.
func (f *Filter) IsExcludedID(id int64) bool {
	return f != nil && f.ExcludedIDs[id]
}

// ExcludesCategory reports whether category cat is excluded.
func (f *Filter) ExcludesCategory(cat string) bool {
	return f != nil && f.ExcludedCategories[strings.ToLower(strings.TrimSpace(cat))]
}

// Excludes reports whether the given item should be filtered out.
func (f *Filter) Excludes(id int64, it *sv.Table, cls *PvPClassifier) bool {
	if f.Empty() {
		return false
	}
	if f.ExcludedIDs[id] {
		return true
	}
	if len(f.ExcludedCategories) > 0 {
		for _, cat := range ItemCategories(id, it, cls) {
			if f.ExcludedCategories[cat] {
				return true
			}
		}
	}
	return false
}

// SortedIDs returns excluded item IDs in ascending order.
func (f *Filter) SortedIDs() []int64 {
	if f == nil || len(f.ExcludedIDs) == 0 {
		return nil
	}
	out := make([]int64, 0, len(f.ExcludedIDs))
	for id := range f.ExcludedIDs {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SortedCategories returns excluded categories in alphabetical order.
func (f *Filter) SortedCategories() []string {
	if f == nil || len(f.ExcludedCategories) == 0 {
		return nil
	}
	out := make([]string, 0, len(f.ExcludedCategories))
	for c := range f.ExcludedCategories {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// ---------- PvP / Honor classification ----------

// Classic and WoW: Forever PvP / Battleground faction IDs.
// 509: The League of Arathor (AB Alliance)
// 510: The Defilers (AB Horde)
// 729: Frostwolf Clan (AV Horde)
// 730: Stormpike Guard (AV Alliance)
// 889: Warsong Outriders (WSG Horde)
// 890: Silverwing Sentinels (WSG Alliance)
// 2798: Darkspear Raiders (Forever BG Horde)
// 2799: Theramore Expeditionary Force (Forever BG Alliance)
// 2800: PvP Rank Points (Forever Honor/Rank faction)
var basePvPFactions = map[int64]bool{
	509:  true,
	510:  true,
	729:  true,
	730:  true,
	889:  true,
	890:  true,
	2798: true,
	2799: true,
	2800: true,
}

// ParentFactionIDs for Battleground forces in Faction.db2 (891 = Alliance Forces, 892 = Horde Forces).
const (
	factionAllianceForces = 891
	factionHordeForces    = 892
)

// PvP / Honor currencies in CurrencyTypes.db2 / ItemExtendedCost.db2.
// 1792: Honor Points, 3468: Rank Points (Forever), 3473: Renown - PvP Rank (Forever).
var pvpCurrencies = map[int64]bool{
	1792: true,
	3468: true,
	3473: true,
}

// Classic Battleground Mark of Honor item IDs used in ItemExtendedCost.db2.
// 20558: Warsong Gulch Mark of Honor
// 20559: Arathi Basin Mark of Honor
// 20560: Alterac Valley Mark of Honor
var pvpMarkItems = map[int64]bool{
	20558: true,
	20559: true,
	20560: true,
}

// DB2 ItemModType bonus stats that mark PvP gear (35 = Resilience Rating, 57 = PvP Power).
var pvpStatTypes = map[int]bool{
	35: true,
	57: true,
}

// Battleground quartermaster NPC prefixes in the bundled/live BiS catalog whose vendor stock
// consists of Battleground reputation / honor rewards (some of which have MinFactionID=0 in 1.60 DB2).
var bgQuartermasters = []string{
	"Illiyana Moonblaze",
	"Kelm Hargunth",
	"Gaelden Hammersmith",
	"Grunnda Wolfheart",
	"Thanthaldis Snowgleam",
	"Jekyll Flandring",
	"Samuel Hawke",
	"Rutherford Twing",
}

// Tooltip patterns (English and German) identifying PvP rank requirements, Battleground faction
// reputation requirements, and PvP-only stats (Resilience / PvP Power).
var (
	pvpRankTipRe = regexp.MustCompile(`(?i)^(?:Requires|Benötigt)\s+(?:` +
		// Generic rank / honor rank lines
		`(?:Honor\s+|PvP\s+)?Rank\s+\d+|(?:Ehren|PvP-)?Rang\s+\d+|` +
		// Alliance ranks 1..14 (EN)
		`Private|Corporal|Sergeant|Master Sergeant|Sergeant Major|Knight|Knight-Lieutenant|Knight-Captain|Knight-Champion|Lieutenant Commander|Commander|Marshal|Field Marshal|Grand Marshal|` +
		// Horde ranks 1..14 (EN)
		`Scout|Grunt|Senior Sergeant|First Sergeant|Stone Guard|Blood Guard|Legionnaire|Centurion|Champion|Lieutenant General|General|Warlord|High Warlord|` +
		// Alliance ranks 1..14 (DE)
		`Gefreiter|Fußknecht|Landsknecht|Feldwebel|Fähnrich|Leutnant|Hauptmann|Kürassier|Ritter der Allianz|Feldkommandant|Rittmeister|Marschall|Feldmarschall|Großmarschall|` +
		// Horde ranks 1..14 (DE)
		`Späher|Grunzer|Waffenträger|Schlachtrufer|Rottenmeister|Steingardist|Blutgardist|Zornbringer|Klinge der Horde|Feldherr|Sturmreiter|Kriegsherr|Kriegsfürst|Oberster Kriegsfürst` +
		`)(?:\s|$|\.)`)

	pvpFactionTipRe = regexp.MustCompile(`(?i)(?:` +
		`Warsong Outriders|Silverwing Sentinels|The League of Arathor|League of Arathor|The Defilers|Defilers|Frostwolf Clan|Stormpike Guard|Darkspear Raiders|Theramore Expeditionary Force|` +
		`Vorhut der Warsong|Silberschwingen|Der Bund von Arathor|Bund von Arathor|Die Entweihten|Entweihten|Frostwolfklan|Stormpikegarde|Räuber der Darkspear|Expedition von Theramore` +
		`)`)

	pvpStatTipRe = regexp.MustCompile(`(?i)\b(?:resilience\s+rating|pvp\s+power|abhärtungswertung|pvp-macht)\b`)
)

// IsPvPFaction reports whether factionID is a known PvP / Battleground reputation faction.
func IsPvPFaction(factionID int64) bool {
	return basePvPFactions[factionID]
}

// IsPvPExtendedCost reports whether an ItemExtendedCost entry requires Honor points, Rank points,
// Battleground marks (WSG, AB, AV), an arena rating, or a PvP faction reputation.
func IsPvPExtendedCost(currencies, itemIDs []int64, arenaRating, minFaction int64) bool {
	if arenaRating > 0 {
		return true
	}
	if minFaction > 0 && basePvPFactions[minFaction] {
		return true
	}
	for _, c := range currencies {
		if pvpCurrencies[c] {
			return true
		}
	}
	for _, iid := range itemIDs {
		if pvpMarkItems[iid] {
			return true
		}
	}
	return false
}

// PvPClassifier holds precomputed PvP item IDs, PvP factions, and PvP extended cost IDs
// loaded from DB2 tables (ItemSparse, Faction, ItemExtendedCost) and the bundled/live BiS catalog.
type PvPClassifier struct {
	pvpIDs      map[int64]bool
	pvpFactions map[int64]bool
	pvpExtCosts map[int64]bool
}

// NewPvPClassifier constructs an initialized empty PvPClassifier (primarily for tests).
func NewPvPClassifier(pvpIDs []int64, pvpExtCosts []int64) *PvPClassifier {
	c := &PvPClassifier{
		pvpIDs:      map[int64]bool{},
		pvpFactions: map[int64]bool{},
		pvpExtCosts: map[int64]bool{},
	}
	for k := range basePvPFactions {
		c.pvpFactions[k] = true
	}
	for _, id := range pvpIDs {
		if id > 0 {
			c.pvpIDs[id] = true
		}
	}
	for _, id := range pvpExtCosts {
		if id > 0 {
			c.pvpExtCosts[id] = true
		}
	}
	return c
}

// IsPvPID reports whether item ID is classified as PvP by DB2 or catalog metadata.
func (c *PvPClassifier) IsPvPID(id int64) bool {
	return c != nil && c.pvpIDs[id]
}

// IsPvPFactionID reports whether factionID is a PvP faction (including Faction.db2 children of BG forces).
func (c *PvPClassifier) IsPvPFactionID(factionID int64) bool {
	if basePvPFactions[factionID] {
		return true
	}
	return c != nil && c.pvpFactions[factionID]
}

// IsPvPExtendedCostID reports whether an ItemExtendedCost ID requires honor, rank points, BG marks, or PvP faction.
func (c *PvPClassifier) IsPvPExtendedCostID(costID int64) bool {
	return c != nil && c.pvpExtCosts[costID]
}

var pvpClsCache struct {
	mu    sync.Mutex
	byKey map[string]*PvPClassifier
}

// LoadPvPClassifier builds (or returns a cached) PvPClassifier for the given game folder and client build.
func LoadPvPClassifier(wow, build, locale string) *PvPClassifier {
	key := wow + "|" + build + "|" + locale
	pvpClsCache.mu.Lock()
	if pvpClsCache.byKey != nil {
		if cls, ok := pvpClsCache.byKey[key]; ok {
			pvpClsCache.mu.Unlock()
			return cls
		}
	}
	pvpClsCache.mu.Unlock()

	cls := NewPvPClassifier(nil, nil)

	// 1. Bundled BiS catalog (always available offline): marks Forever custom PvP/Honor items
	//    (many of which have RequiredPVPRank=0 in ItemSparse) and BG quartermaster items.
	if bc := loadBundledCatalog(); bc != nil {
		if build == "" {
			build = bc.Build
		}
		for idStr, it := range bc.Items {
			if it == nil {
				continue
			}
			id, ok := intKey(idStr)
			if !ok || id <= 0 {
				continue
			}
			if hasBiSPvPSource(it.Sources) {
				cls.pvpIDs[id] = true
			}
		}
	}

	// 2. Live ForeverMyBis addon (if installed): pick up any newer PvP items not yet bundled.
	if wow != "" {
		tmp := &BiSCatalog{PlacesDE: map[string]string{}, Items: map[int64]*BiSCatalogItem{}}
		if mergeLiveForeverMyBis(tmp, wow, locale) {
			for id, it := range tmp.Items {
				if hasBiSPvPSource(it.Sources) {
					cls.pvpIDs[id] = true
				}
			}
		}
	}

	// 3. DB2 tables (Faction, ItemExtendedCost, ItemSparse).
	cache := tables.CacheDir()
	if build != "" {
		tables.Ensure(cache, "Faction", build, "")
		tables.Ensure(cache, "ItemExtendedCost", build, "")
		cls.loadDB2(cache, build, locale)
	}

	pvpClsCache.mu.Lock()
	if pvpClsCache.byKey == nil {
		pvpClsCache.byKey = map[string]*PvPClassifier{}
	}
	pvpClsCache.byKey[key] = cls
	pvpClsCache.mu.Unlock()
	return cls
}

func (c *PvPClassifier) loadDB2(cache, build, locale string) {
	// Faction.db2: detect any faction under Alliance/Horde Forces (891/892) or tied to Honor/PvP Rank currencies.
	if fac, err := db2.Load(cache, "Faction", build, ""); err == nil && fac != nil {
		cID := fac.Col("ID")
		cParent := fac.Col("ParentFactionID")
		cRenown := fac.Col("RenownCurrencyID")
		if cID >= 0 {
			for i := 0; i < fac.Len(); i++ {
				r := fac.Row(i)
				id := int64(r.IntAt(cID))
				if id <= 0 {
					continue
				}
				parent := int64(0)
				if cParent >= 0 {
					parent = int64(r.IntAt(cParent))
				}
				renown := int64(0)
				if cRenown >= 0 {
					renown = int64(r.IntAt(cRenown))
				}
				if parent == factionAllianceForces || parent == factionHordeForces || pvpCurrencies[renown] {
					c.pvpFactions[id] = true
				}
			}
		}
	}

	// ItemExtendedCost.db2: record extended cost IDs that cost Honor, Rank Points, BG Marks, Arena Rating, or PvP rep.
	if iec, err := db2.Load(cache, "ItemExtendedCost", build, ""); err == nil && iec != nil {
		cID := iec.Col("ID")
		cArena := iec.Col("RequiredArenaRating")
		cMinFac := iec.Col("MinFactionID")
		var curCols, itemCols [5]int
		for k := 0; k < 5; k++ {
			curCols[k] = iec.Col(fmt.Sprintf("CurrencyID_%d", k))
			itemCols[k] = iec.Col(fmt.Sprintf("ItemID_%d", k))
		}
		if cID >= 0 {
			for i := 0; i < iec.Len(); i++ {
				r := iec.Row(i)
				id := int64(r.IntAt(cID))
				if id <= 0 {
					continue
				}
				var arena, minFac int64
				if cArena >= 0 {
					arena = int64(r.IntAt(cArena))
				}
				if cMinFac >= 0 {
					minFac = int64(r.IntAt(cMinFac))
				}
				var curs, items []int64
				for k := 0; k < 5; k++ {
					if curCols[k] >= 0 {
						if v := int64(r.IntAt(curCols[k])); v > 0 {
							curs = append(curs, v)
						}
					}
					if itemCols[k] >= 0 {
						if v := int64(r.IntAt(itemCols[k])); v > 0 {
							items = append(items, v)
						}
					}
				}
				if IsPvPExtendedCost(curs, items, arena, minFac) || (minFac > 0 && c.pvpFactions[minFac]) {
					c.pvpExtCosts[id] = true
				}
			}
		}
	}

	// ItemSparse.db2: check RequiredPVPRank, RequiredPVPMedal, MinFactionID, PvP stats, and PvP ItemSets.
	sparse := loadItemSparseAnyLocale(cache, build, locale)
	if sparse == nil {
		return
	}
	cID := sparse.Col("ID")
	cRank := sparse.Col("RequiredPVPRank")
	cMedal := sparse.Col("RequiredPVPMedal")
	cMinFac := sparse.Col("MinFactionID")
	cSet := sparse.Col("ItemSet")
	var statCols [10]int
	for k := 0; k < 10; k++ {
		statCols[k] = sparse.Col(fmt.Sprintf("StatModifier_bonusStat_%d", k))
	}
	if cID < 0 {
		return
	}

	pvpSets := map[int64]bool{}
	setItems := map[int64][]int64{}

	for i := 0; i < sparse.Len(); i++ {
		r := sparse.Row(i)
		id := int64(r.IntAt(cID))
		if id <= 0 {
			continue
		}
		isPvP := c.pvpIDs[id]
		if !isPvP && cRank >= 0 && r.IntAt(cRank) > 0 {
			isPvP = true
		}
		if !isPvP && cMedal >= 0 && r.IntAt(cMedal) > 0 {
			isPvP = true
		}
		if !isPvP && cMinFac >= 0 {
			if mf := int64(r.IntAt(cMinFac)); mf > 0 && c.pvpFactions[mf] {
				isPvP = true
			}
		}
		if !isPvP {
			for k := 0; k < 10; k++ {
				if statCols[k] >= 0 && pvpStatTypes[r.IntAt(statCols[k])] {
					isPvP = true
					break
				}
			}
		}
		if isPvP {
			c.pvpIDs[id] = true
		}
		if cSet >= 0 {
			if sid := int64(r.IntAt(cSet)); sid > 0 {
				setItems[sid] = append(setItems[sid], id)
				if isPvP {
					pvpSets[sid] = true
				}
			}
		}
	}

	// Propagate PvP classification across PvP ItemSets (e.g. Forever custom honor sets sharing a Classic PvP ItemSet).
	for sid := range pvpSets {
		for _, id := range setItems[sid] {
			c.pvpIDs[id] = true
		}
	}
}

func loadItemSparseAnyLocale(cache, build, locale string) *db2.Table {
	tried := map[string]bool{}
	candidates := []string{locale, "enUS", "deDE"}
	for _, loc := range candidates {
		if loc == "" || tried[loc] {
			continue
		}
		tried[loc] = true
		if t, err := db2.Load(cache, "ItemSparse", build, loc); err == nil && t != nil {
			return t
		}
	}
	// Fallback: any cached ItemSparse for this build.
	if matches, err := filepath.Glob(filepath.Join(cache, "ItemSparse-"+build+"-*.csv")); err == nil && len(matches) > 0 {
		base := filepath.Base(matches[0])
		loc := strings.TrimSuffix(strings.TrimPrefix(base, "ItemSparse-"+build+"-"), ".csv")
		if t, err := db2.Load(cache, "ItemSparse", build, loc); err == nil && t != nil {
			return t
		}
	}
	return nil
}

func hasBiSPvPSource(sources []BiSSource) bool {
	if len(sources) == 0 {
		return false
	}
	for _, s := range sources {
		if s.Kind == SrcPvP {
			return true
		}
	}
	// Check if all vendor sources belong to classic Battleground quartermasters.
	hasBGVendor := false
	for _, s := range sources {
		if s.Kind != SrcVendor {
			return false
		}
		if !isBGQuartermasterLabel(s.Label, s.Place) {
			return false
		}
		hasBGVendor = true
	}
	return hasBGVendor
}

func isBGQuartermasterLabel(label, place string) bool {
	if strings.EqualFold(place, "Alterac Valley") || strings.EqualFold(place, "Warsong Gulch") || strings.EqualFold(place, "Arathi Basin") {
		return true
	}
	for _, npc := range bgQuartermasters {
		if strings.Contains(label, npc) {
			return true
		}
	}
	return false
}

// IsPvPItem reports whether an item is PvP / Honor / Battleground gear using the classifier's
// DB2 & catalog data, direct item fields, PvP stats, and localized tooltip lines.
func IsPvPItem(id int64, it *sv.Table, cls *PvPClassifier) bool {
	if cls != nil && cls.IsPvPID(id) {
		return true
	}
	if cls == nil && id > 0 {
		if bc := loadBundledCatalog(); bc != nil {
			if bi := bc.Items[itoa(id)]; bi != nil && hasBiSPvPSource(bi.Sources) {
				return true
			}
		}
	}
	if it == nil {
		return false
	}
	// Direct numeric fields (when supplied by DB2/export/tests).
	for _, k := range []string{"requiredHonorRank", "requiredPvPRank", "requiredPVPRank", "requiredPvPMedal", "requiredPVPMedal"} {
		if n, ok := it.Num(k); ok && n > 0 {
			return true
		}
	}
	for _, k := range []string{"requiredReputationFaction", "minFactionID"} {
		if n, ok := it.Num(k); ok && n > 0 {
			if cls.IsPvPFactionID(int64(n)) {
				return true
			}
		}
	}
	for _, k := range []string{"extendedCost", "extendedCostID"} {
		if n, ok := it.Num(k); ok && n > 0 && cls.IsPvPExtendedCostID(int64(n)) {
			return true
		}
	}
	// PvP-only stats (Resilience / PvP Power).
	if st := it.Tab("stats"); st != nil {
		for _, k := range []string{"ITEM_MOD_RESILIENCE_RATING_SHORT", "ITEM_MOD_PVP_POWER_SHORT"} {
			if n, ok := st.Num(k); ok && n > 0 {
				return true
			}
		}
	}
	// Tooltip lines (English & German): rank requirements, BG faction requirements, PvP stats.
	for _, line := range tipLines(it) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if pvpRankTipRe.MatchString(trimmed) {
			return true
		}
		if (strings.HasPrefix(trimmed, "Requires ") || strings.HasPrefix(trimmed, "Benötigt ")) && pvpFactionTipRe.MatchString(trimmed) {
			return true
		}
		if pvpStatTipRe.MatchString(trimmed) {
			return true
		}
	}
	return false
}

// IsPvPBiSItem reports whether a BiSCatalogItem (optionally paired with its in-game export) is PvP gear.
func IsPvPBiSItem(it *BiSCatalogItem, exported *sv.Table, cls *PvPClassifier) bool {
	if it != nil && hasBiSPvPSource(it.Sources) {
		return true
	}
	id := int64(0)
	if it != nil {
		id = it.ID
	}
	return IsPvPItem(id, exported, cls)
}

// ItemCategories returns all filter categories that match the given item (e.g. ["pvp"]).
func ItemCategories(id int64, it *sv.Table, cls *PvPClassifier) []string {
	var cats []string
	if IsPvPItem(id, it, cls) {
		cats = append(cats, CategoryPvP)
	}
	return cats
}

// ExportedItemSummary holds the name and quality of an item from WoWToolsForever_Items.
type ExportedItemSummary struct {
	Name    string
	Quality int
}

// LookupExportedItemNames resolves name and quality for the given item IDs from the in-game item export.
func LookupExportedItemNames(wow string, ids []int64) map[int64]ExportedItemSummary {
	out := map[int64]ExportedItemSummary{}
	if wow == "" || len(ids) == 0 {
		return out
	}
	exp := ItemExport(wow, nil)
	if exp == nil {
		return out
	}
	items := exp.Tab("items")
	if items == nil {
		return out
	}
	for _, id := range ids {
		it, ok := items.Get(id).(*sv.Table)
		if !ok || it == nil {
			it, ok = items.Get(itoa(id)).(*sv.Table)
		}
		if !ok || it == nil {
			continue
		}
		name := it.Str("name")
		if name == "" {
			continue
		}
		q := 1
		if qv, ok := it.Num("quality"); ok && qv >= 0 {
			q = int(qv)
		}
		out[id] = ExportedItemSummary{Name: name, Quality: q}
	}
	return out
}
