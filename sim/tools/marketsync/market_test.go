package marketsync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

func TestDetectPortalAndParseRealm(t *testing.T) {
	dir := t.TempDir()
	wtf := filepath.Join(dir, "WTF")
	if err := os.MkdirAll(wtf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtf, "Config.wtf"), []byte("SET portal \"EU\"\r\nSET textLocale \"deDE\"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectPortal(dir); got != "eu" {
		t.Fatalf("DetectPortal = %q, want eu", got)
	}
	mk, ok := MarketFromAuctionator(dir, "PvP Horde")
	if !ok || mk.Path() != "eu/pvp/horde" || mk.RealmDisplay != "PvP" || mk.DirName() != "eu-pvp-horde" {
		t.Fatalf("unexpected MarketKey: %+v ok=%v", mk, ok)
	}
	if _, _, _, ok := ParseAuctionatorRealm("UnknownRealm"); ok {
		t.Fatal("expected parse failure without faction suffix")
	}
}

func TestMergeTablesPreservesHistoryAndPicksNewerM(t *testing.T) {
	local := sv.NewTable()
	e2770 := sv.NewTable()
	h := sv.NewTable()
	h.Set("2519", int64(220))
	l := sv.NewTable()
	l.Set("2519", int64(200))
	a := sv.NewTable()
	a.Set("2519", int64(100))
	e2770.Set("h", h)
	e2770.Set("l", l)
	e2770.Set("a", a)
	e2770.Set("m", int64(205))
	local.Set("2770", e2770)
	local.Set("gr:15502:of the Monkey", sv.NewTable())

	synced := &SyncedData{
		Schema:    1,
		Market:    "eu/pvp/horde",
		UpdatedAt: 2000,
		FirstDay:  2480,
		LatestDay: 2519,
		Latest:    map[string][2]int64{"2770": {190, 2519}, "2771": {500, 2519}},
		Items: map[string][][5]int64{
			"2770": {
				{2480, 150, 180, 300, 3}, // older than 21-day Auctionator cutoff
				{2519, 190, 250, 150, 4}, // overlaps with local
			},
			"2771": {
				{2519, 500, 500, 40, 2}, // item missing locally
			},
		},
	}

	// Case 1: synced is newer (2000 > 1000) -> synced 'm' (190) wins
	merged := MergeTables(local, time.Unix(1000, 0), synced)
	it := merged.Get("2770").(*sv.Table)
	if it.Get("m") != int64(190) {
		t.Fatalf("expected synced m=190, got %v", it.Get("m"))
	}
	if it.Get("h").(*sv.Table).Get("2480") != int64(180) {
		t.Fatalf("missing backfilled day 2480")
	}
	if it.Get("l").(*sv.Table).Get("2519") != int64(190) || it.Get("h").(*sv.Table).Get("2519") != int64(250) {
		t.Fatalf("day 2519 range not merged: l=%v h=%v", it.Get("l").(*sv.Table).Get("2519"), it.Get("h").(*sv.Table).Get("2519"))
	}
	if it.Get("a").(*sv.Table).Get("2519") != int64(150) {
		t.Fatalf("day 2519 offers not merged: %v", it.Get("a").(*sv.Table).Get("2519"))
	}
	if merged.Get("2771") == nil || merged.Get("gr:15502:of the Monkey") == nil {
		t.Fatal("expected synced-only item and local gr: key to both exist")
	}

	// Case 2: local scan is newer (3000 > 2000) on same latest day -> local 'm' (205) wins
	mergedLocalWins := MergeTables(local, time.Unix(3000, 0), synced)
	if mergedLocalWins.Get("2770").(*sv.Table).Get("m") != int64(205) {
		t.Fatalf("expected newer local m=205, got %v", mergedLocalWins.Get("2770").(*sv.Table).Get("m"))
	}
}
