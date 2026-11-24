package main

// The app's own update against a local stand-in for the GitHub API and a made-up installer; nothing is
// started (the installer goes to a recorder). What update.test.js checked in the Electron app.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func sum256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func fakeInstaller(version string) []byte {
	return append([]byte("MZ installer "+version+" "), bytes.Repeat([]byte{byte(len(version))}, 300<<10)...)
}

// appRelease is the latest release of the app on the stand-in: sums replaces the text of SHA256SUMS.txt (nil
// = the right one, "-" = no such asset), send is what the download does: other (same size, other bytes), cut
// (the connection drops half way), stall (half of it, then silence), long (more bytes than listed).
type appRelease struct {
	tag, name, digest, send string
	exe                     []byte
	sums                    *string
}

func newRelease(version string) *appRelease {
	return &appRelease{tag: "v" + version, exe: fakeInstaller(version)}
}

type appStandIn struct {
	mu   sync.Mutex
	rel  *appRelease
	hits []string
}

func (s *appStandIn) set(r *appRelease) {
	s.mu.Lock()
	s.rel = r
	s.mu.Unlock()
}

func (s *appStandIn) count(from int, suffix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, h := range s.hits[from:] {
		if suffix == "" || strings.HasSuffix(h, suffix) {
			n++
		}
	}
	return n
}

func (s *appStandIn) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hits)
}

func (s *appStandIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.hits = append(s.hits, r.URL.Path)
	rel := s.rel
	s.mu.Unlock()
	asset := regexp.MustCompile(`^/repos/kejith/wow-tools-forever/releases/assets/(\d+)$`).FindStringSubmatch(r.URL.Path)
	if rel == nil || (asset == nil && r.URL.Path != "/repos/kejith/wow-tools-forever/releases/latest") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
		return
	}
	name := rel.name
	if name == "" {
		name = installerName(strings.TrimPrefix(rel.tag, "v"))
	}
	sums := sum256(rel.exe) + "  " + name + "\n"
	if rel.sums != nil {
		sums = *rel.sums
	}
	if asset == nil {
		assets := []map[string]any{{"id": 21, "name": name, "size": len(rel.exe), "browser_download_url": "https://example.invalid/never-used.exe"}}
		if rel.digest != "" {
			assets[0]["digest"] = rel.digest
		}
		if sums != "-" {
			assets = append(assets, map[string]any{"id": 22, "name": "SHA256SUMS.txt", "size": len(sums)})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": rel.tag, "html_url": "https://example.invalid/never-used",
			"published_at": "2026-11-23T10:00:00Z", "assets": assets})
		return
	}
	switch {
	case r.Header.Get("Accept") != "application/octet-stream":
		w.WriteHeader(http.StatusNotFound)
	case asset[1] == "22" && sums != "-":
		w.Write([]byte(sums))
	case asset[1] == "21":
		half := rel.exe[:len(rel.exe)/2]
		switch rel.send {
		case "cut":
			w.Header().Set("Content-Length", fmt.Sprint(len(rel.exe)))
			w.Write(half)
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		case "stall":
			w.Header().Set("Content-Length", fmt.Sprint(len(rel.exe)))
			w.Write(half)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "other":
			w.Write(bytes.Repeat([]byte{7}, len(rel.exe)))
		case "long":
			w.Write(append(append([]byte{}, rel.exe...), rel.exe...))
		default:
			w.Write(rel.exe)
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// leftovers is what an update folder holds besides the remembered answer of the last check.
func leftovers(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if e.Name() != "update.json" {
			out = append(out, e.Name())
		}
	}
	return out
}

func dirNames(dir string) string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return strings.Join(out, ",")
}

type started struct {
	file     string
	relaunch bool
}

type run struct {
	u      *updater
	dir    string
	mu     sync.Mutex
	states []updateState
}

func (r *run) phases() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, s := range r.states {
		if len(out) == 0 || out[len(out)-1] != s.Phase {
			out = append(out, s.Phase)
		}
	}
	return out
}

func TestUpdater(t *testing.T) {
	setLanguage("en")
	defer setLanguage("en")
	// ---------- small parts ----------
	hexA := strings.Repeat("ab", 32)
	if got := parseSums(strings.Repeat("cd", 32)+"  other.exe\n"+strings.ToUpper(hexA)+" *WoW-Tools-Forever-Setup-1.13.0.exe\r\n", "WoW-Tools-Forever-Setup-1.13.0.exe"); got != hexA {
		t.Fatalf("parseSums %q", got)
	}
	if parseSums(hexA+"  WoW-Tools-Forever-Setup-1.13.0.exe.blockmap\n", "WoW-Tools-Forever-Setup-1.13.0.exe") != "" || parseSums("<html>", "x.exe") != "" {
		t.Fatal("parseSums found a wrong line")
	}
	if strings.Join(installerArgs(true), " ") != "--updated /S --force-run" || strings.Join(installerArgs(false), " ") != "--updated /S" {
		t.Fatal("installer arguments")
	}
	if releaseURL("1.13.0", "") != "https://github.com/kejith/wow-tools-forever/releases/tag/v1.13.0" {
		t.Fatal("release url")
	}

	gh := &appStandIn{}
	server := httptest.NewServer(gh)
	defer server.Close()
	scratch := t.TempDir()
	var autoMu sync.Mutex
	auto := false
	setAuto := func(v bool) { autoMu.Lock(); auto = v; autoMu.Unlock() }
	var startedMu sync.Mutex
	var launched []started
	recorder := func(file string, relaunch bool) error {
		startedMu.Lock()
		launched = append(launched, started{file, relaunch})
		startedMu.Unlock()
		return nil
	}
	// One updater per "start of the app".
	start := func(dir, current string, change func(*updaterOpts)) *run {
		r := &run{dir: filepath.Join(scratch, dir)}
		o := updaterOpts{dir: r.dir, current: current, src: updateSource{api: server.URL},
			automatic: func() bool { autoMu.Lock(); defer autoMu.Unlock(); return auto },
			onState:   func(s updateState) { r.mu.Lock(); r.states = append(r.states, s); r.mu.Unlock() },
			launch:    recorder}
		if change != nil {
			change(&o)
		}
		r.u = newUpdater(o)
		return r
	}
	errOf := func(s updateState) string { return deref(s.Error) }

	// ---------- nothing to do ----------
	app := start("a", "1.12.0", nil)
	if s := app.u.state(); s.Phase != "idle" || !s.CanInstall || s.Automatic {
		t.Fatalf("start %+v", s)
	}
	s := app.u.check(false)
	if s.Phase != "idle" || s.Latest != nil || s.CheckedAt != nil || !strings.Contains(errOf(s), "kejith/wow-tools-forever has no release yet") {
		t.Fatalf("no release %+v", s)
	}

	// The running version is the newest: no checksum is fetched, and within the hour GitHub is not asked again.
	gh.set(newRelease("1.12.0"))
	before := gh.total()
	s = app.u.check(false)
	if s.Phase != "current" || s.Latest.Version != "1.12.0" || s.Error != nil || gh.count(before, "") != 1 || time.Since(time.UnixMilli(*s.CheckedAt)) > 5*time.Second {
		t.Fatalf("current %+v", s)
	}
	gh.set(newRelease("1.13.0"))
	if s = app.u.check(false); s.Phase != "current" || gh.count(before, "") != 1 {
		t.Fatalf("asked again within the hour %+v", s)
	}

	// ---------- an update, step by step (automatic off) ----------
	before = gh.total()
	s = app.u.check(true)
	if s.Phase != "available" || s.Latest.Version != "1.13.0" || s.Latest.Size != int64(len(fakeInstaller("1.13.0"))) || gh.count(before, "/assets/21") != 0 {
		t.Fatalf("available %+v", s)
	}
	if s.Latest.URL != "https://github.com/kejith/wow-tools-forever/releases/tag/v1.13.0" || deref(s.Latest.PublishedAt) != "2026-11-23T10:00:00Z" {
		t.Fatalf("latest %+v", s.Latest) // the address is built, not taken from the answer
	}
	if app.u.installsOnQuit() || app.u.install(true).Started {
		t.Fatal("installs before a download")
	}
	s = app.u.download()
	exe := filepath.Join(app.dir, "WoW-Tools-Forever-Setup-1.13.0.exe")
	data, _ := os.ReadFile(exe)
	if s.Phase != "ready" || s.Progress != nil || s.Error != nil || !bytes.Equal(data, fakeInstaller("1.13.0")) {
		t.Fatalf("downloaded %+v", s)
	}
	if dirNames(app.dir) != "WoW-Tools-Forever-Setup-1.13.0.exe,update.json" {
		t.Fatalf("update folder %s", dirNames(app.dir))
	}
	want := []string{"checking", "idle", "checking", "current", "checking", "current", "checking", "available", "downloading", "ready"}
	if !slices.Equal(app.phases(), want) {
		t.Fatalf("phases %v", app.phases())
	}
	var loading []updateState
	for _, st := range app.states {
		if st.Phase == "downloading" {
			loading = append(loading, st)
		}
	}
	if *loading[0].Progress != (updateProgress{0, int64(len(fakeInstaller("1.13.0")))}) {
		t.Fatalf("first progress %+v", loading[0].Progress)
	}
	if app.u.installsOnQuit() { // the user turned the automatic off
		t.Fatal("installs on quit without the automatic")
	}

	// "Restart and update": the installer is started for an update, silently, and starts the app again.
	r := app.u.install(true)
	if !r.Started || r.Phase != "installing" || len(launched) != 1 || launched[0] != (started{exe, true}) {
		t.Fatalf("install %+v %v", r, launched)
	}
	if app.u.check(true).Phase != "installing" { // nothing else runs while it installs
		t.Fatal("checked while installing")
	}

	// ---------- the next start finds the download; with the automatic it goes in when the app closes ----------
	setAuto(true)
	before = gh.total()
	app = start("a", "1.12.0", nil)
	if s = app.u.check(false); s.Phase != "ready" || !s.Automatic || gh.count(before, "") != 0 || !app.u.installsOnQuit() {
		t.Fatalf("download found again %+v", s)
	}
	if r = app.u.install(false); !r.Started || launched[len(launched)-1] != (started{exe, false}) {
		t.Fatalf("install on quit %+v", r)
	}

	// A file that changed while it waited is not started.
	app = start("a", "1.12.0", func(o *updaterOpts) { o.src.maxAge = 1000 * time.Hour })
	app.u.check(false)
	os.WriteFile(exe, bytes.Repeat([]byte{1}, len(fakeInstaller("1.13.0"))), 0o644)
	r = app.u.install(true)
	if r.Started || r.Phase != "available" || len(launched) != 2 || !strings.Contains(deref(r.Error), "does not match its checksum") || exists(exe) {
		t.Fatalf("changed file %+v", r)
	}

	// ---------- automatic: found, downloaded, ready in one go ----------
	app = start("b", "1.12.0", nil)
	if s = app.u.check(false); s.Phase != "ready" || s.Error != nil || !slices.Equal(app.phases(), []string{"checking", "available", "downloading", "ready"}) {
		t.Fatalf("automatic %+v %v", s, app.phases())
	}

	// A newer release replaces the download that waited.
	gh.set(newRelease("1.14.0"))
	if s = app.u.check(true); s.Phase != "ready" || s.Latest.Version != "1.14.0" || dirNames(app.dir) != "WoW-Tools-Forever-Setup-1.14.0.exe,update.json" {
		t.Fatalf("newer release %+v %s", s, dirNames(app.dir))
	}

	// After the update the new version tidies up and says so.
	os.WriteFile(filepath.Join(app.dir, "WoW-Tools-Forever-Setup-1.15.0.exe.part"), []byte("half"), 0o644)
	app = start("b", "1.14.0", func(o *updaterOpts) { o.updated = true })
	if dirNames(app.dir) != "update.json" {
		t.Fatalf("not tidied: %s", dirNames(app.dir))
	}
	if s = app.u.check(false); s.Phase != "current" || !s.Updated || s.Latest.Version != "1.14.0" {
		t.Fatalf("after the update %+v", s)
	}

	// A file that cannot be removed yet (the installer that still runs right after the update; here a folder of
	// that name) does not keep the rest from going, and the next check tries again.
	locked := filepath.Join(app.dir, "WoW-Tools-Forever-Setup-1.13.0.exe")
	os.MkdirAll(filepath.Join(locked, "in-use"), 0o755)
	os.WriteFile(filepath.Join(app.dir, "WoW-Tools-Forever-Setup-1.14.0.exe"), []byte("done"), 0o644)
	app = start("b", "1.14.0", nil)
	if dirNames(app.dir) != "WoW-Tools-Forever-Setup-1.13.0.exe,update.json" {
		t.Fatalf("locked file: %s", dirNames(app.dir))
	}
	os.RemoveAll(locked)
	os.WriteFile(locked, []byte("released"), 0o644)
	app.u.check(false)
	if dirNames(app.dir) != "update.json" {
		t.Fatalf("released file stays: %s", dirNames(app.dir))
	}

	// Turning the automatic on fetches an update that was only shown.
	setAuto(false)
	gh.set(newRelease("1.15.0"))
	if s = app.u.check(true); s.Phase != "available" || app.u.settingsChanged().Phase != "available" {
		t.Fatalf("shown only %+v", s)
	}
	setAuto(true)
	if s = app.u.settingsChanged(); s.Phase != "ready" {
		t.Fatalf("automatic on %+v", s)
	}

	// ---------- started from source, or no Windows: it tells, it does not install ----------
	app = start("c", "1.12.0", func(o *updaterOpts) { o.reason = "source" })
	before = gh.total()
	s = app.u.check(false)
	if s.Phase != "available" || s.CanInstall || deref(s.Reason) != "source" || s.Latest.Version != "1.15.0" || gh.count(before, "/assets/21") != 0 {
		t.Fatalf("from source %+v", s)
	}
	if app.u.download().Phase != "available" || app.u.install(true).Started || app.u.installsOnQuit() {
		t.Fatal("installs from source")
	}
	// A checkout ahead of the release is current.
	if s = start("c", "1.16.0", func(o *updaterOpts) { o.reason = "source" }).u.check(false); s.Phase != "current" {
		t.Fatalf("ahead %+v", s)
	}

	// ---------- releases the app does not take ----------
	refused := func(dir string, rel *appRelease, pattern, phase string) *run {
		t.Helper()
		gh.set(rel)
		r := start(dir, "1.12.0", nil)
		st := r.u.check(false)
		if !regexp.MustCompile(pattern).MatchString(errOf(st)) || st.Phase != phase || len(leftovers(r.dir)) != 0 {
			t.Fatalf("%s: %+v (%s) %v", dir, st, errOf(st), leftovers(r.dir))
		}
		return r
	}
	withTag := newRelease("1.13.0")
	withTag.tag = "nightly"
	refused("r1", withTag, `Unexpected release name: nightly`, "idle")
	evil := newRelease("1.13.0")
	evil.name = "evil.exe"
	refused("r2", evil, `Release v1\.13\.0 has no installer`, "idle")
	noSums := newRelease("1.13.0")
	none := "-"
	noSums.sums = &none
	refused("r3", noSums, `Release v1\.13\.0 has no checksum for WoW-Tools-Forever-Setup-1\.13\.0\.exe`, "idle")
	otherSums := newRelease("1.13.0")
	other := hexA + "  another-file.exe\n"
	otherSums.sums = &other
	refused("r4", otherSums, `has no checksum for`, "idle")
	badDigest := newRelease("1.13.0")
	badDigest.digest = "sha256:" + hexA
	refused("r5", badDigest, `The checksums of release v1\.13\.0 do not agree`, "idle")
	// GitHub's digest and the release's own list agree: fine.
	good := newRelease("1.13.0")
	good.digest = "sha256:" + sum256(good.exe)
	gh.set(good)
	if s = start("r6", "1.12.0", nil).u.check(false); s.Phase != "ready" {
		t.Fatalf("digest that agrees %+v", s)
	}

	// ---------- downloads that go wrong leave nothing behind and can be tried again ----------
	for dir, send := range map[string]string{"d1": "other", "d2": "long", "d3": "cut"} {
		rel := newRelease("1.13.0")
		rel.send = send
		pattern := "does not match its checksum"
		if send == "cut" {
			pattern = "The download of the update stopped"
		}
		refused(dir, rel, pattern, "available")
	}
	stall := newRelease("1.13.0")
	stall.send = "stall"
	gh.set(stall)
	app = start("d4", "1.12.0", func(o *updaterOpts) { o.src.idle = 300 * time.Millisecond })
	s = app.u.check(false)
	if !strings.Contains(errOf(s), "The download of the update stopped (no data for 0 s)") || dirNames(app.dir) != "update.json" {
		t.Fatalf("stalled %+v %s", s, dirNames(app.dir))
	}
	gh.set(newRelease("1.13.0"))
	if s = app.u.download(); s.Phase != "ready" || s.Error != nil {
		t.Fatalf("second try %+v", s)
	}

	// ---------- the real launcher ----------
	err := launchInstaller(filepath.Join(scratch, "no-such-setup.exe"), true)
	wantErr(t, err, `The installer could not be started \(`)
	// An installer that does not start leaves the update ready for another try.
	app = start("d4", "1.12.0", func(o *updaterOpts) {
		o.launch = func(string, bool) error { return launchInstaller(filepath.Join(scratch, "no-such-setup.exe"), true) }
	})
	app.u.check(false)
	if r = app.u.install(true); r.Started || r.Phase != "ready" || !strings.Contains(deref(r.Error), "could not be started") {
		t.Fatalf("failed start %+v", r)
	}

	// ---------- without network the last answer counts, and the reason is told ----------
	server.Close()
	if s = app.u.check(true); s.Phase != "ready" || s.Latest.Version != "1.13.0" || !strings.Contains(errOf(s), "GitHub cannot be reached") {
		t.Fatalf("offline %+v", s)
	}
	setLanguage("de")
	if s = start("e", "1.12.0", nil).u.check(false); s.Phase != "idle" || s.Latest != nil || !strings.Contains(errOf(s), "GitHub nicht erreichbar") {
		t.Fatalf("German offline %+v", s)
	}
}
