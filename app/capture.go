package main

// --capture=<folder>: renders every page once and saves screenshots, for checking the UI without clicking.
// With --lang=en or --lang=de the pages come in that language, whatever the settings say, and --char=<name>
// picks the character. Next to the screenshots goes report.json with the error each page showed, if any. With
// --require=<pages> (comma separated) the app exits with code 1 when one of these pages shows an error or
// never finishes; CI uses that as a smoke test of the installed app.
//
// The page is driven by scripts the app runs in it (evalJS); each one answers through CaptureReply.

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

type reply struct {
	value json.RawMessage
	err   string
}

type replies struct {
	mu   sync.Mutex
	next int
	wait map[int]chan reply
}

// CaptureReply is how a script of evalJS answers: the value as JSON, or the error it threw.
func (a *App) CaptureReply(id int, value string, errText string) {
	a.replies.mu.Lock()
	ch := a.replies.wait[id]
	delete(a.replies.wait, id)
	a.replies.mu.Unlock()
	if ch != nil {
		ch <- reply{json.RawMessage(value), errText}
	}
}

// evalJS runs an expression in the page (it may be a promise) and returns what it gave.
func (a *App) evalJS(expr string) (json.RawMessage, error) {
	a.replies.mu.Lock()
	if a.replies.wait == nil {
		a.replies.wait = map[int]chan reply{}
	}
	a.replies.next++
	id := a.replies.next
	ch := make(chan reply, 1)
	a.replies.wait[id] = ch
	a.replies.mu.Unlock()
	wails.WindowExecJS(a.ctx, fmt.Sprintf(`(async () => {
  let value = null, error = '';
  try { value = await (%s); } catch (e) { error = String(e && e.stack || e); }
  window.go.main.App.CaptureReply(%d, JSON.stringify(value === undefined ? null : value), error);
})();`, expr, id))
	select {
	case r := <-ch:
		if r.err != "" {
			return nil, errors.New(r.err)
		}
		return r.value, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("no answer from the page to %s", truncate(expr, 80))
	}
}

func (a *App) evalBool(expr string) bool {
	v, err := a.evalJS(expr)
	var b bool
	return err == nil && json.Unmarshal(v, &b) == nil && b
}

func (a *App) runJS(expr string) { a.evalJS(expr) }

func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func wait(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

func (a *App) runCapture(dir string) {
	required := []string{}
	for _, p := range strings.Split(argValue("require"), ",") {
		if p != "" {
			required = append(required, p)
		}
	}
	// A relative folder is below the working folder of the installed app, or below app/ in a checkout.
	base := filepath.Join(a.root, "app")
	if a.packaged {
		base, _ = os.Getwd()
	}
	out := dir
	if !filepath.IsAbs(out) {
		out = filepath.Join(base, dir)
	}
	failed, err := a.capturePages(out, required)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture failed: %v\n", err)
		os.Exit(1)
	}
	for _, line := range failed {
		fmt.Fprintf(os.Stderr, "capture: %s\n", line)
	}
	if len(failed) > 0 {
		os.Exit(1)
	}
	os.Exit(0)
}

type pageReport struct {
	Idle    bool     `json:"idle"`
	Error   *string  `json:"error"`
	Notices []string `json:"notices"`
}

func (a *App) shot(out, name string) error {
	img, err := captureWindow()
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(out, name+".png"))
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func (a *App) capturePages(out string, required []string) ([]string, error) {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	pages := []string{"connection", "character", "writs", "upgrades", "rotations", "recipes", "consumables", "pricehistory", "goldperhour"}
	exe, _ := os.Executable()
	report := map[string]any{"language": language(), "version": a.version, "packaged": a.packaged, "bridge": exe}
	results := map[string]pageReport{}
	// The Connection page shows the lists for the game. They arrive after the page; wait for them like for any
	// other page's data (the window hears the finished sync through "game-data").
	a.gameData()
	wait(300)
	var shotErrors []string
	shot := func(name string) {
		if err := a.shot(out, name); err != nil {
			shotErrors = append(shotErrors, name+": "+err.Error())
		}
	}
	for _, page := range pages {
		a.runJS(fmt.Sprintf("window.__showPage(%s)", jsQuote(page)))
		idle := false
		for i := 0; i < 600 && !idle; i++ {
			idle = a.evalBool("window.__idle === true")
			if !idle {
				wait(500)
			}
		}
		wait(400)
		// error: the page as a whole failed. notices: error boxes inside a page that otherwise rendered, e.g. no
		// Auctionator scan yet.
		var found struct {
			Error   *string  `json:"error"`
			Notices []string `json:"notices"`
		}
		v, err := a.evalJS(`({
      error: (document.querySelector('#content > .notice.error') || {}).textContent || null,
      notices: [...document.querySelectorAll('#content .notice.error')].map((el) => el.textContent),
    })`)
		if err == nil {
			json.Unmarshal(v, &found)
		}
		notices := []string{}
		for _, n := range found.Notices {
			if found.Error == nil || n != *found.Error {
				notices = append(notices, n)
			}
		}
		results[page] = pageReport{Idle: idle, Error: found.Error, Notices: notices}
		shot(page)
		switch page {
		case "connection":
			a.captureScrolled(shot, "#lists", "connection-lists")
			a.captureUpdate(shot)
		case "character":
			// A class that heals has the heal tab.
			if a.clickTab(`#page-tabs [data-tab="heal"]`) {
				shot("character-healing")
				a.captureScrolled(shot, "#c-heal .heal-grid", "character-healing-ranks")
			}
			// A class that deals damage with spells has the damage tab.
			if a.clickTab(`#page-tabs [data-tab="dmg"]`) {
				shot("character-damage")
				a.captureScrolled(shot, "#c-dmg .dmg-spells", "character-damage-spells")
				a.captureScrolled(shot, "#c-dmg .dmg-grid", "character-damage-fights")
			}
			if a.clickTab(`#page-tabs [data-tab="cons"]`) {
				shot("character-consumables")
				a.captureScrolled(shot, "#c-cons .cons-grid", "character-consumables-kinds")
			}
			a.clickTab(`#page-tabs [data-tab="gear"]`)
		case "upgrades":
			a.captureUpgradeDetails(shot)
		case "rotations":
			a.captureRotationDetails(shot)
		case "writs":
			if a.clickTab(`#page-tabs [data-tab="crates"]`) {
				shot("writs-crates")
				a.clickTab(`#page-tabs [data-tab="writs"]`)
			}
		case "recipes":
			a.captureHelp(shot, "#content th .help", "help")
		case "pricehistory":
			a.captureHelp(shot, ".ph-chart .hit:last-of-type", "pricehistory-day")
		case "consumables":
			if a.clickTab(`#page-tabs [data-tab="kinds"]`) {
				shot("consumables-kinds")
			}
			if a.clickTab(`#page-tabs [data-tab="buffs"]`) {
				shot("consumables-buffs")
			}
			a.clickTab(`#page-tabs [data-tab="fight"]`)
			a.captureConsumablesByValue(shot)
		case "goldperhour":
			// Open the first finished session, then shoot settings and history.
			a.runJS(`document.querySelector("[data-gph-open]")?.click()`)
			wait(600)
			a.captureScrolled(shot, "#gph-settings", "goldperhour-history")
		}
	}
	report["pages"] = results
	if len(shotErrors) > 0 {
		report["screenshotErrors"] = shotErrors
	}
	data, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "report.json"), append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	var failed []string
	for _, page := range required {
		r, ok := results[page]
		switch {
		case !ok:
			failed = append(failed, "unknown page "+page)
		case !r.Idle:
			failed = append(failed, page+" did not finish")
		case r.Error != nil:
			failed = append(failed, page+": "+*r.Error)
		}
	}
	// A screenshot that failed (a session without a desktop) is in the report but fails nothing: --require
	// is about the pages.
	return failed, nil
}

// clickTab switches a real tab bar on a page before taking its screenshot.
func (a *App) clickTab(selector string) bool {
	if !a.evalBool(fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (el) el.click(); return !!el; })()`, jsQuote(selector))) {
		return false
	}
	wait(300)
	return true
}

// captureScrolled is a second shot of a long page, scrolled to selector; it scrolls back to the top afterwards.
func (a *App) captureScrolled(shot func(string), selector, name string) {
	if !a.evalBool(fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (el) el.scrollIntoView(); return !!el; })()`, jsQuote(selector))) {
		return
	}
	wait(300)
	shot(name)
	a.runJS(`document.querySelector(".content").scrollTop = 0`)
}

// captureHelp shows one explanation behind a "?" (by default the first one of a table header on the page), or
// the popover of any other element with data-help.
func (a *App) captureHelp(shot func(string), selector, name string) {
	if !a.evalBool(fmt.Sprintf("window.__showHelp(%s)", jsQuote(selector))) {
		return
	}
	wait(300)
	shot(name)
	a.runJS(`document.body.click(); document.querySelector(".content").scrollTop = 0`)
}

// captureUpdate is the Connection page with an update of the app that waits to be installed. Screenshots never
// ask GitHub, so the state is made up: the next patch version, downloaded.
func (a *App) captureUpdate(shot func(string)) {
	real := a.updater.state()
	var major, minor, patch int
	fmt.Sscanf(real.Current, "%d.%d.%d", &major, &minor, &patch)
	version := fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
	now := time.Now().UnixMilli()
	published := time.Now().UTC().Format(time.RFC3339)
	fake := real
	fake.CanInstall, fake.Reason, fake.Phase, fake.CheckedAt, fake.Error = true, nil, "ready", &now, nil
	fake.Latest = &updateLatest{Version: version, Tag: "v" + version, URL: releaseURL(version, ""), PublishedAt: &published, Size: 32 << 20}
	wails.EventsEmit(a.ctx, "app-update", fake)
	wait(400)
	shot("connection-update")
	wails.EventsEmit(a.ctx, "app-update", real)
	wait(300)
}

// captureConsumablesByValue is the consumables page with the set by value: the buying order. It switches back
// to the strongest set, which the pages after it (and the Character page of a second run) expect.
func (a *App) captureConsumablesByValue(shot func(string)) {
	choose := func(value string) bool {
		return a.evalBool(fmt.Sprintf(`(() => {
      const el = document.querySelector('#c-set');
      if (!el) return false;
      el.value = %s;
      el.dispatchEvent(new Event('change', { bubbles: true }));
      return true;
    })()`, jsQuote(value)))
	}
	// The select renders the page again by itself, so __idle does not say when it is done; the spinner does.
	idle := func() {
		wait(300)
		for i := 0; i < 600 && a.evalBool(`!!document.querySelector("#content .loading")`); i++ {
			wait(500)
		}
		wait(400)
	}
	if !choose("value") {
		return
	}
	idle()
	shot("consumables-value")
	choose("best")
	idle()
}

// captureRotationDetails: the played fight of the first card that has one, opened, the fights against several
// enemies, and the second guide of a class with two (a priest heals and deals damage).
func (a *App) captureRotationDetails(shot func(string)) {
	if a.evalBool(`(() => { const el = document.querySelector("details.rot-timeline"); if (!el) return false; el.open = true; el.scrollIntoView(); return true; })()`) {
		wait(300)
		shot("rotations-timeline")
		a.runJS(`document.querySelector("details.rot-timeline").open = false; document.querySelector(".content").scrollTop = 0`)
	}
	a.captureScrolled(shot, `.rot-card[data-key="pack"]`, "rotations-enemies")
	a.captureScrolled(shot, ".rot-guide + .rot-guide", "rotations-second")
}

func (a *App) captureUpgradeDetails(shot func(string)) {
	// What the combat model does with the spells and talents: the list is closed by default.
	if a.evalBool(`(() => { const el = document.querySelector("details.coverage"); if (!el) return false; el.open = true; el.scrollIntoView(); return true; })()`) {
		wait(300)
		shot("upgrades-coverage")
		a.runJS(`document.querySelector("details.coverage").open = false; document.querySelector(".content").scrollTop = 0`)
	}
	// The upgrades page is longer than the window: a second shot of the cards per slot below the shopping list.
	if a.evalBool(`(() => { const el = document.querySelector(".slot-card"); if (el) el.scrollIntoView(); return !!el; })()`) {
		wait(300)
		shot("upgrades-slots")
		a.runJS(`document.querySelector(".content").scrollTop = 0`)
	}
	// One tooltip with comparison: the second line of the shopping list against the equipped item.
	if a.evalBool("window.__showTip(1)") {
		wait(300)
		shot("tooltip")
		a.runJS(`document.querySelector("#wtip").classList.remove("show")`)
	}
}
