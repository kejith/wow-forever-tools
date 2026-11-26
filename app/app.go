package main

// The app between the page and the game. It talks to the game only through files: it reads the SavedVariables
// the WoWToolsForever and GoldPerHour addons and Auctionator write on /reload or logout, and it writes AppRequest.lua
// into the installed addons, which the client loads on the next /reload. All calculations run in the bridge
// (bridge.go), which also writes AppData.lua into the WoWToolsForever addon: the lists the addon shows in the game.
// The addons themselves come from their GitHub releases (addon.go, releases.go), cached in the app's data
// folder; settings.addonSources = { wowtoolsforever: <folder>, goldperhour: <folder> } installs from a checkout instead.
// Language: settings.language (auto, en, de); auto follows the game client (textLocale in WTF/Config.wtf) and,
// without a client, the operating system. The bridge gets the same language, so its texts match the page.
// The installed app updates itself from the releases of its own repository (update.go).
//
// The exported methods of App are what the page calls (renderer/wails.js turns them into window.wow).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

const updateEvery = 6 * time.Hour

// App is bound to the page; its exported methods are window.go.main.App.*.
type App struct {
	ctx      context.Context
	version  string
	packaged bool   // the installed app, not a build from source
	root     string // where the bridge runs: the installation, or the checkout
	userData string // %APPDATA%\wowmcp-app: the name it had before the app was renamed, so settings stay
	capture  bool   // --capture=<folder>: screenshots of every page (capture.go)

	mu       sync.Mutex
	settings map[string]any

	updater  *updater
	syncer   *marketSyncer
	quitting atomic.Bool
	game         gameSync
	icons        iconCache
	watch        watcher
	replies      replies
	onFilterSync func(write bool) // test hook; nil uses syncGameData
}

// argValue is the value of --name=value on the command line, "" without one.
func argValue(name string) string {
	for _, a := range os.Args[1:] {
		if v, ok := strings.CutPrefix(a, "--"+name+"="); ok {
			return v
		}
	}
	return ""
}

func hasArg(name string) bool {
	for _, a := range os.Args[1:] {
		if a == "--"+name {
			return true
		}
	}
	return false
}

// defaultWow is the standard install; under WSL and Linux the Windows install is reached through /mnt/c.
func defaultWow() string {
	if runtime.GOOS == "windows" {
		return `C:\Program Files (x86)\World of Warcraft\_forever_`
	}
	return "/mnt/c/Program Files (x86)/World of Warcraft/_forever_"
}

// repoRoot is the checkout the app was started from: the first folder above the executable or the working
// folder that has sim/go.mod and app/renderer. "" if there is none.
func repoRoot() string {
	var starts []string
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, dir := range starts {
		for d := dir; ; d = filepath.Dir(d) {
			if exists(filepath.Join(d, "sim", "go.mod")) && exists(filepath.Join(d, "app", "renderer")) {
				return d
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return ""
}

func newApp(version string) *App {
	a := &App{version: version, packaged: packaged != "", capture: argValue("capture") != ""}
	if config, err := os.UserConfigDir(); err == nil {
		a.userData = filepath.Join(config, "wowmcp-app")
	}
	if exe, err := os.Executable(); err == nil && a.packaged {
		a.root = filepath.Dir(exe)
	} else if root := repoRoot(); root != "" {
		a.root = root
	} else {
		a.root, _ = os.Getwd()
	}
	a.loadSettings()
	return a
}

// cacheDir holds downloaded game data and icons. The installed app cannot write next to itself.
func (a *App) cacheDir() string {
	if a.packaged {
		return filepath.Join(a.userData, "game-data")
	}
	return filepath.Join(a.root, "scripts", "cache")
}

func (a *App) settingsFile() string { return filepath.Join(a.userData, "settings.json") }

func (a *App) loadSettings() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.settings = map[string]any{}
	if data, err := os.ReadFile(a.settingsFile()); err == nil {
		json.Unmarshal(data, &a.settings)
		if a.settings == nil {
			a.settings = map[string]any{}
		}
	}
	if w, _ := a.settings["wowDir"].(string); w == "" {
		a.settings["wowDir"] = defaultWow()
		if env := os.Getenv("WOW_FOREVER_DIR"); env != "" {
			a.settings["wowDir"] = env
		}
	}
	a.applyLanguageLocked()
}

func (a *App) saveSettingsLocked() error {
	if err := os.MkdirAll(a.userData, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(a.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.settingsFile(), data, 0o644)
}

func (a *App) wowDir() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	w, _ := a.settings["wowDir"].(string)
	return w
}

// applyLanguageLocked: --lang=en|de on the command line (screenshots) wins over the setting; "auto" follows the
// game client, and without one the operating system.
func (a *App) applyLanguageLocked() {
	wanted := argValue("lang")
	if wanted == "" {
		wanted, _ = a.settings["language"].(string)
	}
	if knownLanguage(wanted) {
		setLanguage(wanted)
		return
	}
	locale := clientLocale(a.settings["wowDir"].(string))
	if locale == "" {
		locale = osLocale()
	}
	setLanguage(languageOf(locale))
}

func (a *App) publicSettings() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]any{}
	for k, v := range a.settings {
		out[k] = v
	}
	if l, _ := out["language"].(string); !knownLanguage(l) {
		out["language"] = "auto"
	}
	out["activeLanguage"] = language()
	out["clientLocale"] = nullable(clientLocale(a.settings["wowDir"].(string)))
	out["version"] = a.version
	out["capture"] = a.capture
	out["captureChar"] = argValue("char")
	return out
}

func (a *App) addonSources() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]string{}
	if m, ok := a.settings["addonSources"].(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

func (a *App) addonOpts(force bool) addonOpts {
	return addonOpts{cacheDir: filepath.Join(a.userData, "addons"), sources: a.addonSources(), force: force}
}

func addonKey(key string) string {
	if _, ok := addons[key]; ok {
		return key
	}
	return "wowtoolsforever"
}

func (a *App) autoUpdate() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings["autoUpdate"] != false
}

// ---------- lifecycle ----------

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startWatcher()
	a.syncGameData(a.sendsToGame())
	a.startUpdater()
	a.startMarketSync()
}

func (a *App) domReady(ctx context.Context) {
	if dir := argValue("capture"); dir != "" {
		go a.runCapture(dir)
	}
}

// beforeClose: a downloaded update goes in when the app closes; the quit waits until the installer is started.
func (a *App) beforeClose(ctx context.Context) bool {
	if a.quitting.Load() || a.updater == nil || !a.updater.installsOnQuit() {
		return false
	}
	a.quitting.Store(true)
	go func() {
		a.updater.install(false)
		wails.Quit(ctx)
	}()
	return true
}

// ---------- app update ----------
// The installed app looks for a newer release of its repository at the start and every six hours. Unless
// settings.autoUpdate is false it downloads the installer right away and runs it when the app closes;
// "Restart and update" runs it now. Started from source, or not on Windows, the app only tells that there is
// a newer version.

func (a *App) startUpdater() {
	reason := ""
	if !a.packaged {
		reason = "source"
	} else if runtime.GOOS != "windows" {
		reason = "platform"
	}
	a.updater = newUpdater(updaterOpts{
		dir: filepath.Join(a.userData, "update"), current: a.version, reason: reason, automatic: a.autoUpdate,
		updated: hasArg("updated"), // the installer started the app after an update
		onState: func(s updateState) {
			if a.ctx != nil {
				wails.EventsEmit(a.ctx, "app-update", s)
			}
		},
		// For trying an update against a stand-in for the GitHub API (update_test.go has one).
		src: updateSource{api: os.Getenv("WOW_TOOLS_UPDATE_API")},
	})
	if a.capture {
		return // screenshots do not ask GitHub
	}
	go func() {
		a.updater.check(false)
		for range time.Tick(updateEvery) {
			a.updater.check(false)
		}
	}()
}

// ---------- what the page calls ----------

func fail(err error) any { return errorAnswer(err.Error()) }

// Bridge answers one bridge command (see bridgeArgs for the options).
func (a *App) Bridge(command string, opts map[string]any) any { return a.runBridge(command, opts) }

// AddonInfo is the state of an addon: installed, its source, what changed; opts.force asks GitHub now.
func (a *App) AddonInfo(key string, opts map[string]any) any {
	info, err := addonInfo(a.wowDir(), addonKey(key), a.addonOpts(opts != nil && opts["force"] == true))
	if err != nil {
		return fail(err)
	}
	return info
}

// InstallAddon installs or updates an addon from its source.
func (a *App) InstallAddon(key string) any {
	info, err := installAddon(a.wowDir(), addonKey(key), a.addonOpts(false))
	if err != nil {
		return fail(err)
	}
	if addonKey(key) == "wowtoolsforever" {
		a.syncGameData(a.sendsToGame()) // a new addon may be the first one that can show lists
	}
	return info
}

// GphRequest sends GoldPerHour a request: {action: start|stop|reset, settings: {…}}.
func (a *App) GphRequest(req map[string]any) any {
	settings, _ := req["settings"].(map[string]any)
	r, err := writeGphRequest(a.wowDir(), req["action"], settings)
	if err != nil {
		return fail(err)
	}
	return r
}

// GphClearRequest withdraws GoldPerHour's open request.
func (a *App) GphClearRequest() any {
	r, err := clearGphRequest(a.wowDir())
	if err != nil {
		return fail(err)
	}
	return r
}

// CreateRequest asks WoWToolsForever for an item export or a snapshot.
func (a *App) CreateRequest(action string, opts map[string]any) any {
	r, err := writeRequest(a.wowDir(), action, opts != nil && opts["full"] == true)
	if err != nil {
		return fail(err)
	}
	return r
}

// GameData is the last sync of the lists for the game, or the running one.
func (a *App) GameData() any { return a.gameData() }

// SendGameData computes the lists and writes them now.
func (a *App) SendGameData() any {
	run := a.syncGameData(true)
	<-run.done
	return run.result
}

func (a *App) UpdateState() updateState    { return a.updater.state() }
func (a *App) CheckUpdate() updateState    { return a.updater.check(true) }
func (a *App) DownloadUpdate() updateState { return a.updater.download() }

// MarketSyncState returns the current community AH price sync state.
func (a *App) MarketSyncState() marketSyncState {
	if a.syncer == nil {
		a.startMarketSync()
	}
	return a.syncer.getState()
}

// SyncMarketNow triggers an immediate upload+download cycle and returns the resulting state.
func (a *App) SyncMarketNow() marketSyncState {
	if a.syncer == nil {
		a.startMarketSync()
	}
	a.syncer.syncAll(true)
	return a.syncer.getState()
}

// InstallUpdate: the installer waits for the app to exit, replaces it and starts it again.
func (a *App) InstallUpdate() installResult {
	r := a.updater.install(true)
	if r.Started {
		a.quitting.Store(true)
		go wails.Quit(a.ctx)
	}
	return r
}

// OpenReleaseNotes opens the release page of the version the updater found; the address is built here, not
// handed in.
func (a *App) OpenReleaseNotes() {
	if l := a.updater.state().Latest; l != nil {
		wails.BrowserOpenURL(a.ctx, releaseURL(l.Version, ""))
	}
}

// Icons maps item IDs to FileDataIDs: {icons: {id: fdid|null}, fallback}.
func (a *App) Icons(ids []any) any { return a.resolveIcons(ids) }

// WowRunning tells whether WoW: Forever runs: {running, otherClient} or {running: false, unknown: true}.
func (a *App) WowRunning() map[string]bool { return wowRunning() }

// OpenPath shows a folder in the file manager; "" when it worked, as Electron's shell.openPath answered.
func (a *App) OpenPath(p string) string {
	if err := openPath(p); err != nil {
		return err.Error()
	}
	return ""
}

func (a *App) GetSettings() map[string]any { return a.publicSettings() }

// SetSettings changes settings and answers with all of them.
func (a *App) SetSettings(patch map[string]any) map[string]any {
	for _, k := range []string{"activeLanguage", "clientLocale", "version", "capture", "captureChar"} {
		delete(patch, k)
	}
	before := a.wowDir() + "|" + boolString(a.sendsToGame())
	beforeFilters := a.filterSignature()
	beforeSync := boolString(a.marketSyncEnabled()) + "|" + boolString(a.marketSyncInGame()) + "|" + strconv.Itoa(a.marketSyncInGameDays())
	autoUpdate := a.autoUpdate()
	a.mu.Lock()
	for k, v := range patch {
		switch k {
		case "excludedItemIds":
			a.settings[k] = parseIntList(v)
		case "excludedCategories":
			a.settings[k] = parseCategoryList(v)
		default:
			a.settings[k] = v
		}
	}
	if w, _ := a.settings["wowDir"].(string); w == "" {
		a.settings["wowDir"] = defaultWow()
	}
	a.saveSettingsLocked()
	a.applyLanguageLocked()
	a.mu.Unlock()
	a.startWatcher()
	if before != a.wowDir()+"|"+boolString(a.sendsToGame()) {
		a.syncGameData(a.sendsToGame())
	} else if beforeFilters != a.filterSignature() {
		a.notifyFiltersChanged()
	}
	if beforeSync != boolString(a.marketSyncEnabled())+"|"+boolString(a.marketSyncInGame())+"|"+strconv.Itoa(a.marketSyncInGameDays()) {
		if a.syncer != nil {
			a.syncer.restartLoop()
		}
		a.syncGameData(a.sendsToGame())
	}
	if autoUpdate != a.autoUpdate() && a.updater != nil {
		go a.updater.settingsChanged()
	}
	return a.publicSettings()
}

func boolString(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
