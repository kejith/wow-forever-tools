package writs

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/pycsv"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/internal/tables"
	"github.com/kejith/wow-forever-tools/sim/tools/upgrades"
)

// Reputation is the standing with the writ faction.
type Reputation struct {
	Name, Standing string
	Value, Max     int64
}

var (
	blockRe = regexp.MustCompile(`\{([^{}]*)\}`)
	idRe    = regexp.MustCompile(`\["id"\] = (\d+)`)
	countRe = regexp.MustCompile(`\["count"\] = (\d+)`)
	nameRe  = regexp.MustCompile(`\["name"\] = "([^"]*)"`)
	fieldRe = map[string]*regexp.Regexp{}
)

func init() {
	for _, k := range []string{"standing", "value", "max"} {
		fieldRe[k] = regexp.MustCompile(`\["` + k + `"\] = "?([^",]*)"?,`)
	}
}

// LoadOwned is writs.load_char: the inventory counts (every innermost table with an id, a count and a link:
// bags and bank) and the standing with the writ faction, read from the text of WoWMCP.lua with the same
// regular expressions as Python (not through the SavedVariables reader).
func LoadOwned(path string) (map[int64]int64, *Reputation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	// open(…, encoding='utf-8', errors='replace').read(): invalid bytes replaced, universal newlines.
	text := sv.DecodeReplace(data)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	owned := map[int64]int64{}
	var rep *Reputation
	for _, m := range blockRe.FindAllStringSubmatch(text, -1) {
		block := m[1]
		idm := idRe.FindStringSubmatch(block)
		if idm == nil {
			continue
		}
		id, ok := pyfmt.ParseInt(idm[1])
		if !ok {
			return nil, nil, intError(idm[1])
		}
		if cnt := countRe.FindStringSubmatch(block); cnt != nil && strings.Contains(block, `["link"]`) {
			n, ok := pyfmt.ParseInt(cnt[1])
			if !ok {
				return nil, nil, intError(cnt[1])
			}
			owned[id] += n
		}
		name := nameRe.FindStringSubmatch(block)
		if name != nil && strings.Contains(block, `["standing"]`) && Factions[id] {
			g := func(k string) (string, error) {
				f := fieldRe[k].FindStringSubmatch(block)
				if f == nil { // Python: AttributeError on None.group
					return "", fmt.Errorf("'NoneType' object has no attribute 'group'")
				}
				return f[1], nil
			}
			standing, err := g("standing")
			if err != nil {
				return nil, nil, err
			}
			r := &Reputation{Name: name[1], Standing: standing}
			for _, k := range []string{"value", "max"} {
				s, err := g(k)
				if err != nil {
					return nil, nil, err
				}
				n, ok := pyfmt.ParseInt(s)
				if !ok {
					return nil, nil, intError(s)
				}
				if k == "value" {
					r.Value = n
				} else {
					r.Max = n
				}
			}
			rep = r
		}
	}
	return owned, rep, nil
}

func intError(s string) error {
	return fmt.Errorf("invalid literal for int() with base 10: '%s'", s)
}

// Extras are writs.load_extras: the payouts the addon measured (account-wide) and the character's level
// and Merchant's Favor (nil when unknown).
type Extras struct {
	Pouches    *sv.Table // pouch item -> {n, sum, min, max, …}
	Crates     *sv.Table // sealed crate -> {n, sum, …}
	WritQuests *sv.Table // quest -> {pouch, money, n, …}
	Level      any
	Favor      any
}

// LoadExtras is writs.load_extras: every account's WoWMCP_Account.payouts merged (a later account's entry
// wins), plus level and favor from the character's snapshot (charPath "" for none).
func LoadExtras(wow, charPath string) (*Extras, error) {
	e := &Extras{Pouches: sv.NewTable(), Crates: sv.NewTable(), WritQuests: sv.NewTable()}
	for _, path := range game.AccountFiles(wow, "WoWMCP.lua") {
		acc, err := sv.Load(path, "WoWMCP_Account")
		if err != nil {
			return nil, err
		}
		var p *sv.Table
		if t, ok := acc.(*sv.Table); ok && sv.Truthy(t) {
			p, _ = t.Get("payouts").(*sv.Table)
		}
		for kind, into := range map[string]*sv.Table{"pouches": e.Pouches, "crates": e.Crates,
			"writQuests": e.WritQuests} {
			if part := p.Tab(kind); sv.Truthy(part) {
				for i, k := range part.Keys() {
					into.Set(k, part.Values()[i])
				}
			}
		}
	}
	if charPath != "" {
		c, err := upgrades.LoadChar(charPath)
		if err != nil {
			return nil, err
		}
		e.Level = c.Tab("character").Get("level")
		if cur, ok := c.Tab("currencies").Get(int64(FavorCurrency)).(*sv.Table); ok && !cur.IsList() {
			e.Favor = cur.Get("quantity")
		}
	}
	return e, nil
}

// QuestOf maps a writ item to the quest it starts (ItemSparse.StartQuestID), in the order of the table it
// was read from.
type QuestOf struct {
	Writs []int64
	Quest map[int64]int64
}

// Add appends a writ's quest.
func (q *QuestOf) Add(writ, quest int64) {
	if q.Quest == nil {
		q.Quest = map[int64]int64{}
	}
	if _, ok := q.Quest[writ]; !ok {
		q.Writs = append(q.Writs, writ)
	}
	q.Quest[writ] = quest
}

var (
	questMu    sync.Mutex
	questCache = map[string]*QuestOf{}
)

// WritQuests is writs.writ_quests: writ item -> quest from the newest cached ItemSparse table (by
// modification time, any build and locale) in the cache folder ("" for tables.CacheDir()); empty without one.
func WritQuests(cache string) (*QuestOf, error) {
	if cache == "" {
		cache = tables.CacheDir()
	}
	files := game.Glob(cache, "ItemSparse-*.csv")
	if len(files) == 0 {
		return &QuestOf{}, nil
	}
	times := map[string]float64{}
	for _, f := range files {
		times[f], _ = game.Mtime(f)
	}
	sort.SliceStable(files, func(i, j int) bool { return times[files[i]] < times[files[j]] })
	path := files[len(files)-1]
	key := fmt.Sprintf("%s|%v", path, times[path])
	questMu.Lock()
	q := questCache[key]
	questMu.Unlock()
	if q != nil {
		return q, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	wanted := map[int64]bool{}
	for _, w := range WritList {
		wanted[w.ID] = true
	}
	if q, err = questsFromCSV(string(data), wanted); err != nil {
		return nil, err
	}
	questMu.Lock()
	questCache[key] = q
	questMu.Unlock()
	return q, nil
}

// questsFromCSV is the loop of writ_quests over csv.DictReader of an ItemSparse table: for every row of a
// wanted writ, its StartQuestID when that is a number other than 0. Every row's ID must be an integer, as
// int(r['ID']) demands.
//
// The table is about 5 MB; splitting every field of it, as tables.Parse does, takes most of the command's
// time. A line without a quote that starts a record is exactly its comma-separated fields in Python's csv
// module, so such lines are only cut where the two columns are; every other line goes through the csv state
// machine of pycsv (a quoted field may go on into the next line).
func questsFromCSV(text string, wanted map[int64]bool) (*QuestOf, error) {
	q := &QuestOf{Quest: map[int64]int64{}}
	var header []string
	idCol, questCol := -1, -1
	row := func(get func(i int) (string, bool), empty bool) error {
		if header == nil {
			return nil
		}
		if empty { // DictReader skips the rows that are []
			return nil
		}
		idText, _ := get(idCol)
		id, ok := pyfmt.ParseInt(idText)
		if !ok {
			return intError(idText)
		}
		if !wanted[id] {
			return nil
		}
		s, _ := get(questCol)
		if s == "" {
			s = "0"
		}
		if !isDigits(s) {
			return nil
		}
		if n, ok := pyfmt.ParseInt(s); ok && n != 0 {
			q.Add(id, n)
		}
		return nil
	}
	record := func(fields []string) error {
		if header == nil {
			header = fields
			if header == nil {
				header = []string{}
			}
			for i, h := range header {
				// DictReader's dict: a repeated column name keeps its last field.
				switch h {
				case "ID":
					idCol = i
				case "StartQuestID":
					questCol = i
				}
			}
			return nil
		}
		return row(func(i int) (string, bool) {
			if i < 0 || i >= len(fields) {
				return "", false
			}
			return fields[i], true
		}, len(fields) == 0)
	}
	var r pycsv.Reader
	pending := false
	for _, line := range pycsv.SplitLines(text) {
		if !pending && !strings.Contains(line, `"`) {
			body := strings.TrimRight(line, "\r\n")
			if header == nil {
				var fields []string
				if body != "" {
					fields = strings.Split(body, ",")
				}
				if err := record(fields); err != nil {
					return nil, err
				}
				continue
			}
			if err := row(func(i int) (string, bool) { return nthField(body, i) }, body == ""); err != nil {
				return nil, err
			}
			continue
		}
		fields, ok := r.Feed(line)
		pending = !ok
		if ok {
			if err := record(fields); err != nil {
				return nil, err
			}
		}
	}
	if fields, ok := r.End(); ok {
		if err := record(fields); err != nil {
			return nil, err
		}
	}
	return q, nil
}

// nthField is field i of a line split at its commas, false if the line has fewer fields.
func nthField(line string, i int) (string, bool) {
	if i < 0 {
		return "", false
	}
	for ; i > 0; i-- {
		c := strings.IndexByte(line, ',')
		if c < 0 {
			return "", false
		}
		line = line[c+1:]
	}
	if c := strings.IndexByte(line, ','); c >= 0 {
		return line[:c], true
	}
	return line, true
}

// isDigits is str.isdigit for ASCII text: not empty, digits only.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
