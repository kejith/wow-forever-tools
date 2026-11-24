package upgrades

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// SlotOf maps an equipLoc to the inventory slots it fits (upgrades.SLOT_OF).
var SlotOf = map[string][]int64{
	"INVTYPE_HEAD": {1}, "INVTYPE_NECK": {2}, "INVTYPE_SHOULDER": {3}, "INVTYPE_CHEST": {5}, "INVTYPE_ROBE": {5},
	"INVTYPE_WAIST": {6}, "INVTYPE_LEGS": {7}, "INVTYPE_FEET": {8}, "INVTYPE_WRIST": {9}, "INVTYPE_HAND": {10},
	"INVTYPE_FINGER": {11, 12}, "INVTYPE_TRINKET": {13, 14}, "INVTYPE_CLOAK": {15},
	"INVTYPE_WEAPON": {16}, "INVTYPE_WEAPONMAINHAND": {16}, "INVTYPE_HOLDABLE": {17}, "INVTYPE_SHIELD": {17},
	"INVTYPE_RANGED": {18}, "INVTYPE_RANGEDRIGHT": {18}, "INVTYPE_2HWEAPON": {16, 17},
}

// Weapon subclasses usable per class (WoW ItemSubclassWeapon):
// 0=1H Axe, 1=2H Axe, 2=Bow, 3=Gun, 4=1H Mace, 5=2H Mace, 6=Polearm,
// 7=1H Sword, 8=2H Sword, 10=Staff, 13=Fist, 15=Dagger, 16=Thrown,
// 18=Crossbow, 19=Wand.
var usableWeaponsByClass = map[string]map[int64]bool{
	"WARLOCK": {7: true, 10: true, 15: true, 19: true},
	"PRIEST":  {4: true, 10: true, 15: true, 19: true},
	"MAGE":    {7: true, 10: true, 15: true, 19: true},
	"DRUID":   {4: true, 5: true, 10: true, 13: true, 15: true},
	"SHAMAN":  {0: true, 1: true, 4: true, 5: true, 10: true, 13: true, 15: true},
	"PALADIN": {0: true, 1: true, 4: true, 5: true, 6: true, 7: true, 8: true},
	"ROGUE":   {0: true, 2: true, 3: true, 4: true, 7: true, 13: true, 15: true, 16: true, 18: true},
	"HUNTER":  {0: true, 1: true, 2: true, 3: true, 6: true, 7: true, 8: true, 10: true, 13: true, 15: true, 18: true},
	"WARRIOR": {0: true, 1: true, 2: true, 3: true, 4: true, 5: true, 6: true, 7: true, 8: true, 10: true, 13: true, 15: true, 16: true, 18: true},
}

func usableWeaponsFor(classFile string) map[int64]bool {
	if m, ok := usableWeaponsByClass[classFile]; ok {
		return m
	}
	return usableWeaponsByClass["WARLOCK"]
}

// usableArmorFor returns the ItemSubclassArmor values a class can equip at
// level (0=Misc/rings/trinkets/cloaks/offhand, 1=Cloth, 2=Leather, 3=Mail,
// 4=Plate, 6=Shield).
func usableArmorFor(classFile string, level float64) map[int64]bool {
	switch classFile {
	case "DRUID", "ROGUE":
		return map[int64]bool{0: true, 1: true, 2: true}
	case "HUNTER":
		if level >= 40 {
			return map[int64]bool{0: true, 1: true, 2: true, 3: true}
		}
		return map[int64]bool{0: true, 1: true, 2: true}
	case "SHAMAN":
		if level >= 40 {
			return map[int64]bool{0: true, 1: true, 2: true, 3: true, 6: true}
		}
		return map[int64]bool{0: true, 1: true, 2: true, 6: true}
	case "PALADIN", "WARRIOR":
		if level >= 40 {
			return map[int64]bool{0: true, 1: true, 2: true, 3: true, 4: true, 6: true}
		}
		return map[int64]bool{0: true, 1: true, 2: true, 3: true, 6: true}
	default: // WARLOCK, PRIEST, MAGE, unknown
		return map[int64]bool{0: true, 1: true}
	}
}

func isUnique(tip []string) bool {
	for _, t := range tip {
		if t == "Unique" || t == "Unique-Equipped" || t == "Einzigartig" || t == "Einzigartig anlegbar" ||
			strings.HasPrefix(t, "Unique-Equipped:") || strings.HasPrefix(t, "Einzigartig anlegbar:") {
			return true
		}
	}
	return false
}

var (
	equipPrefix   = []string{"Equip:", "Anlegen:"}
	usePrefix     = []string{"Use:", "Benutzen:"}
	classesPrefix = []string{"Classes:", "Klassen:"}
	warlockNames  = []string{"Warlock", "Hexenmeister", "Hexenmeisterin"}
)

// pyD is Python's \d in a str pattern: every Unicode decimal digit.
const pyD = `\p{Nd}`

// statLine is upgrades.STAT_LINE without its two negative lookaheads, which RE2 does not have: the two
// alternatives that use them are checked by statIncrease.
var statLine = regexp.MustCompile(`^(?:Equip: (?:\+` + pyD + `+ Attack Power` +
	`|Improves your chance to hit by|Improves your chance to get a critical strike by|Your spells pierce ` + pyD + `+` +
	`|Decreases the magical resistances of your spell targets by ` + pyD + `+` +
	`|Restores ` + pyD + `+ (?:mana|health) per ` + pyD + `+ sec|Increased Defense \+` + pyD + `+)` +
	`|Anlegen: (?:\+` + pyD + `+ Angriffskraft` +
	`|Verbessert Eure Trefferchance um|Erhöht Eure kritische Trefferchance um|Eure Zauber durchdringen ` + pyD + `+` +
	`|Stellt alle ` + pyD + `+ Sek\. ` + pyD + `+ (?:Mana|Gesundheit) wieder her|Verteidigung \+` + pyD + `+))`)

var (
	increaseEN = regexp.MustCompile(`^.*(?:damage|healing).*by up to ` + pyD + `+`)
	increaseDE = regexp.MustCompile(`^.*(?:Schaden|Heilung).*um bis zu ` + pyD + `+`)
)

// firstLine is the text up to a line break: what `.` in a Python pattern can reach.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// isStatLine is STAT_LINE.match: an "Equip:" line that only spells out a value from the stats.
func isStatLine(t string) bool {
	if statLine.MatchString(t) {
		return true
	}
	// "Equip: Increases (?!damage done to ).*(?:damage|healing).*by up to \d+"
	if rest, ok := strings.CutPrefix(t, "Equip: Increases "); ok &&
		!strings.HasPrefix(rest, "damage done to ") && increaseEN.MatchString(rest) {
		return true
	}
	// "Anlegen: Erhöht (?!.*Schaden gegen ).*(?:Schaden|Heilung).*um bis zu \d+"
	if rest, ok := strings.CutPrefix(t, "Anlegen: Erhöht "); ok &&
		!strings.Contains(firstLine(rest), "Schaden gegen ") && increaseDE.MatchString(rest) {
		return true
	}
	return false
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// SpecialEffects is upgrades.special_effects: tooltip effects the scale cannot rate. "Chance on hit:" procs
// need melee hits and are left out.
func SpecialEffects(tip []string) []string {
	out := []string{}
	for _, t := range tip {
		if hasAnyPrefix(t, usePrefix) || (hasAnyPrefix(t, equipPrefix) && !isStatLine(t)) {
			out = append(out, t)
		}
	}
	return out
}

var skillReq = regexp.MustCompile(`^(?:Requires|Benötigt) (.+?) \((` + pyD + `+)\)$`)

// meetsSkillRequirements is upgrades.meets_skill_requirements: false for a line like "Requires Engineering
// (215)" the character does not meet.
func meetsSkillRequirements(tip []string, skills map[string]float64) bool {
	for _, t := range tip {
		m := skillReq.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		need, _ := pyfmt.ParseInt(m[2])
		if skills[m[1]] < float64(need) {
			return false
		}
	}
	return true
}

// strings of a tooltip table (non-strings are left out).
func tipLines(rec *sv.Table) []string {
	var out []string
	for _, v := range rec.Tab("tooltip").List() {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// LearnRatings is upgrades.learn_ratings: rating per 1 % hit and crit, measured from items whose tooltip
// spells out the percentage (the median ratio).
func LearnRatings(items []*sv.Table) map[string]float64 {
	out := map[string]float64{"hit": DefaultRatings["hit"], "crit": DefaultRatings["crit"]}
	for _, pl := range PctLine {
		var ratios []float64
		for _, it := range items {
			rating, _ := it.Tab("stats").Num("ITEM_MOD_" + strings.ToUpper(pl.Kind) + "_RATING_SHORT")
			pct := tooltipPct(it, pl.Re)
			if rating != 0 && pct != 0 {
				ratios = append(ratios, rating/pct)
			}
		}
		if len(ratios) > 0 {
			sort.Float64s(ratios)
			out[pl.Kind] = ratios[len(ratios)/2]
		}
	}
	return out
}

// ItemExport is the item export of "/wtf items" (upgrades.load_item_export): the account-wide
// WoWToolsForever_Items with the newest "updated", else the older per-character copy.
func ItemExport(wow string, char *sv.Table) *sv.Table {
	var best *sv.Table
	bestUpdated := 0.0
	for _, path := range game.AccountFiles(wow, "WoWToolsForever.lua") {
		data, err := sv.LoadTable(path, "WoWToolsForever_Items")
		if err != nil || data == nil {
			continue
		}
		u, _ := data.Num("updated")
		if u > bestUpdated {
			best, bestUpdated = data, u
		}
	}
	if best == nil || !sv.Truthy(best.Get("items")) {
		best = char.Tab("itemInfo")
	}
	if best == nil {
		best = sv.NewTable()
	}
	return best
}

// Owned is one piece of gear the account already has.
type Owned struct {
	ID     int64
	It     *sv.Table
	Source string // "bags" or "bank"
	Owner  string // another character's name, "" for the character itself
}

var (
	ownedClass = []string{"classID", "subclassID", "equipLoc"}
	ownedFill  = []string{"stats", "tooltip", "tipColors", "reqLevel", "quality"}
)

func records(v any) []*sv.Table {
	t, ok := v.(*sv.Table)
	if !ok {
		return nil
	}
	return t.Tables()
}

// carried is upgrades.carried: (source, item) for everything in a character's bags and bank.
func carried(char *sv.Table, yield func(source string, it *sv.Table)) {
	for _, bag := range records(char.Get("bags")) {
		for _, it := range records(bag.Get("items")) {
			yield("bags", it)
		}
	}
	for _, bag := range records(char.Tab("bank").Get("containers")) {
		for _, it := range records(bag.Get("items")) {
			yield("bank", it)
		}
	}
}

func copyTable(t *sv.Table) *sv.Table {
	out := sv.NewTable()
	for i, k := range t.Keys() {
		out.Set(k, t.Values()[i])
	}
	return out
}

type ownedKey struct {
	id   int64
	name any
}

// OwnedGear is upgrades.owned_gear: armor and weapons the character could put on without buying them, as
// (items with what they do, items the snapshots only name). The same item is listed once, the character's own
// copy first. export maps item IDs to the item export's records.
func OwnedGear(ownFile string, char *sv.Table, export map[int64]*sv.Table) (found, unknown []Owned) {
	seen := map[ownedKey]bool{}
	take := func(c *sv.Table, owner string) {
		carried(c, func(source string, it *sv.Table) {
			if !sv.Truthy(it.Get("id")) || (owner != "" && sv.Truthy(it.Get("bound"))) {
				return
			}
			idf, _ := sv.Num(it.Get("id"))
			if s, ok := it.Get("id").(string); ok {
				n, _ := pyfmt.ParseInt(s)
				idf = float64(n)
			}
			iid := int64(idf)
			known := export[iid]
			rec := copyTable(it)
			for _, k := range ownedClass {
				if rec.Get(k) == nil {
					rec.Set(k, known.Get(k))
				}
			}
			loc, _ := rec.Get("equipLoc").(string)
			key := ownedKey{iid, rec.Get("name")}
			if _, ok := SlotOf[loc]; !ok || seen[key] {
				return
			}
			seen[key] = true
			if known.Get("name") == rec.Get("name") {
				for _, k := range ownedFill {
					if rec.Get(k) == nil {
						rec.Set(k, known.Get(k))
					}
				}
			}
			entry := Owned{ID: iid, It: rec, Source: source, Owner: owner}
			if rec.Get("classID") == nil || !(sv.Truthy(rec.Get("stats")) || sv.Truthy(rec.Get("tooltip"))) {
				unknown = append(unknown, entry)
			} else {
				found = append(found, entry)
			}
		})
	}
	take(char, "")
	own := absPath(ownFile)
	realm := filepath.Dir(filepath.Dir(filepath.Dir(own)))
	others := game.Glob(realm, "*", "SavedVariables", "WoWToolsForever.lua")
	sort.Strings(others)
	for _, path := range others {
		if absPath(path) == own {
			continue
		}
		other, err := LoadChar(path)
		if err != nil || other == nil {
			other = sv.NewTable()
		}
		name := other.Tab("character").Str("name")
		if name == "" {
			name = filepath.Base(filepath.Dir(filepath.Dir(path)))
		}
		take(other, name)
	}
	return found, unknown
}

// absPath is os.path.abspath.
func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// WhereText is upgrades.where_text: where an owned item is, as a sentence part.
func WhereText(source, owner string) string {
	switch source {
	case "bags":
		if owner != "" {
			return i18n.T("in {name}'s bags", "name", owner)
		}
		return i18n.T("in your bags")
	case "bank":
		if owner != "" {
			return i18n.T("in {name}'s bank", "name", owner)
		}
		return i18n.T("in your bank")
	}
	return ""
}

// itoa is str() of an int.
func itoa(n int64) string { return strconv.FormatInt(n, 10) }
