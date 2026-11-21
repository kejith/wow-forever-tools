package pricehistory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/tables"
)

// NamesVersion is NAMES_VERSION: the format of the names cache. Python and Go share the file.
const NamesVersion = 1

// Names is item ID (as text) -> [name or nil, quality, class ID] from ItemSparse and Item.
type Names struct {
	raw   map[string]json.RawMessage // read from the cache, decoded on demand
	built map[string][]any
	keys  []string // the order of built (for the cache file)
}

// Get is (names.get(key) or [None, None, None])[:3].
func (n *Names) Get(key string) [3]any {
	var e []any
	if n != nil && n.built != nil {
		e = n.built[key]
	} else if n != nil {
		if raw, ok := n.raw[key]; ok {
			e, _ = decode(raw).([]any)
		}
	}
	var out [3]any
	copy(out[:], e)
	return out
}

// Len is the number of items.
func (n *Names) Len() int {
	if n.built != nil {
		return len(n.built)
	}
	return len(n.raw)
}

// decode is json.loads of one value, numbers as Python reads them (an int stays an int).
func decode(raw json.RawMessage) any {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return nil
	}
	return pyValue(v)
}

func pyValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	case []any:
		for i := range x {
			x[i] = pyValue(x[i])
		}
	}
	return v
}

// cacheFile is the top level of the cache file.
type cacheFile map[string]json.RawMessage

func readCache(path string) (cacheFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f cacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// itemsOf is data["items"] (or data.get("items") or {}) as Names.
func itemsOf(f cacheFile) *Names {
	n := &Names{raw: map[string]json.RawMessage{}}
	if raw, ok := f["items"]; ok {
		_ = json.Unmarshal(raw, &n.raw) // anything but an object counts as none
	}
	return n
}

// versionIs is data.get("version") == NAMES_VERSION (Python's 1 == 1.0 == True).
func (f cacheFile) versionIs(v int) bool {
	switch x := decode(f["version"]).(type) {
	case int64:
		return x == int64(v)
	case float64:
		return x == float64(v)
	case bool:
		return x == (v == 1)
	}
	return false
}

// LoadNames is pricehistory.load_names: the names of a build and locale, from the cache
// <cache>/itemnames-<build>-<locale>.json or else built from the tables Item and ItemSparse and cached.
// Without a build the newest cached copy is used.
func LoadNames(cache, build, locale string) (*Names, error) {
	path := ""
	if build != "" {
		path = filepath.Join(cache, fmt.Sprintf("itemnames-%s-%s.json", build, locale))
		if _, err := os.Stat(path); err == nil {
			f, err := readCache(path)
			if err != nil {
				return nil, err
			}
			if f.versionIs(NamesVersion) {
				if _, ok := f["items"]; !ok {
					return nil, errors.New("'items'") // Python's KeyError
				}
				return itemsOf(f), nil
			}
		}
	}
	if build == "" {
		for _, pattern := range []string{"itemnames-*-" + locale + ".json", "itemnames-*.json"} {
			cached := game.Glob(cache, pattern)
			if len(cached) == 0 {
				continue
			}
			times := map[string]float64{}
			for _, p := range cached {
				times[p], _ = game.Mtime(p)
			}
			sort.SliceStable(cached, func(i, j int) bool { return times[cached[i]] < times[cached[j]] })
			f, err := readCache(cached[len(cached)-1])
			if err != nil {
				return nil, err
			}
			return itemsOf(f), nil
		}
	}

	n := &Names{built: map[string][]any{}}
	add := func(id string, e []any) []any {
		if old, ok := n.built[id]; ok {
			return old
		}
		n.built[id] = e
		n.keys = append(n.keys, id)
		return e
	}
	item, err := tables.Read(cache, "Item", build, "")
	if err != nil {
		return nil, err
	}
	if item != nil {
		for _, r := range item.Rows() {
			cls := int64(0)
			if v := r.Str("ClassID"); v != "" {
				c, ok := pyfmt.ParseInt(v)
				if !ok {
					return nil, fmt.Errorf("invalid literal for int() with base 10: '%s'", v)
				}
				cls = c
			}
			id := r.Str("ID")
			e := add(id, []any{nil, nil, cls})
			e[0], e[1], e[2] = nil, nil, cls // a row of the same ID again replaces the entry
		}
	}
	sparse, err := tables.Read(cache, "ItemSparse", build, locale)
	if err != nil {
		return nil, err
	}
	if sparse != nil {
		for _, r := range sparse.Rows() {
			e := add(r.Str("ID"), []any{nil, nil, nil})
			e[0] = nil
			if name := r.Str("Display_lang"); name != "" {
				e[0] = name
			}
			e[1] = nil
			if q := r.Str("OverallQualityID"); isDigit(q) {
				e[1], _ = pyfmt.ParseInt(q)
			}
		}
	}
	if path != "" && len(n.keys) > 0 {
		writeCache(cache, path, n)
	}
	return n, nil
}

// writeCache writes the names as json.dump({"version": …, "items": …}, ensure_ascii=False) does, in one step
// (several bridge processes may build the same file at once). A failure leaves the cache as it was.
func writeCache(cache, path string, n *Names) {
	items := pyjson.New()
	for _, k := range n.keys {
		items.Set(k, n.built[k])
	}
	data, err := pyjson.Encode(pyjson.New("version", NamesVersion, "items", items))
	if err != nil {
		return
	}
	_ = os.MkdirAll(cache, 0o755)
	part := fmt.Sprintf("%s.%d.part", path, os.Getpid())
	if os.WriteFile(part, data, 0o644) != nil {
		_ = os.Remove(part)
		return
	}
	if os.Rename(part, path) != nil {
		_ = os.Remove(part)
	}
}
