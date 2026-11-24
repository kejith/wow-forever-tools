package main

// Lists in the game: the WoWToolsForever addon shows the app's results in the game (a window with the lists, Auctionator
// shopping lists, tooltip lines and vendor hints; the bridge command ingame). The bridge computes them for
// every character and writes AppData.lua into the installed addon, which the client loads on the next
// /reload. That happens at the start, whenever the game has saved (the watcher) and after an addon install,
// unless settings.sendToGame is false; the file is only rewritten when its content changed. One run at a time.

import (
	"encoding/json"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type gameRun struct {
	done   chan struct{}
	result any
}

type gameSync struct {
	mu   sync.Mutex
	last *gameRun // the last run started, perhaps still running
}

func (a *App) sendsToGame() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings["sendToGame"] != false
}

// syncGameData queues a run of the bridge command ingame (with --write when write is true) and returns it.
func (a *App) syncGameData(write bool) *gameRun {
	a.game.mu.Lock()
	previous := a.game.last
	run := &gameRun{done: make(chan struct{})}
	a.game.last = run
	a.game.mu.Unlock()
	go func() {
		defer close(run.done)
		if previous != nil {
			<-previous.done
		}
		answer := a.runBridge("ingame", map[string]any{"write": write})
		result := map[string]any{}
		if raw, ok := answer.(json.RawMessage); ok {
			json.Unmarshal(raw, &result)
		} else {
			result = answer.(map[string]any)
		}
		result["automatic"] = a.sendsToGame()
		run.result = result
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "game-data", result)
		}
	}()
	return run
}

// gameData is the last sync, or the one that is running; the first call starts one.
func (a *App) gameData() any {
	a.game.mu.Lock()
	run := a.game.last
	a.game.mu.Unlock()
	if run == nil {
		run = a.syncGameData(a.sendsToGame())
	}
	<-run.done
	return run.result
}
