// Package icons is scripts/icons.py: item icons for the desktop app. Item ID -> icon FileDataID from the DB2
// tables (ItemModifiedAppearance -> ItemAppearance.DefaultIconFileDataID, Item.IconFileDataID as fallback:
// newer Forever items have 0 there), the icon itself a BLP from wago.tools' CASC endpoint, stored as PNG in
// <cache>/icons/<FileDataID>.png. Everything is cached, so each table and icon is downloaded once.
package icons

import (
	"bytes"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/tables"
)

// QuestionMark is INV_Misc_QuestionMark, the icon of an item without one.
const QuestionMark = 134400

const userAgent = "Mozilla/5.0 (wow-forever-tools)"

// baseURL is wago.tools; tests replace it.
var baseURL = "https://wago.tools"

var client = &http.Client{Timeout: 30 * time.Second}

// Dir is where the PNGs are: <cache>/icons.
func Dir(cache string) string { return game.Join(cache, "icons") }

func intOf(s string) int64 { n, _ := pyfmt.ParseInt(s); return n }

// Map is icons.icon_map: item ID -> icon FileDataID for the requested items.
func Map(cache string, ids map[int64]bool, build string) (map[int64]int64, error) {
	out := map[int64]int64{}
	type pick struct{ modifier, appearance int64 }
	appearanceOf := map[int64]pick{} // modifier 0 wins, then the lowest appearance
	t, err := tables.Read(cache, "ItemModifiedAppearance", build, "")
	if err != nil {
		return nil, err
	}
	if t != nil {
		for _, r := range t.Rows() {
			id := intOf(r.Str("ItemID"))
			if !ids[id] {
				continue
			}
			k := pick{intOf(r.Str("ItemAppearanceModifierID")), intOf(r.Str("ItemAppearanceID"))}
			if old, ok := appearanceOf[id]; !ok || k.modifier < old.modifier ||
				k.modifier == old.modifier && k.appearance < old.appearance {
				appearanceOf[id] = k
			}
		}
	}
	wanted := map[int64]bool{}
	for _, p := range appearanceOf {
		wanted[p.appearance] = true
	}
	iconOf := map[int64]int64{}
	if t, err = tables.Read(cache, "ItemAppearance", build, ""); err != nil {
		return nil, err
	}
	if t != nil {
		for _, r := range t.Rows() {
			if id := intOf(r.Str("ID")); wanted[id] {
				iconOf[id] = intOf(r.Str("DefaultIconFileDataID"))
			}
		}
	}
	for id, p := range appearanceOf {
		if icon := iconOf[p.appearance]; icon != 0 {
			out[id] = icon
		}
	}
	missing := false
	for id := range ids {
		if _, ok := out[id]; !ok {
			missing = true
		}
	}
	if missing {
		if t, err = tables.Read(cache, "Item", build, ""); err != nil {
			return nil, err
		}
		if t != nil {
			for _, r := range t.Rows() {
				id := intOf(r.Str("ID"))
				if _, have := out[id]; ids[id] && !have {
					if icon := intOf(r.Str("IconFileDataID")); icon != 0 {
						out[id] = icon
					}
				}
			}
		}
	}
	return out, nil
}

// Fetch is icons.fetch_icon: downloads one icon BLP and stores it as PNG; true if the PNG exists afterwards.
func Fetch(cache string, fdid int64) bool {
	path := game.Join(Dir(cache), fmt.Sprintf("%d.png", fdid))
	if _, err := os.Stat(path); err == nil {
		return true
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/casc/%d?download", baseURL, fdid), nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		return false
	}
	img, err := DecodeBLP(data)
	if err != nil {
		return false
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return false
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return false
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return false
	}
	return true
}

// Resolve is icons.resolve: {"icons": {itemId: FileDataID or null}, "fallback", "dir"}; downloads what is
// not cached yet, eight at a time.
func Resolve(wow string, itemIDs []int64) (*pyjson.Obj, error) {
	cache := tables.CacheDir()
	ids := map[int64]bool{}
	for _, id := range itemIDs {
		if id > 0 {
			ids[id] = true
		}
	}
	mapping := map[int64]int64{}
	if len(ids) > 0 {
		var err error
		if mapping, err = Map(cache, ids, tables.ClientBuild(wow)); err != nil {
			return nil, err
		}
	}
	_ = os.MkdirAll(Dir(cache), 0o755)
	need := map[int64]bool{QuestionMark: true}
	for _, icon := range mapping {
		need[icon] = true
	}
	ok := map[int64]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for icon := range need {
		wg.Add(1)
		go func(icon int64) {
			defer wg.Done()
			slots <- struct{}{}
			got := Fetch(cache, icon)
			<-slots
			mu.Lock()
			ok[icon] = got
			mu.Unlock()
		}(icon)
	}
	wg.Wait()
	sorted := make([]int64, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	out := pyjson.New()
	for _, id := range sorted {
		icon, have := mapping[id]
		if have && ok[icon] {
			out.Set(strconv.FormatInt(id, 10), icon)
		} else {
			out.Set(strconv.FormatInt(id, 10), nil)
		}
	}
	var fallback any
	if ok[QuestionMark] {
		fallback = int64(QuestionMark)
	}
	return pyjson.New("icons", out, "fallback", fallback, "dir", Dir(cache)), nil
}
