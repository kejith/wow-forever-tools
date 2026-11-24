package main

// Install/update/request handling against a scratch WoW folder, and the download of addon releases against a
// local stand-in for the GitHub API (what addon.test.js checked in the Electron app).

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

func writeTree(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func stub(global string) string {
	return fmt.Sprintf("-- Example: %s = { id = 1, action = \"items\" }\n%s = nil\n", global, global)
}

func addonTestFiles(version string) map[string]string {
	return map[string]string{
		"WoWToolsForever.toc":     "## Interface: 16001\n## Version: " + version + "\n## SavedVariables: WoWToolsForever_Account\n\nAppRequest.lua\nWoWToolsForever.lua\n",
		"AppRequest.lua": stub("WoWToolsForever_AppRequest"),
		"WoWToolsForever.lua":     "-- WoWToolsForever " + version + "\n",
	}
}

func gphFiles(version string) map[string]string {
	return map[string]string{
		"GoldPerHour.toc": "## Interface: 16001\n## Version: " + version + "\n\nAppRequest.lua\nGoldPerHour.lua\n",
		"AppRequest.lua":  stub("GoldPerHour_AppRequest"),
		"GoldPerHour.lua": "-- GoldPerHour " + version + "\n",
	}
}

type zipItem struct {
	name, data string
	store      bool
	crc        *uint32 // a wrong checksum on purpose
}

// makeZip writes what real zips contain: folder entries, stored and deflated files, an archive comment.
func makeZip(t *testing.T, items []zipItem, comment string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, it := range items {
		data := []byte(it.data)
		method := zip.Deflate
		packed := data
		if strings.HasSuffix(it.name, "/") || it.store {
			method = zip.Store
		} else {
			var p bytes.Buffer
			fw, _ := flate.NewWriter(&p, flate.DefaultCompression)
			fw.Write(data)
			fw.Close()
			packed = p.Bytes()
		}
		crc := crc32.ChecksumIEEE(data)
		if it.crc != nil {
			crc = *it.crc
		}
		fw, err := w.CreateRaw(&zip.FileHeader{Name: it.name, Method: method, CRC32: crc,
			CompressedSize64: uint64(len(packed)), UncompressedSize64: uint64(len(data))})
		if err != nil {
			t.Fatal(err)
		}
		fw.Write(packed)
	}
	if comment != "" {
		w.SetComment(comment)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// addonZip is a release zip: the folder <name>/ with the files, the .toc first and stored.
func addonZip(t *testing.T, name string, files map[string]string) []byte {
	items := []zipItem{{name: name + "/"}}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if strings.HasSuffix(a, ".toc") != strings.HasSuffix(b, ".toc") {
			if strings.HasSuffix(a, ".toc") {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	for i, k := range keys {
		items = append(items, zipItem{name: name + "/" + k, data: files[k], store: i == 0})
	}
	return makeZip(t, items, strings.Repeat("a", 40))
}

type published struct {
	tag string
	id  int
	zip []byte
}

// githubStandIn is api.github.com for the addon repositories: releases[repo] is the latest release, missing = no
// release yet.
type githubStandIn struct {
	mu       sync.Mutex
	releases map[string]published
	hits     []string
}

func (g *githubStandIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.hits = append(g.hits, r.URL.Path)
	latest := regexp.MustCompile(`^/repos/(kejith/\w+)/releases/latest$`).FindStringSubmatch(r.URL.Path)
	asset := regexp.MustCompile(`^/repos/(kejith/\w+)/releases/assets/(\d+)$`).FindStringSubmatch(r.URL.Path)
	repo := ""
	if latest != nil {
		repo = latest[1]
	} else if asset != nil {
		repo = asset[1]
	}
	rel, ok := g.releases[repo]
	g.mu.Unlock()
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
		return
	}
	if latest != nil {
		name := strings.Split(repo, "/")[1]
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": rel.tag, "html_url": "https://github.com/" + repo + "/releases/tag/" + rel.tag, "published_at": "2026-11-17T20:00:00Z",
			"assets": []map[string]any{{"id": 7, "name": "notes.txt", "size": 1},
				{"id": rel.id, "name": name + "-" + rel.tag + ".zip", "size": len(rel.zip), "browser_download_url": "https://example.invalid/never-used.zip"}},
		})
		return
	}
	if asset[2] == fmt.Sprint(rel.id) && r.Header.Get("Accept") == "application/octet-stream" {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(rel.zip)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (g *githubStandIn) publish(repo string, p published) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.releases[repo] = p
}

func (g *githubStandIn) hitCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.hits)
}

// must is the value of a call that has to work; a failure ends the test with a panic.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func wantErr(t *testing.T, err error, pattern string) {
	t.Helper()
	if err == nil || !regexp.MustCompile(pattern).MatchString(err.Error()) {
		t.Fatalf("want an error matching %q, got %v", pattern, err)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestVersionsAndZips(t *testing.T) {
	setLanguage("en")
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.10.0", "1.9.3", 1}, {"v0.3.0", "0.3", 0}, {"0.2.0", "0.3.0-beta", -1}} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%s, %s) = %d", c.a, c.b, got)
		}
	}
	files, err := readZip(addonZip(t, "WoWToolsForever", addonTestFiles("1.5.0")))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.name)
	}
	if strings.Join(names, " ") != "WoWToolsForever/WoWToolsForever.toc WoWToolsForever/AppRequest.lua WoWToolsForever/WoWToolsForever.lua" {
		t.Fatalf("zip entries %v", names)
	}
	if string(files[2].data) != "-- WoWToolsForever 1.5.0\n" {
		t.Fatalf("content %q", files[2].data)
	}
	one := uint32(1)
	_, err = readZip(makeZip(t, []zipItem{{name: "WoWToolsForever/x.lua", data: "x", crc: &one}}, ""))
	wantErr(t, err, "checksum does not match")
	_, err = readZip([]byte("<html>not a zip</html>"))
	wantErr(t, err, "not a zip file")

	// Nothing may land outside the addon's folder.
	scratch := t.TempDir()
	for _, name := range []string{"../evil.lua", "WoWToolsForever/../../evil.lua", "Other/x.lua", "/abs.lua", `WoWToolsForever/a\..\b.lua`} {
		out := filepath.Join(scratch, "extract", "WoWToolsForever")
		err := extractAddon(makeZip(t, []zipItem{{name: name, data: "x"}, {name: "WoWToolsForever/ok.lua", data: "ok"}}, ""), "WoWToolsForever", out)
		wantErr(t, err, "Unexpected entry in the zip")
		if exists(filepath.Join(scratch, "evil.lua")) || exists(filepath.Join(scratch, "extract", "evil.lua")) {
			t.Fatalf("%s wrote outside the folder", name)
		}
	}
}

func TestLocalSource(t *testing.T) {
	setLanguage("en")
	scratch := t.TempDir()
	wow := filepath.Join(scratch, "wow")
	target := installedAddonDir(wow, "wowtoolsforever")
	src := writeTree(t, filepath.Join(scratch, "src", "WoWToolsForever"), addonTestFiles("1.5.0"))
	gphSrc := writeTree(t, filepath.Join(scratch, "src", "GoldPerHour"), gphFiles("0.3.0"))
	local := addonOpts{sources: map[string]string{"wowtoolsforever": src, "goldperhour": gphSrc}}

	info := must(addonInfo(wow, "wowtoolsforever", local))
	if info.Installed || info.Source.Kind != "local" || deref(info.SourceVersion) != "1.5.0" {
		t.Fatalf("before install: %+v", info)
	}
	_, err := writeRequest(wow, "items", false)
	wantErr(t, err, "WoWToolsForever is not installed")

	// A fresh install ships every file from the .toc.
	info = must(installAddon(wow, "wowtoolsforever", local))
	if !info.Installed || !info.UpToDate || !*info.FirstInstall || !exists(filepath.Join(target, "AppRequest.lua")) {
		t.Fatalf("fresh install: %+v", info)
	}
	// The shipped stub (with an example in a comment) is no pending request.
	if r := readRequest(wow, "wowtoolsforever"); r != nil {
		t.Fatalf("stub read as request %+v", r)
	}

	// A request is written as Lua the addon can read, ids grow even within one second.
	r1 := must(writeRequest(wow, "items", true))
	r2 := must(writeRequest(wow, "sync", false))
	if r2.ID <= r1.ID {
		t.Fatalf("ids %d then %d", r1.ID, r2.ID)
	}
	text, _ := os.ReadFile(filepath.Join(target, "AppRequest.lua"))
	if !regexp.MustCompile(`WoWToolsForever_AppRequest = \{ id = \d+, action = "sync" \}`).Match(text) {
		t.Fatalf("request file %s", text)
	}
	pending := func(key string) string {
		r := readRequest(wow, key)
		if r == nil {
			return "nil"
		}
		return fmt.Sprintf("%d %s %t", r.ID, deref(r.Action), r.Settings)
	}
	syncReq := fmt.Sprintf("%d sync false", r2.ID)
	if pending("wowtoolsforever") != syncReq {
		t.Fatalf("pending %s", pending("wowtoolsforever"))
	}

	// Updating an install from 1.0 (old .toc, ItemQueue.lua) keeps the pending request and removes the old file.
	writeTree(t, target, map[string]string{
		"WoWToolsForever.toc":    "## Interface: 16001\n## Version: 1.0.0\n## SavedVariablesPerCharacter: WoWToolsForever_Char\n\nItemQueue.lua\nWoWToolsForever.lua\n",
		"ItemQueue.lua": "WoWToolsForever_ItemQueue = {}\n",
	})
	info = must(addonInfo(wow, "wowtoolsforever", local))
	if info.UpToDate || deref(info.InstalledVersion) != "1.0.0" || !slices.Contains(info.Changed, "WoWToolsForever.toc") {
		t.Fatalf("old install: %+v", info)
	}
	info = must(installAddon(wow, "wowtoolsforever", local))
	if !info.UpToDate || *info.FirstInstall || exists(filepath.Join(target, "ItemQueue.lua")) || pending("wowtoolsforever") != syncReq {
		t.Fatalf("update from 1.0: %+v", info)
	}

	// A change in the checkout without a new version number still counts as an update.
	f, _ := os.OpenFile(filepath.Join(src, "WoWToolsForever.lua"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("-- edited\n")
	f.Close()
	info = must(addonInfo(wow, "wowtoolsforever", local))
	if info.UpToDate || info.Ahead || strings.Join(info.Changed, ",") != "WoWToolsForever.lua" {
		t.Fatalf("edited checkout: %+v", info)
	}
	must(installAddon(wow, "wowtoolsforever", local))

	// Since 1.7 the addon has AppData.lua, the lists the app's bridge writes. An update ships the empty stub,
	// later updates keep what the app wrote, and the file never counts as a difference to the source.
	withLists := func(version string) map[string]string {
		files := addonTestFiles(version)
		files["WoWToolsForever.toc"] = "## Interface: 16001\n## Version: " + version + "\n\nAppRequest.lua\nAppData.lua\nWoWToolsForever.lua\n"
		files["AppData.lua"] = stub("WoWToolsForever_AppData")
		return files
	}
	lists := addonOpts{sources: map[string]string{"wowtoolsforever": writeTree(t, filepath.Join(scratch, "src", "lists"), withLists("1.7.0"))}}
	info = must(installAddon(wow, "wowtoolsforever", lists))
	data, _ := os.ReadFile(filepath.Join(target, "AppData.lua"))
	if deref(info.InstalledVersion) != "1.7.0" || !info.UpToDate || !regexp.MustCompile(`(?m)^WoWToolsForever_AppData = nil$`).Match(data) {
		t.Fatalf("lists stub: %+v %s", info, data)
	}
	sent := "WoWToolsForever_AppData = { version = 1, generated = 1, hash = \"abc\", chars = {} }\n"
	os.WriteFile(filepath.Join(target, "AppData.lua"), []byte(sent), 0o644)
	info = must(addonInfo(wow, "wowtoolsforever", lists))
	if !info.UpToDate || len(info.Changed) != 0 {
		t.Fatalf("written lists count as a change: %+v", info)
	}
	writeTree(t, lists.sources["wowtoolsforever"], withLists("1.7.1"))
	info = must(installAddon(wow, "wowtoolsforever", lists))
	data, _ = os.ReadFile(filepath.Join(target, "AppData.lua"))
	if deref(info.InstalledVersion) != "1.7.1" || !info.UpToDate || string(data) != sent || pending("wowtoolsforever") != syncReq {
		t.Fatalf("update with lists: %+v %s", info, data)
	}
	os.Remove(filepath.Join(target, "AppData.lua"))
	must(installAddon(wow, "wowtoolsforever", local))

	// Media assets (such as the minimap icon texture) are copied and tracked for changes, and installing
	// WoWToolsForever removes a legacy WoWMCP addon folder if one exists.
	withMedia := addonTestFiles("1.8.0")
	withMedia["Media/Icon.tga"] = "TGAv1"
	mediaSrc := writeTree(t, filepath.Join(scratch, "src", "media"), withMedia)
	mediaOpts := addonOpts{sources: map[string]string{"wowtoolsforever": mediaSrc}}
	legacyDir := filepath.Join(wow, "Interface", "AddOns", "WoWMCP")
	writeTree(t, legacyDir, map[string]string{"WoWMCP.toc": "## Interface: 16001\n## Version: 1.12.0\n"})
	info = must(installAddon(wow, "wowtoolsforever", mediaOpts))
	if !info.UpToDate || !exists(filepath.Join(target, "Media", "Icon.tga")) || exists(legacyDir) {
		t.Fatalf("media install / legacy cleanup: %+v", info)
	}
	os.WriteFile(filepath.Join(mediaSrc, "Media", "Icon.tga"), []byte("TGAv2"), 0o644)
	info = must(addonInfo(wow, "wowtoolsforever", mediaOpts))
	if info.UpToDate || strings.Join(info.Changed, ",") != "Media/Icon.tga" {
		t.Fatalf("media change detection: %+v", info)
	}
	must(installAddon(wow, "wowtoolsforever", local))

	// A .toc must not send the installer outside the addon folder.
	evil := writeTree(t, filepath.Join(scratch, "src", "evil"), map[string]string{"WoWToolsForever.toc": "## Version: 9\n\n..\\..\\evil.lua\n", "evil.lua": "x"})
	_, err = installAddon(wow, "wowtoolsforever", addonOpts{sources: map[string]string{"wowtoolsforever": evil}})
	wantErr(t, err, `Path not allowed in WoWToolsForever\.toc`)
	info = must(addonInfo(wow, "wowtoolsforever", addonOpts{sources: map[string]string{"wowtoolsforever": filepath.Join(scratch, "nowhere")}}))
	if !regexp.MustCompile(`Local source without WoWToolsForever\.toc`).MatchString(deref(info.SourceError)) || !info.UpToDate {
		t.Fatalf("missing source: %+v", info) // nothing to compare against, the install is left alone
	}

	// GoldPerHour installs next to WoWToolsForever into its own folder and has its own requests.
	gph := installedAddonDir(wow, "goldperhour")
	if must(addonInfo(wow, "goldperhour", local)).Installed {
		t.Fatal("GoldPerHour installed too early")
	}
	_, err = writeGphRequest(wow, "start", nil)
	wantErr(t, err, "GoldPerHour is not installed")
	info = must(installAddon(wow, "goldperhour", local))
	if !info.Installed || !info.UpToDate || info.Name != "GoldPerHour" || !exists(filepath.Join(gph, "GoldPerHour.lua")) {
		t.Fatalf("GoldPerHour install: %+v", info)
	}
	if pending("goldperhour") != "nil" || pending("wowtoolsforever") != syncReq { // WoWToolsForever's request untouched
		t.Fatalf("requests %s / %s", pending("goldperhour"), pending("wowtoolsforever"))
	}

	var settings map[string]any
	json.Unmarshal([]byte(`{"scale": 9, "tooltipItems": "4", "priceSource": "vendor\"; os.exit() --", "autoStart": true, "shown": false,
		"herbSpells": [2366, "1234", -1, "x", 1234], "evil": "print(1)"}`), &settings)
	g1 := must(writeGphRequest(wow, "reset", settings))
	gText, _ := os.ReadFile(filepath.Join(gph, "AppRequest.lua"))
	want := fmt.Sprintf(`GoldPerHour_AppRequest = { id = %d, action = "reset", settings = { shown = false, autoStart = true, scale = 2, tooltipItems = 4, herbSpells = { 2366, 1234 } } }`, g1.ID)
	if !strings.Contains(string(gText), want) || regexp.MustCompile(`os\.exit|evil|print`).Match(gText) {
		t.Fatalf("GoldPerHour request\n%s\nwant %s", gText, want)
	}
	if pending("goldperhour") != fmt.Sprintf("%d reset true", g1.ID) {
		t.Fatalf("pending %s", pending("goldperhour"))
	}
	_, err = writeGphRequest(wow, "explode", nil)
	wantErr(t, err, "Unknown action: explode")
	_, err = writeGphRequest(wow, nil, map[string]any{})
	wantErr(t, err, "Nothing to send")
	g2 := must(writeGphRequest(wow, nil, map[string]any{"priceSource": "auctionator"}))
	if g2.ID <= g1.ID || g2.Action != nil {
		t.Fatalf("settings only: %+v", g2)
	}

	// An update keeps the request, withdrawing leaves an empty one.
	writeTree(t, gph, map[string]string{"GoldPerHour.toc": "## Interface: 16001\n## Version: 0.2.0\n\nGoldPerHour.lua\n"})
	if v := deref(must(addonInfo(wow, "goldperhour", local)).InstalledVersion); v != "0.2.0" {
		t.Fatalf("installed %s", v)
	}
	info = must(installAddon(wow, "goldperhour", local))
	if !info.UpToDate || readRequest(wow, "goldperhour").ID != g2.ID {
		t.Fatalf("update kept no request: %+v", info)
	}
	must(clearGphRequest(wow))
	gText, _ = os.ReadFile(filepath.Join(gph, "AppRequest.lua"))
	if readRequest(wow, "goldperhour") != nil || !regexp.MustCompile(`(?m)^GoldPerHour_AppRequest = nil$`).Match(gText) {
		t.Fatalf("withdrawn request %s", gText)
	}
}

func TestReleaseSource(t *testing.T) {
	setLanguage("en")
	defer setLanguage("en")
	scratch := t.TempDir()
	gh := &githubStandIn{releases: map[string]published{}}
	server := httptest.NewServer(gh)
	cacheDir := filepath.Join(scratch, "cache")
	opts := addonOpts{cacheDir: cacheDir, api: server.URL}
	force := opts
	force.force = true
	wow2 := filepath.Join(scratch, "wow2")
	gph2 := installedAddonDir(wow2, "goldperhour")
	t.Setenv("GOLDPERHOUR_ADDON_DIR", "")
	t.Setenv("WOWTOOLSFOREVER_ADDON_DIR", "")

	// No release yet and nothing cached: the reason is reported and nothing can be installed.
	info := must(addonInfo(wow2, "goldperhour", opts))
	if !strings.Contains(deref(info.SourceError), "kejith/GoldPerHour has no release yet") || info.Source != nil {
		t.Fatalf("no release: %+v", info)
	}
	_, err := installAddon(wow2, "goldperhour", opts)
	wantErr(t, err, "has no release yet")

	// First release: downloaded through the asset id, unpacked into the cache, installed from there.
	gh.publish("kejith/GoldPerHour", published{tag: "v0.3.0", id: 11, zip: addonZip(t, "GoldPerHour", gphFiles("0.3.0"))})
	info = must(addonInfo(wow2, "goldperhour", opts))
	if info.Source.Kind != "release" || info.Source.Tag != "v0.3.0" || deref(info.SourceVersion) != "0.3.0" || info.Source.Offline {
		t.Fatalf("first release: %+v", info.Source)
	}
	if deref(info.Source.URL) != "https://github.com/kejith/GoldPerHour/releases/tag/v0.3.0" || deref(info.SourceDir) != filepath.Join(cacheDir, "GoldPerHour", "v0.3.0") {
		t.Fatalf("first release: %+v", info.Source)
	}
	info = must(installAddon(wow2, "goldperhour", opts))
	data, _ := os.ReadFile(filepath.Join(gph2, "GoldPerHour.lua"))
	if !info.Installed || !info.UpToDate || deref(info.InstalledVersion) != "0.3.0" || !*info.FirstInstall || string(data) != "-- GoldPerHour 0.3.0\n" {
		t.Fatalf("install from release: %+v", info)
	}

	// Within maxAge the app does not ask GitHub again; force does and finds the next release.
	gh.publish("kejith/GoldPerHour", published{tag: "v0.4.0", id: 12, zip: addonZip(t, "GoldPerHour", gphFiles("0.4.0"))})
	before := gh.hitCount()
	info = must(addonInfo(wow2, "goldperhour", opts))
	if gh.hitCount() != before || !info.UpToDate {
		t.Fatalf("asked GitHub within maxAge: %+v", info)
	}
	info = must(addonInfo(wow2, "goldperhour", force))
	if info.UpToDate || deref(info.SourceVersion) != "0.4.0" || deref(info.InstalledVersion) != "0.3.0" || exists(filepath.Join(cacheDir, "GoldPerHour", "v0.3.0")) {
		t.Fatalf("forced check: %+v", info)
	}
	must(writeGphRequest(wow2, "start", nil))
	info = must(installAddon(wow2, "goldperhour", opts))
	if !info.UpToDate || deref(info.InstalledVersion) != "0.4.0" || deref(info.PendingRequest.Action) != "start" {
		t.Fatalf("update from release: %+v", info)
	}

	// A broken or hostile download changes nothing: the last good release stays the source.
	gh.publish("kejith/GoldPerHour", published{tag: "v0.5.0", id: 13, zip: makeZip(t, []zipItem{
		{name: "GoldPerHour/GoldPerHour.toc", data: "## Version: 0.5.0\n"}, {name: "../evil.lua", data: "x"}}, "")})
	info = must(addonInfo(wow2, "goldperhour", force))
	if deref(info.SourceVersion) != "0.4.0" || !info.Source.Offline || !info.UpToDate || !strings.Contains(info.Source.Error, "Unexpected entry in the zip") {
		t.Fatalf("hostile release: %+v", info.Source)
	}
	entries, _ := os.ReadDir(filepath.Join(cacheDir, "GoldPerHour"))
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if exists(filepath.Join(cacheDir, "evil.lua")) || strings.Join(left, ",") != "release.json,v0.4.0" {
		t.Fatalf("cache after a hostile release: %v", left)
	}

	// Installed from a checkout that is ahead of the release: not offered as an update.
	writeTree(t, gph2, gphFiles("0.6.0"))
	gh.publish("kejith/GoldPerHour", published{tag: "v0.4.0", id: 12, zip: addonZip(t, "GoldPerHour", gphFiles("0.4.0"))})
	info = must(addonInfo(wow2, "goldperhour", force))
	if !info.Ahead || !info.UpToDate || deref(info.SourceVersion) != "0.4.0" {
		t.Fatalf("ahead of the release: %+v", info)
	}

	// Without network the cached release keeps working and says so.
	server.Close()
	info = must(addonInfo(wow2, "goldperhour", force))
	if !info.Source.Offline || deref(info.SourceVersion) != "0.4.0" || !strings.Contains(info.Source.Error, "GitHub cannot be reached") {
		t.Fatalf("offline: %+v", info.Source)
	}
	info = must(installAddon(wow2, "goldperhour", force))
	if deref(info.InstalledVersion) != "0.4.0" || !info.UpToDate {
		t.Fatalf("offline install: %+v", info)
	}
	info = must(addonInfo(wow2, "wowtoolsforever", opts))
	if !regexp.MustCompile(`Release of kejith/WoWToolsForever not loaded: GitHub cannot be reached`).MatchString(deref(info.SourceError)) {
		t.Fatalf("offline without cache: %s", deref(info.SourceError))
	}

	// The same messages in German when the app runs in German.
	setLanguage("de")
	info = must(addonInfo(wow2, "wowtoolsforever", opts))
	if !regexp.MustCompile(`Release von kejith/WoWToolsForever nicht geladen: GitHub nicht erreichbar`).MatchString(deref(info.SourceError)) {
		t.Fatalf("German: %s", deref(info.SourceError))
	}
	_, err = writeGphRequest(wow2, "explode", nil)
	wantErr(t, err, "Unbekannte Aktion: explode")
}
