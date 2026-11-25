package ingame

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/tools/marketsync"
)

func TestWriteSyncHistoryAndHashDedup(t *testing.T) {
	wow := t.TempDir()
	addon := filepath.Join(wow, "Interface", "AddOns", "WoWToolsForever")
	if err := os.MkdirAll(addon, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(addon, "WoWToolsForever.toc"), []byte("## Version: 2.2.0\nAppData.lua\nSyncHistory.lua\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	synced := &marketsync.SyncedData{
		Schema:       1,
		Market:       "eu/pvp/horde",
		RealmDisplay: "PvP",
		Faction:      "Horde",
		UpdatedAt:    1764060300,
		FirstDay:     2400,
		LatestDay:    2519,
		Latest:       map[string][2]int64{"2770": {195, 2519}},
		Items: map[string][][5]int64{
			"2770": {
				{2400, 10, 15, 100, 2},   // trimmed when maxDays=30
				{2518, 190, 190, 200, 3}, // high omitted when == low
				{2519, 195, 220, 4546, 4},
			},
		},
	}

	res, err := WriteSyncHistory(wow, synced, 30)
	if err != nil {
		t.Fatalf("WriteSyncHistory: %v", err)
	}
	if !res.Written || res.Items != 1 || res.Days != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}
	raw, err := os.ReadFile(filepath.Join(addon, SyncHistoryFile))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `[2770] = "195|2518,190,,200;2519,195,220,4546"`) {
		t.Fatalf("unexpected encoded item line:\n%s", text)
	}

	// Second write with identical data should skip writing (unchanged).
	res2, err := WriteSyncHistory(wow, synced, 30)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Written || !res2.Unchanged {
		t.Fatalf("expected unchanged on second write: %+v", res2)
	}
}
