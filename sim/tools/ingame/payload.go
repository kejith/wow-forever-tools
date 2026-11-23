package ingame

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/tools/consumables"
	"github.com/kejith/wow-forever-tools/sim/tools/enginebundle"
	"github.com/kejith/wow-forever-tools/sim/tools/recipes"
	"github.com/kejith/wow-forever-tools/sim/tools/upgrades"
	"github.com/kejith/wow-forever-tools/sim/tools/writs"
)

const (
	// Version is the format of the data file (WoWMCP_AppData.version).
	Version = 1
	// DataFile is the file in the addon folder.
	DataFile   = "AppData.lua"
	global     = "WoWMCP_AppData"
	defaultLoc = "enUS"
)

// The four calculations, as ingame.py calls them; the tests replace them with hand-made results.
var (
	computeUpgrades = func(wow, folder string, prices *sv.Table) (any, error) {
		return upgrades.Compute(wow, folder, prices, upgrades.Options{Preview: 4, Top: 3, Sort: "gain",
			Engine: "auto", FightSeconds: 60, TargetDelta: -1, KillsPerMinute: -1})
	}
	computeWrits = func(wow, folder string, prices *sv.Table) (any, error) {
		return writs.Compute(wow, folder, 3, false, prices)
	}
	computeRecipes = func(wow, folder string, prices *sv.Table) (any, error) {
		return recipes.Compute(wow, folder, "", 0, prices)
	}
	computeConsumables = func(wow, folder string) (any, error) {
		return consumables.Compute(wow, folder, 3, false, "off", 60, -1, nil, consumables.Set{})
	}
	loadPrices = writs.LoadPrices
)

// ---------- Payload ----------

// Char is what the game gets for one character: {level, lists, tips, vendor, errors?}.
type Char struct {
	Level  any
	Lists  []*pyjson.Obj
	Tips   *intDict
	Vendor *intDict
	Errors *pyjson.Obj // nil without one
}

func (c *Char) value() *pyjson.Obj {
	lists := make([]any, len(c.Lists))
	for i, l := range c.Lists {
		lists[i] = l
	}
	out := pyjson.New("level", c.Level, "lists", lists, "tips", c.Tips, "vendor", c.Vendor)
	if c.Errors != nil {
		out.Set("errors", c.Errors)
	}
	return out
}

// Payload is WoWMCP_AppData: {version, generated, locale, scanDate, chars, hash}.
type Payload struct {
	Generated int64
	Locale    string
	ScanDate  string // "" = none
	Names     []string
	Chars     map[string]*Char
	Hash      string
}

// value is the payload as ingame.compute returns it, in its key order.
func (p *Payload) value() *pyjson.Obj {
	chars := pyjson.New()
	for _, n := range p.Names {
		chars.Set(n, p.Chars[n].value())
	}
	var scan any
	if p.ScanDate != "" {
		scan = p.ScanDate
	}
	out := pyjson.New("version", int64(Version), "generated", p.Generated, "locale", p.Locale, "scanDate", scan,
		"chars", chars)
	if p.Hash != "" {
		out.Set("hash", p.Hash)
	}
	return out
}

// Obj is the payload as ingame.compute returns it (what `ingame --json` prints), tips and vendor hints as
// objects in the order they were added.
func (p *Payload) Obj() *pyjson.Obj {
	dict := func(d *intDict) *pyjson.Obj {
		o := pyjson.New()
		for _, k := range d.keys {
			o.Set(strconv.FormatInt(k, 10), d.vals[k])
		}
		return o
	}
	chars := pyjson.New()
	for _, n := range p.Names {
		c := p.Chars[n]
		chars.Set(n, c.value().Set("tips", dict(c.Tips)).Set("vendor", dict(c.Vendor)))
	}
	return p.value().Set("chars", chars)
}

// characterData is (first name, data) for one character export; "" when the export cannot be read.
func characterData(wow, path string, prices *sv.Table) (name string, data *Char) {
	defer func() {
		if recover() != nil { // a half-written file while WoW is saving
			name, data = "", nil
		}
	}()
	c, err := upgrades.LoadChar(path)
	if err != nil {
		return "", nil
	}
	who := c.Tab("character")
	folder := game.CharKey(path)
	out := &Char{Level: get(who, "level"), Tips: newIntDict(), Vendor: newIntDict()}
	errs := pyjson.New()
	part := func(key string, build func() ([]*pyjson.Obj, error)) {
		made, err := build()
		if err != nil {
			errs.Set(key, err.Error())
			return
		}
		for _, one := range made {
			if one != nil {
				out.Lists = append(out.Lists, one)
			}
		}
	}
	classFile, _ := get(who, "classFile").(string)
	if enginebundle.Supported(classFile) {
		part("upgrades", func() ([]*pyjson.Obj, error) {
			r, err := computeUpgrades(wow, folder, prices)
			if err != nil {
				return nil, err
			}
			return []*pyjson.Obj{upgradeList(r, out.Tips)}, nil
		})
	}
	part("writs", func() ([]*pyjson.Obj, error) {
		r, err := computeWrits(wow, folder, prices)
		if err != nil {
			return nil, err
		}
		return []*pyjson.Obj{writList(r, out.Tips), crateList(r, out.Tips)}, nil
	})
	part("recipes", func() ([]*pyjson.Obj, error) {
		r, err := computeRecipes(wow, folder, prices)
		if err != nil {
			return nil, err
		}
		return []*pyjson.Obj{recipeList(r, out.Tips, out.Vendor)}, nil
	})
	part("consumables", func() ([]*pyjson.Obj, error) {
		r, err := computeConsumables(wow, folder)
		if err != nil {
			return nil, err
		}
		return []*pyjson.Obj{consumableList(r, out.Tips)}, nil
	})
	if len(errs.Keys()) > 0 {
		out.Errors = errs
	}
	n, _ := get(who, "name").(string)
	return n, out
}

// Compute is ingame.compute: the payload for every exported character (or the ones whose folder name
// contains char). The texts are in the client's language, whatever the caller's is: they are read in the
// game.
func Compute(wow, char string) *Payload {
	locale := i18n.ClientLocale(wow)
	previous := i18n.Language()
	i18n.SetLanguage(i18n.LanguageOf(locale), "")
	defer i18n.SetLanguage(previous, "")
	prices, err := loadPrices(wow)
	if err != nil {
		prices = nil // every part then reports the missing scan itself
	}
	p := &Payload{Locale: locale, Chars: map[string]*Char{}}
	if p.Locale == "" {
		p.Locale = defaultLoc
	}
	if prices != nil && prices.Len() > 0 {
		if day, ok := sv.ScanDate(prices); ok {
			p.ScanDate = day.Format("2006-01-02")
		}
	}
	for _, path := range game.FindChars(wow, char) {
		name, data := characterData(wow, path, prices)
		// The newest export wins when two folders hold the same first name.
		if _, seen := p.Chars[name]; name != "" && !seen {
			p.Names = append(p.Names, name)
			p.Chars[name] = data
		}
	}
	p.Generated = time.Now().Unix()
	p.Hash = Digest(p)
	return p
}

// Digest is ingame.digest: the hash of everything but the time, so unchanged data is not written (and
// reloaded) again. It is SHA-1 over json.dumps(sort_keys=True, ensure_ascii=False), as Python writes it.
func Digest(p *Payload) string {
	rest := pyjson.New()
	v := p.value()
	for _, k := range v.Keys() {
		if k != "generated" && k != "hash" {
			rest.Set(k, v.Get(k))
		}
	}
	var b bytes.Buffer
	if err := sortedJSON(&b, rest); err != nil {
		panic(err)
	}
	sum := sha1.Sum(b.Bytes())
	return hex.EncodeToString(sum[:])[:16]
}

// sortedJSON writes json.dumps(v, sort_keys=True, ensure_ascii=False).
func sortedJSON(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case *pyjson.Obj:
		keys := append([]string(nil), x.Keys()...)
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			key, _ := pyjson.Encode(k)
			b.Write(key)
			b.WriteString(": ")
			if err := sortedJSON(b, x.Get(k)); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case *intDict:
		b.WriteByte('{')
		for i, k := range x.sortedKeys() {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "\"%d\": ", k)
			if err := sortedJSON(b, x.vals[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case *sv.Table:
		if x != nil && !x.IsList() {
			return fmt.Errorf("cannot hash a table with keys")
		}
		return sortedJSON(b, items(x))
	case nil, bool, string, int, int64, float64:
		data, err := pyjson.Encode(v)
		if err != nil {
			return err
		}
		b.Write(data)
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Slice {
			return fmt.Errorf("cannot hash %T", v)
		}
		b.WriteByte('[')
		for i, item := range items(v) {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := sortedJSON(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	}
	return nil
}

// ---------- Lua ----------

// luaString is a Lua string literal. Everything outside printable ASCII is written as a decimal escape of its
// UTF-8 bytes, so no input can end the string or reach the file as code.
func luaString(text string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c >= 32 && c < 127:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "\\%03d", c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

var (
	identifier  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	luaKeywords = map[string]bool{"and": true, "break": true, "do": true, "else": true, "elseif": true,
		"end": true, "false": true, "for": true, "function": true, "goto": true, "if": true, "in": true,
		"local": true, "nil": true, "not": true, "or": true, "repeat": true, "return": true, "then": true,
		"true": true, "until": true, "while": true}
)

func luaKey(k string) string {
	if identifier.MatchString(k) && !luaKeywords[k] {
		return k
	}
	return "[" + luaString(k) + "]"
}

// isContainer: a dict or a list (ingame.lua_value's isinstance(v, (dict, list, tuple))).
func isContainer(v any) bool {
	switch v.(type) {
	case *pyjson.Obj, *intDict, map[string]any, *sv.Table:
		return true
	case nil, string:
		return false
	}
	k := reflect.ValueOf(v).Kind()
	return k == reflect.Slice || k == reflect.Array
}

// isScalar: a str, int or float, not a bool.
func isScalar(v any) bool {
	switch v.(type) {
	case string, int, int64, float64:
		return true
	}
	return false
}

// luaValue is ingame.lua_value: data as a Lua table constructor, nil entries left out.
func luaValue(v any, indent int) (string, error) {
	pad, inner := strings.Repeat("  ", indent), strings.Repeat("  ", indent+1)
	switch x := v.(type) {
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "", fmt.Errorf("not a finite number")
		}
		if x == math.Trunc(x) {
			return strconv.FormatFloat(x, 'f', 0, 64), nil
		}
		return pyfmt.Float(pyfmt.Round(x, 4)), nil
	case string:
		return luaString(x), nil
	case *pyjson.Obj, *intDict:
		var keys []string
		var vals []any
		if o, ok := x.(*pyjson.Obj); ok {
			for _, k := range o.Keys() {
				keys = append(keys, luaKey(k))
				vals = append(vals, o.Get(k))
			}
		} else {
			d := x.(*intDict)
			for _, k := range d.keys {
				keys = append(keys, fmt.Sprintf("[%d]", k))
				vals = append(vals, d.vals[k])
			}
		}
		var parts []string
		nested, size := false, 0
		for i, val := range vals {
			if isContainer(val) {
				nested = true
			}
			if val == nil {
				continue
			}
			text, err := luaValue(val, indent+1)
			if err != nil {
				return "", err
			}
			part := keys[i] + " = " + text
			parts = append(parts, part)
			size += len(part)
		}
		return join(parts, !nested && size < 100, pad, inner), nil
	}
	if v == nil || !isContainer(v) {
		return "", fmt.Errorf("cannot write %T as Lua", v)
	}
	var parts []string
	flat, size := true, 0
	for _, item := range items(v) {
		if !isScalar(item) {
			flat = false
		}
		if item == nil {
			continue
		}
		text, err := luaValue(item, indent+1)
		if err != nil {
			return "", err
		}
		parts = append(parts, text)
		size += len(text)
	}
	return join(parts, flat && size < 90, pad, inner), nil
}

func join(parts []string, oneLine bool, pad, inner string) string {
	if len(parts) == 0 {
		return "{}"
	}
	if oneLine {
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	var b strings.Builder
	b.WriteString("{\n")
	for _, p := range parts {
		b.WriteString(inner + p + ",\n")
	}
	b.WriteString(pad + "}")
	return b.String()
}

const note = "-- Written by the WoW Forever Tools desktop app (github.com/kejith/wow-forever-tools). The client reads this\n" +
	"-- file on /reload; the WoWMCP addon shows it (/wowmcp show). Do not edit: the app overwrites it.\n"

// ToLua is the data file. Time and hash come first: FileState reads them from the head of the file.
func ToLua(p *Payload) (string, error) {
	v := p.value()
	ordered := pyjson.New()
	for _, k := range []string{"version", "generated", "hash"} {
		if val := v.Get(k); val != nil || hasKey(v, k) {
			ordered.Set(k, val)
		}
	}
	for _, k := range v.Keys() {
		ordered.Set(k, v.Get(k))
	}
	text, err := luaValue(ordered, 0)
	if err != nil {
		return "", err
	}
	return note + global + " = " + text + "\n", nil
}

func hasKey(o *pyjson.Obj, k string) bool {
	for _, key := range o.Keys() {
		if key == k {
			return true
		}
	}
	return false
}

// ---------- File ----------

func addonDir(wow string) string { return game.Join(wow, "Interface", "AddOns", "WoWMCP") }

var tocVersion = regexp.MustCompile(`(?m)^## Version:\s*(\S+)`)

// splitLines is str.splitlines().
func splitLines(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			return true
		}
		return false
	})
}

// AddonState is ingame.addon_state: is the addon installed, and does its .toc load AppData.lua (1.7 and
// newer)?
func AddonState(wow string) *pyjson.Obj {
	data, err := os.ReadFile(game.Join(addonDir(wow), "WoWMCP.toc"))
	if err != nil {
		return pyjson.New("installed", false, "ready", false, "version", nil)
	}
	toc := strings.TrimPrefix(string(data), "\ufeff")
	ready := false
	for _, line := range splitLines(toc) {
		if strings.ReplaceAll(strings.TrimFunc(line, isSpace), "\\", "/") == DataFile {
			ready = true
		}
	}
	var version any
	if m := tocVersion.FindStringSubmatch(toc); m != nil {
		version = m[1]
	}
	return pyjson.New("installed", true, "ready", ready, "version", version)
}

var (
	headGenerated = regexp.MustCompile(`\bgenerated = (\d+)`)
	headHash      = regexp.MustCompile(`\bhash = "(\w+)"`)
)

// FileState is ingame.file_state: time and hash of the data file the game will read on the next /reload;
// nil without one.
func FileState(wow string) *pyjson.Obj {
	f, err := os.Open(game.Join(addonDir(wow), DataFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, 8000)
	n, _ := f.Read(buf)
	head := []rune(string(buf[:n]))
	if len(head) > 2000 {
		head = head[:2000]
	}
	text := string(head)
	g, h := headGenerated.FindStringSubmatch(text), headHash.FindStringSubmatch(text)
	if g == nil || h == nil {
		return nil
	}
	generated, _ := pyfmt.ParseInt(g[1])
	return pyjson.New("generated", generated, "hash", h[1])
}

// savedTime is when the game last wrote the addon's account data (on /reload or logout); nil if never.
func savedTime(wow string) any {
	var best any
	for _, path := range game.AccountFiles(wow, "WoWMCP.lua") {
		if t, err := game.Mtime(path); err == nil && (best == nil || t > best.(float64)) {
			best = t
		}
	}
	return best
}

// loadedState is what the game had loaded when it last saved: WoWMCP_Account.appData.
func loadedState(wow string) any {
	var newest *sv.Table
	for _, path := range game.AccountFiles(wow, "WoWMCP.lua") {
		account, err := sv.LoadTable(path, "WoWMCP_Account")
		if err != nil || account == nil {
			continue
		}
		seen := account.Tab("appData")
		if seen == nil || seen.IsList() && seen.Len() > 0 {
			continue
		}
		if newest == nil || num(or0(seen.Get("loaded"))) > num(or0(newest.Get("loaded"))) {
			newest = seen
		}
	}
	if newest == nil {
		return nil
	}
	return newest
}

// Write is ingame.write: the payload into the addon folder, unless the addon cannot read it or nothing changed.
func Write(wow string, p *Payload) (*pyjson.Obj, error) {
	state := AddonState(wow)
	path := game.Join(addonDir(wow), DataFile)
	if state.Get("installed") != true {
		return pyjson.New("written", false, "reason", i18n.T("The WoWMCP addon is not installed.")), nil
	}
	if state.Get("ready") != true {
		version, _ := state.Get("version").(string)
		if version == "" {
			version = "?"
		}
		return pyjson.New("written", false, "reason",
			i18n.T("WoWMCP {version} cannot show lists. Update the addon to 1.7 or newer.", "version", version)), nil
	}
	if old := FileState(wow); old != nil && old.Get("hash") == p.Hash {
		return pyjson.New("written", false, "unchanged", true, "file", path, "generated", old.Get("generated")), nil
	}
	text, err := ToLua(p)
	if err != nil {
		return nil, err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil { // in one step: the game never reads half a file
		os.Remove(tmp)
		return nil, err
	}
	return pyjson.New("written", true, "file", path, "generated", p.Generated), nil
}

// Summary is ingame.summary: what the app shows about a payload, per character the lists with their sizes.
func Summary(p *Payload) *pyjson.Obj {
	var chars []any
	for _, name := range p.Names {
		c := p.Chars[name]
		var lists []any
		for _, one := range c.Lists {
			rows, shop := 0, 0
			for _, s := range items(one.Get("sections")) {
				for _, r := range items(get(s, "rows")) {
					rows++
					if truthy(get(r, "shop")) {
						shop++
					}
				}
			}
			lists = append(lists, pyjson.New("key", one.Get("key"), "title", one.Get("title"), "rows", rows,
				"shop", shop))
		}
		if lists == nil {
			lists = []any{}
		}
		errs := c.Errors
		if errs == nil {
			errs = pyjson.New()
		}
		chars = append(chars, pyjson.New("name", name, "level", c.Level, "lists", lists, "tips", c.Tips.Len(),
			"vendor", c.Vendor.Len(), "errors", errs))
	}
	if chars == nil {
		chars = []any{}
	}
	var scan any
	if p.ScanDate != "" {
		scan = p.ScanDate
	}
	return pyjson.New("generated", p.Generated, "hash", p.Hash, "scanDate", scan, "locale", p.Locale,
		"chars", chars)
}

// Sync is ingame.sync: computes the payload and, with write, sends it to the game. The answer is the summary
// for the app.
func Sync(wow, char string, write bool) (*pyjson.Obj, error) {
	p := Compute(wow, char)
	out := Summary(p)
	out.Set("addon", AddonState(wow))
	var result any
	if write {
		r, err := Write(wow, p)
		if err != nil {
			return nil, err
		}
		result = r
	}
	out.Set("result", result)
	var file any
	if f := FileState(wow); f != nil {
		if t, err := game.Mtime(game.Join(addonDir(wow), DataFile)); err == nil {
			f.Set("modified", t)
		}
		file = f
	}
	out.Set("file", file)
	out.Set("saved", savedTime(wow))
	out.Set("loaded", loadedState(wow))
	return out, nil
}
