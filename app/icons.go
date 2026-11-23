package main

// Item icons: item ID -> FileDataID, remembered for the session. Unknown IDs go to the bridge in one batch,
// which also downloads the PNGs into <cache>/icons; IDs without an icon are not asked for again until the app
// restarts. The page loads the images as /wowicon/<FileDataID>.png, served by iconHandler.

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
)

type iconCache struct {
	mu       sync.Mutex // one bridge call at a time, so two pages asking for the same icons do not download them twice
	of       map[int64]any
	fallback any
}

func (a *App) resolveIcons(ids []any) any {
	a.icons.mu.Lock()
	defer a.icons.mu.Unlock()
	if a.icons.of == nil {
		a.icons.of = map[int64]any{}
	}
	var unknown []any
	seen := map[int64]bool{}
	for _, v := range ids {
		n, ok := jsNumber(v)
		id := int64(n)
		if !ok || id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if _, known := a.icons.of[id]; !known {
			unknown = append(unknown, float64(id))
		}
	}
	if len(unknown) > 0 {
		answer := a.runBridge("icons", map[string]any{"ids": unknown})
		raw, ok := answer.(json.RawMessage)
		if !ok {
			return answer // {"error": …}
		}
		var r struct {
			Error    string         `json:"error"`
			Icons    map[string]any `json:"icons"`
			Fallback any            `json:"fallback"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return errorAnswer(err.Error())
		}
		if r.Error != "" {
			return errorAnswer(r.Error)
		}
		for _, v := range unknown {
			id := int64(v.(float64))
			a.icons.of[id] = r.Icons[strconv.FormatInt(id, 10)]
		}
		if r.Fallback != nil {
			a.icons.fallback = r.Fallback
		}
	}
	icons := map[string]any{}
	for _, v := range ids {
		n, _ := jsNumber(v)
		icons[jsString(v)] = a.icons.of[int64(n)]
	}
	return map[string]any{"icons": icons, "fallback": a.icons.fallback}
}

var iconPath = regexp.MustCompile(`^/wowicon/(\d+\.png)$`)

// iconHandler serves the PNGs the bridge downloaded; the asset server hands it every path it has no file for.
func (a *App) iconHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := iconPath.FindStringSubmatch(r.URL.Path)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "max-age=86400")
		http.ServeFile(w, r, filepath.Join(a.cacheDir(), "icons", m[1]))
	})
}
