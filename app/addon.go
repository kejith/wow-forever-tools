package main

// Installing the addons the app works with (WoWToolsForever, GoldPerHour) and writing app requests. The addons live in
// their own repositories; the app installs the latest GitHub release (releases.go) or, for development, a
// local checkout. Nothing here needs the window, so addon_test.go runs all of it against a scratch folder.

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const requestFile = "AppRequest.lua"

// dataFile is what the bridge writes for the addon to show in the game (sim/tools/ingame). Like a request, it
// belongs to the app and not to the addon's release: an update keeps it and it never counts as changed.
const (
	dataFile        = "AppData.lua"
	syncHistoryFile = "SyncHistory.lua"
)

func isAppFile(f string) bool { return f == requestFile || f == dataFile || f == syncHistoryFile }

// addonSpec is an addon the app can install. Each one ships an AppRequest.lua stub that the app later
// overwrites with a request; global is the variable the addon reads from it. env names a local checkout that
// takes the place of the release.
type addonSpec struct {
	name, repo, env, global string
	legacyDir               string
	obsolete                []string
}

var addons = map[string]addonSpec{
	"wowtoolsforever": {name: "WoWToolsForever", repo: "kejith/WoWToolsForever", env: "WOWTOOLSFOREVER_ADDON_DIR", global: "WoWToolsForever_AppRequest", legacyDir: "WoWMCP", obsolete: []string{"ItemQueue.lua"}},
	"goldperhour":     {name: "GoldPerHour", repo: "kejith/GoldPerHour", env: "GOLDPERHOUR_ADDON_DIR", global: "GoldPerHour_AppRequest"},
}

func spec(key string) (addonSpec, error) {
	s, ok := addons[key]
	if !ok {
		return s, errors.New(t("Unknown addon: {key}", "key", key))
	}
	return s, nil
}

func installedAddonDir(wowDir, key string) string {
	s, _ := spec(key)
	return filepath.Join(wowDir, "Interface", "AddOns", s.name)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// addonOpts is where the app looks for an addon's files.
type addonOpts struct {
	cacheDir string
	sources  map[string]string // key -> local checkout
	api      string
	maxAge   time.Duration
	force    bool
}

// resolveSource says where the addon's files come from. A local checkout (opts.sources from the app settings,
// or the environment variable) wins, so a change can be tried in the game without publishing a release.
// Otherwise the latest GitHub release, cached under opts.cacheDir; force asks GitHub now instead of using the
// last answer.
func resolveSource(key string, o addonOpts) (*source, error) {
	s, err := spec(key)
	if err != nil {
		return nil, err
	}
	local := o.sources[key]
	if local == "" {
		local = os.Getenv(s.env)
	}
	if local != "" {
		dir, _ := filepath.Abs(local)
		if !exists(filepath.Join(dir, s.name+".toc")) {
			return nil, errors.New(t("Local source without {toc}: {dir}", "toc", s.name+".toc", "dir", dir))
		}
		return &source{Kind: "local", Dir: dir, Repo: s.repo, Version: nullable(tocVersion(dir, s.name))}, nil
	}
	return latestRelease(releaseOpts{repo: s.repo, name: s.name, cacheDir: o.cacheDir, api: o.api, maxAge: o.maxAge, force: o.force})
}

// tocFiles are the files the .toc lists, with forward slashes. A path that leaves the addon folder is refused:
// these names decide where the installer writes.
func tocFiles(dir, key string) ([]string, error) {
	s, _ := spec(key)
	data, err := os.ReadFile(filepath.Join(dir, s.name+".toc"))
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.ReplaceAll(strings.TrimSpace(line), `\`, "/")
		if f == "" || strings.HasPrefix(f, "#") {
			continue
		}
		bad := strings.HasPrefix(f, "/") || strings.Contains(f, ":")
		for _, p := range strings.Split(f, "/") {
			bad = bad || p == ".."
		}
		if bad {
			return nil, errors.New(t("Path not allowed in {toc}: {path}", "toc", s.name+".toc", "path", f))
		}
		files = append(files, f)
	}
	return files, nil
}

// requestText is the assignment of the request file; the stub's comment shows an example request.
func requestText(wowDir, key string) string {
	s, _ := spec(key)
	data, err := os.ReadFile(filepath.Join(installedAddonDir(wowDir, key), requestFile))
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(s.global) + `\s*=`)
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if re.MatchString(l) {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// pendingRequest is a request the game has not read yet.
type pendingRequest struct {
	ID       int64   `json:"id"`
	Action   *string `json:"action"`
	Settings bool    `json:"settings,omitempty"`
}

var (
	requestID       = regexp.MustCompile(`\bid\s*=\s*(\d+)`)
	requestAction   = regexp.MustCompile(`action\s*=\s*"(\w+)"`)
	requestSettings = regexp.MustCompile(`\bsettings\s*=`)
)

func readRequest(wowDir, key string) *pendingRequest {
	text := requestText(wowDir, key)
	id := requestID.FindStringSubmatch(text)
	if id == nil {
		return nil
	}
	n, _ := strconv.ParseInt(id[1], 10, 64)
	out := &pendingRequest{ID: n}
	if a := requestAction.FindStringSubmatch(text); a != nil {
		out.Action = &a[1]
	}
	if key != "wowtoolsforever" && requestSettings.MatchString(text) {
		out.Settings = true
	}
	return out
}

// addonFiles returns the .toc, every file it lists, and any asset files under Media/.
func addonFiles(dir, key string) ([]string, error) {
	s, _ := spec(key)
	listed, err := tocFiles(dir, key)
	if err != nil {
		return nil, err
	}
	files := append([]string{s.name + ".toc"}, listed...)
	mediaDir := filepath.Join(dir, "Media")
	if info, err := os.Stat(mediaDir); err == nil && info.IsDir() {
		var media []string
		_ = filepath.WalkDir(mediaDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if rel, err := filepath.Rel(dir, p); err == nil {
				media = append(media, filepath.ToSlash(rel))
			}
			return nil
		})
		sort.Strings(media)
		files = append(files, media...)
	}
	return files, nil
}

func changedFiles(sourceDir, target, key string) ([]string, error) {
	files, err := addonFiles(sourceDir, key)
	if err != nil {
		return nil, err
	}
	changed := []string{}
	for _, f := range files {
		if isAppFile(f) {
			continue
		}
		a, errA := os.ReadFile(filepath.Join(sourceDir, filepath.FromSlash(f)))
		b, errB := os.ReadFile(filepath.Join(target, filepath.FromSlash(f)))
		if errA != nil || errB != nil || !bytes.Equal(a, b) {
			changed = append(changed, f)
		}
	}
	return changed, nil
}

// addonState is what the Connection page shows of an addon.
type addonState struct {
	Key                  string          `json:"key"`
	Name                 string          `json:"name"`
	Repo                 string          `json:"repo"`
	InstalledDir         string          `json:"installedDir"`
	Installed            bool            `json:"installed"`
	InstalledVersion     *string         `json:"installedVersion"`
	Source               *source         `json:"source"`
	SourceError          *string         `json:"sourceError"`
	SourceDir            *string         `json:"sourceDir"`
	SourceVersion        *string         `json:"sourceVersion"`
	Changed              []string        `json:"changed"`
	Ahead                bool            `json:"ahead"`
	UpToDate             bool            `json:"upToDate"`
	AuctionatorInstalled bool            `json:"auctionatorInstalled"`
	PendingRequest       *pendingRequest `json:"pendingRequest"`
	FirstInstall         *bool           `json:"firstInstall,omitempty"`
}

func addonInfo(wowDir, key string, o addonOpts) (*addonState, error) {
	s, err := spec(key)
	if err != nil {
		return nil, err
	}
	target := installedAddonDir(wowDir, key)
	installed := exists(filepath.Join(target, s.name+".toc"))
	info := &addonState{Key: key, Name: s.name, Repo: s.repo, InstalledDir: target, Installed: installed, Changed: []string{},
		AuctionatorInstalled: exists(filepath.Join(wowDir, "Interface", "AddOns", "Auctionator")),
		PendingRequest:       readRequest(wowDir, key)}
	if installed {
		info.InstalledVersion = nullable(tocVersion(target, s.name))
	}
	found, err := resolveSource(key, o)
	if err == nil {
		var changed []string
		if changed, err = changedFiles(found.Dir, target, key); err == nil {
			info.Changed = changed
			info.Source = found
			info.SourceDir = &found.Dir
			info.SourceVersion = found.Version
		}
	}
	if err != nil {
		msg := err.Error()
		info.SourceError = &msg
	}
	// Installed from a local checkout that is ahead of the release: nothing to update.
	installedVersion, sourceVersion := "", ""
	if info.InstalledVersion != nil {
		installedVersion = *info.InstalledVersion
	}
	if info.SourceVersion != nil {
		sourceVersion = *info.SourceVersion
	}
	info.Ahead = info.Source != nil && installed && len(info.Changed) > 0 && compareVersions(installedVersion, sourceVersion) > 0
	// Forever re-reads the .toc on /reload (tested 2026-11-15: new files and new SavedVariables load), so an
	// update never needs a game restart. Without a source there is nothing to compare against; an installed
	// addon then counts as current.
	info.UpToDate = installed && (len(info.Changed) == 0 || info.Ahead)
	return info, nil
}

func installAddon(wowDir, key string, o addonOpts) (*addonState, error) {
	s, err := spec(key)
	if err != nil {
		return nil, err
	}
	src, err := resolveSource(key, o)
	if err != nil {
		return nil, err
	}
	target := installedAddonDir(wowDir, key)
	wasInstalled := exists(filepath.Join(target, s.name+".toc"))
	files, err := addonFiles(src.Dir, key)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, f := range files {
		if !exists(filepath.Join(src.Dir, filepath.FromSlash(f))) {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return nil, errors.New(t("The source lacks files listed in the .toc: {files}", "files", strings.Join(missing, ", ")))
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, err
	}
	for _, f := range files {
		to := filepath.Join(target, filepath.FromSlash(f))
		// Keep a request and the lists the app already wrote; only ship the empty stubs where none exist.
		if isAppFile(f) && exists(to) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src.Dir, filepath.FromSlash(f)))
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			return nil, err
		}
	}
	for _, f := range s.obsolete {
		os.Remove(filepath.Join(target, f))
	}
	if s.legacyDir != "" {
		oldDir := filepath.Join(wowDir, "Interface", "AddOns", s.legacyDir)
		if exists(filepath.Join(oldDir, s.legacyDir+".toc")) {
			_ = os.RemoveAll(oldDir)
		}
	}
	o.force = false
	info, err := addonInfo(wowDir, key, o)
	if err != nil {
		return nil, err
	}
	first := !wasInstalled
	info.FirstInstall = &first
	return info, nil
}

// nextID is seconds since 1970, but always above the previous id so two quick clicks never share one.
func nextID(wowDir, key string) int64 {
	id := time.Now().Unix()
	if r := readRequest(wowDir, key); r != nil && r.ID+1 > id {
		id = r.ID + 1
	}
	return id
}

const requestNote = "-- Written by the WoW Tools Forever desktop app. The client re-reads this file on /reload; a request runs once.\n"

func writeRequestFile(wowDir, key string, fields []string) error {
	s, err := spec(key)
	if err != nil {
		return err
	}
	target := installedAddonDir(wowDir, key)
	if !exists(filepath.Join(target, s.name+".toc")) {
		return errors.New(t("{name} is not installed.", "name", s.name))
	}
	value := "nil"
	if fields != nil {
		value = "{ " + strings.Join(fields, ", ") + " }"
	}
	return os.WriteFile(filepath.Join(target, requestFile), []byte(requestNote+s.global+" = "+value+"\n"), 0o644)
}

type requestResult struct {
	ID     int64  `json:"id"`
	Action string `json:"action"`
}

// writeRequest asks WoWToolsForever for an item export ("items", full = everything again) or a snapshot ("sync").
func writeRequest(wowDir, action string, full bool) (*requestResult, error) {
	id := nextID(wowDir, "wowtoolsforever")
	if action != "sync" {
		action = "items"
	}
	fields := []string{fmt.Sprintf("id = %d", id), fmt.Sprintf(`action = "%s"`, action)}
	if full {
		fields = append(fields, "full = true")
	}
	if err := writeRequestFile(wowDir, "wowtoolsforever", fields); err != nil {
		return nil, err
	}
	return &requestResult{ID: id, Action: action}, nil
}

// ---------- GoldPerHour ----------

var gphActions = []string{"start", "stop", "reset"}

// jsNumber is what JavaScript's Number() makes of a value the page sent; ok is false for NaN.
func jsNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case float64:
		return x, !math.IsNaN(x)
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, true
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil && !math.IsNaN(f)
	}
	return 0, false
}

func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

func luaNumber(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }

// gphSettingsLua: only known settings with the right type reach the file; strings are limited to the allowed
// values, so nothing from the page can end up as Lua code.
func gphSettingsLua(s map[string]any) []string {
	var out []string
	for _, key := range []string{"shown", "resumeAfterReload", "autoStart", "deductAhCut", "vendorFloor", "trackAlways"} {
		if b, ok := s[key].(bool); ok {
			out = append(out, fmt.Sprintf("%s = %t", key, b))
		}
	}
	if v, present := s["scale"]; present {
		if scale, ok := jsNumber(v); ok && !math.IsInf(scale, 0) {
			out = append(out, "scale = "+luaNumber(math.Min(2, math.Max(0.5, jsRound(scale*100)/100))))
		}
	}
	if v, present := s["tooltipItems"]; present {
		if items, ok := jsNumber(v); ok && !math.IsInf(items, 0) {
			out = append(out, "tooltipItems = "+luaNumber(math.Min(30, math.Max(1, jsRound(items)))))
		}
	}
	if v, present := s["ahMinValue"]; present {
		if n, ok := jsNumber(v); ok && !math.IsInf(n, 0) {
			out = append(out, "ahMinValue = "+luaNumber(math.Max(0, jsRound(n))))
		}
	}
	if v, present := s["wealthInterval"]; present {
		if n, ok := jsNumber(v); ok && !math.IsInf(n, 0) {
			out = append(out, "wealthInterval = "+luaNumber(math.Min(3600, math.Max(60, jsRound(n)))))
		}
	}
	if v, present := s["minDropQuality"]; present {
		if n, ok := jsNumber(v); ok && !math.IsInf(n, 0) {
			out = append(out, "minDropQuality = "+luaNumber(math.Min(5, math.Max(0, jsRound(n)))))
		}
	}
	if v, present := s["minDropValue"]; present {
		if n, ok := jsNumber(v); ok && !math.IsInf(n, 0) {
			out = append(out, "minDropValue = "+luaNumber(math.Max(0, jsRound(n))))
		}
	}
	if p, _ := s["priceSource"].(string); p == "auctionator" || p == "vendor" {
		out = append(out, fmt.Sprintf(`priceSource = "%s"`, p))
	}
	for _, listKey := range []string{"herbSpells", "miningSpells"} {
		if list, ok := s[listKey].([]any); ok {
			ids := []string{}
			seen := map[float64]bool{}
			for _, v := range list {
				n, ok := jsNumber(v)
				if !ok || n <= 0 || n != math.Trunc(n) || math.IsInf(n, 0) || seen[n] {
					continue
				}
				seen[n] = true
				ids = append(ids, luaNumber(n))
			}
			out = append(out, listKey+" = { "+strings.Join(ids, ", ")+" }")
		}
	}
	return out
}

type gphResult struct {
	ID       int64   `json:"id"`
	Action   *string `json:"action"`
	Settings bool    `json:"settings"`
}

// writeGphRequest asks GoldPerHour to start, stop or reset its session and/or to take new settings.
func writeGphRequest(wowDir string, action any, settings map[string]any) (*gphResult, error) {
	var act string
	if action != nil {
		a, ok := action.(string)
		known := false
		for _, x := range gphActions {
			known = known || (ok && a == x)
		}
		if !known {
			return nil, errors.New(t("Unknown action: {action}", "action", fmt.Sprint(action)))
		}
		act = a
	}
	var lua []string
	if settings != nil {
		lua = gphSettingsLua(settings)
	}
	if act == "" && len(lua) == 0 {
		return nil, errors.New(t("Nothing to send."))
	}
	id := nextID(wowDir, "goldperhour")
	fields := []string{fmt.Sprintf("id = %d", id)}
	if act != "" {
		fields = append(fields, fmt.Sprintf(`action = "%s"`, act))
	}
	if len(lua) > 0 {
		fields = append(fields, "settings = { "+strings.Join(lua, ", ")+" }")
	}
	if err := writeRequestFile(wowDir, "goldperhour", fields); err != nil {
		return nil, err
	}
	return &gphResult{ID: id, Action: nullable(act), Settings: len(lua) > 0}, nil
}

// clearGphRequest withdraws a request the game has not read yet: an empty request, so nothing runs on the
// next /reload.
func clearGphRequest(wowDir string) (map[string]bool, error) {
	if err := writeRequestFile(wowDir, "goldperhour", nil); err != nil {
		return nil, err
	}
	return map[string]bool{"cleared": true}, nil
}
