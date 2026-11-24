package main

// The game writes its SavedVariables on /reload and logout. The watcher looks at WoWToolsForever.lua, Auctionator.lua
// and GoldPerHour.lua below WTF/Account once a second; after 1.5 s without a further change (WoW writes
// several files in a row) it computes new lists for the game and tells the window ("data-changed"). Looking
// at the file times instead of waiting for change events also works where the folder is a network or WSL path.

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var watchedFile = regexp.MustCompile(`(?i)(WoWToolsForever|Auctionator|GoldPerHour)\.lua$`)

const (
	watchEvery = time.Second
	watchQuiet = 1500 * time.Millisecond
)

type fileStamp struct {
	size int64
	mod  time.Time
}

type watcher struct {
	mu   sync.Mutex
	stop chan struct{}
}

// stamps are the watched files below dir with their size and time.
func stamps(dir string) map[string]fileStamp {
	out := map[string]fileStamp{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !watchedFile.MatchString(d.Name()) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			out[p] = fileStamp{info.Size(), info.ModTime()}
		}
		return nil
	})
	return out
}

// startWatcher watches the account folder of the current game folder; a running watcher stops.
func (a *App) startWatcher() {
	a.watch.mu.Lock()
	defer a.watch.mu.Unlock()
	if a.watch.stop != nil {
		close(a.watch.stop)
	}
	stop := make(chan struct{})
	a.watch.stop = stop
	dir := filepath.Join(a.wowDir(), "WTF", "Account")
	go func() {
		known := stamps(dir)
		var changed string
		var since time.Time
		tick := time.NewTicker(watchEvery)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			now := stamps(dir)
			for p, s := range now {
				if known[p] != s {
					changed, since = p, time.Now()
				}
			}
			known = now
			if changed == "" || time.Since(since) < watchQuiet {
				continue
			}
			file, _ := filepath.Rel(dir, changed)
			changed = ""
			a.syncGameData(a.sendsToGame()) // new scan or snapshot: new lists for the game
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "data-changed", map[string]string{"file": file})
			}
		}
	}()
}
