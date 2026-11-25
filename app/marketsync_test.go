package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kejith/wow-tools-forever/sim/tools/marketsync"
)

func makeGzipBytes(t *testing.T, v any) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if err := json.NewEncoder(gz).Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func makeFakeAuctionator(t *testing.T, wowDir string) {
	t.Helper()
	wtf := filepath.Join(wowDir, "WTF")
	svDir := filepath.Join(wtf, "Account", "12345", "SavedVariables")
	if err := os.MkdirAll(svDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtf, "Config.wtf"), []byte("SET portal \"EU\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Minimal valid CBOR map for {"2770": {"h": {"2519": 220}, "l": {"2519": 195}, "a": {"2519": 100}, "m": 195}}
	cborBytes := []byte{
		0xa1, // map(1)
		0x64, '2', '7', '7', '0',
		0xa4, // map(4)
		0x61, 'h', 0xa1, 0x64, '2', '5', '1', '9', 0x18, 220,
		0x61, 'l', 0xa1, 0x64, '2', '5', '1', '9', 0x18, 195,
		0x61, 'a', 0xa1, 0x64, '2', '5', '1', '9', 0x18, 100,
		0x61, 'm', 0x18, 195,
	}
	var esc strings.Builder
	for _, b := range cborBytes {
		if b >= 32 && b < 127 && b != '"' && b != '\\' {
			esc.WriteByte(b)
		} else {
			esc.WriteString("\\")
			esc.WriteString(pad3(int(b)))
		}
	}
	lua := "AUCTIONATOR_PRICE_DATABASE = {\n[\"__dbversion\"] = 8,\n[\"PvP Horde\"] = \"" + esc.String() + "\",\n}\n"
	if err := os.WriteFile(filepath.Join(svDir, "Auctionator.lua"), []byte(lua), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pad3(n int) string {
	return string([]byte{byte('0' + n/100), byte('0' + (n/10)%10), byte('0' + n%10)})
}

func TestMarketSyncEndToEndPushPullAndInGameWrite(t *testing.T) {
	wow := t.TempDir()
	userData := t.TempDir()
	makeFakeAuctionator(t, wow)

	// Install WoWToolsForever.toc with SyncHistory.lua so WriteSyncHistory runs
	addonDir := filepath.Join(wow, "Interface", "AddOns", "WoWToolsForever")
	if err := os.MkdirAll(addonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(addonDir, "WoWToolsForever.toc"), []byte("## Version: 2.2.0\nAppData.lua\nSyncHistory.lua\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snapBytes, snapSHA := makeGzipBytes(t, syncSnapshotBlob{
		Schema:    1,
		Market:    "eu/pvp/horde",
		Version:   1,
		MaxDay:    2518,
		UpdatedAt: 1764000000,
		Latest:    map[string][2]int64{"2770": {180, 2518}},
		Items:     map[string][][5]int64{"2770": {{2500, 140, 160, 200, 3}, {2518, 180, 200, 300, 3}}},
	})
	dayBytes, daySHA := makeGzipBytes(t, syncDayBlob{
		Schema:    1,
		Market:    "eu/pvp/horde",
		Day:       2519,
		Rev:       1,
		UpdatedAt: 1764060000,
		Rows:      [][]int64{{2770, 190, 220, 400, 4, 190}, {2771, 500, 550, 20, 2, 500}},
	})

	uploads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/markets/eu/pvp/horde/uploads":
			uploads++
			gz, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Fatalf("upload not gzip: %v", err)
			}
			defer gz.Close()
			var body map[string]any
			if err := json.NewDecoder(gz).Decode(&body); err != nil {
				t.Fatalf("decode upload: %v", err)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"accepted":1}`))
		case "/v1/markets/eu/pvp/horde/manifest.json":
			etag := `"v1"`
			w.Header().Set("ETag", etag)
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			_ = json.NewEncoder(w).Encode(syncManifest{
				Schema:       1,
				Market:       "eu/pvp/horde",
				RealmDisplay: "PvP",
				Faction:      "Horde",
				UpdatedAt:    1764060000,
				FirstDay:     2500,
				LatestDay:    2519,
				TotalItems:   2,
				Snapshot:     &syncSnapshotRef{Version: 1, MaxDay: 2518, URL: "/v1/markets/eu/pvp/horde/snapshots/1.json.gz", SHA256: snapSHA, Bytes: len(snapBytes)},
				Days:         []syncManifestDay{{Day: 2519, Rev: 1, URL: "/v1/markets/eu/pvp/horde/days/2519/1.json.gz", SHA256: daySHA, Bytes: len(dayBytes), Items: 2}},
			})
		case "/v1/markets/eu/pvp/horde/snapshots/1.json.gz":
			_, _ = w.Write(snapBytes)
		case "/v1/markets/eu/pvp/horde/days/2519/1.json.gz":
			_, _ = w.Write(dayBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := &App{
		version:  "test",
		userData: userData,
		settings: map[string]any{
			"wowDir":               wow,
			"marketSyncEnabled":    true,
			"marketSyncInGame":     true,
			"marketSyncInGameDays": float64(180),
			"marketSyncUrl":        srv.URL,
			"sendToGame":           false,
		},
	}
	a.syncer = &marketSyncer{app: a, client: srv.Client()}

	// First sync: uploads 1 item-day, downloads snapshot+day, materializes merged cache, writes SyncHistory.lua.
	if !a.syncer.syncAll(true) {
		t.Fatal("expected first sync to upload")
	}
	if uploads != 1 {
		t.Fatalf("uploads = %d, want 1", uploads)
	}
	st := a.syncer.getState()
	if st.Phase != "synced" || st.TotalItems != 2 || !st.InGameWritten || st.InGameItems != 2 {
		t.Fatalf("unexpected sync state: %+v", st)
	}

	// Verify merged cache readable by sim/internal/marketsync
	cachePath := marketsync.MergedCachePath(a.marketSyncDir(), marketsync.MarketKey{Region: "eu", Realm: "pvp", Faction: "horde"})
	cached, err := marketsync.ReadCache(cachePath)
	if err != nil || len(cached.Items["2770"]) != 3 || len(cached.Items["2771"]) != 1 {
		t.Fatalf("unexpected cached data: %+v err=%v", cached, err)
	}

	// Verify SyncHistory.lua written
	if !exists(filepath.Join(addonDir, "SyncHistory.lua")) {
		t.Fatal("expected SyncHistory.lua to be written")
	}

	// Second sync with unchanged Auctionator.lua: delta upload skips POST (uploads stays 1) and manifest returns 304.
	if a.syncer.syncAll(true) {
		t.Fatal("expected second sync to skip upload (unchanged)")
	}
	if uploads != 1 {
		t.Fatalf("expected uploads to remain 1, got %d", uploads)
	}
}
