package main

// Community AH price history sync: uploads incremental Auctionator scans to the sync server
// (default https://ah.sane.run) and downloads consolidated realm+faction history (snapshots + day deltas)
// into <userData>/market-sync/<market>/merged.json.gz. Never mutates Auctionator.lua.
// Optionally writes SyncHistory.lua into the WoWToolsForever addon for in-game /wtf price.

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kejith/wow-tools-forever/sim/tools/ingame"
	"github.com/kejith/wow-tools-forever/sim/tools/marketsync"
	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultMarketSyncURL   = "https://ah.sane.run"
	marketSyncPollInterval = 30 * time.Minute
	marketSyncUploadDelay  = 2 * time.Second
	marketSyncPostPullWait = 12 * time.Second
	defaultInGameDays      = 180
)

type marketSyncState struct {
	Enabled         bool   `json:"enabled"`
	InGame          bool   `json:"inGame"`
	InGameDays      int    `json:"inGameDays"`
	Phase           string `json:"phase"` // off | idle | syncing | synced | error
	Market          string `json:"market,omitempty"`
	MarketPath      string `json:"marketPath,omitempty"`
	LastSyncAt      int64  `json:"lastSyncAt,omitempty"`
	LastUploadAt    int64  `json:"lastUploadAt,omitempty"`
	LastUploadItems int    `json:"lastUploadItems,omitempty"`
	FirstDay        int64  `json:"firstDay,omitempty"`
	LatestDay       int64  `json:"latestDay,omitempty"`
	DaysCount       int    `json:"daysCount,omitempty"`
	TotalItems      int    `json:"totalItems,omitempty"`
	InGameWritten   bool   `json:"inGameWritten,omitempty"`
	InGameItems     int    `json:"inGameItems,omitempty"`
	Error           string `json:"error,omitempty"`
}

type marketSyncer struct {
	mu      sync.Mutex
	app     *App
	client  *http.Client
	state   marketSyncState
	stop    chan struct{}
	running bool
}

type syncManifest struct {
	Schema       int                `json:"schema"`
	Market       string             `json:"market"`
	RealmDisplay string             `json:"realmDisplay"`
	Faction      string             `json:"faction"`
	UpdatedAt    int64              `json:"updatedAt"`
	FirstDay     int64              `json:"firstDay"`
	LatestDay    int64              `json:"latestDay"`
	TotalItems   int                `json:"totalItems"`
	Snapshot     *syncSnapshotRef   `json:"snapshot"`
	Days         []syncManifestDay  `json:"days"`
}

type syncSnapshotRef struct {
	Version int    `json:"version"`
	MaxDay  int64  `json:"maxDay"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Bytes   int    `json:"bytes"`
}

type syncManifestDay struct {
	Day       int64  `json:"day"`
	Rev       int    `json:"rev"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Bytes     int    `json:"bytes"`
	Items     int    `json:"items"`
	UpdatedAt int64  `json:"updatedAt"`
}

type syncSnapshotBlob struct {
	Schema    int                   `json:"schema"`
	Market    string                `json:"market"`
	Version   int                   `json:"version"`
	MaxDay    int64                 `json:"maxDay"`
	UpdatedAt int64                 `json:"updatedAt"`
	Latest    map[string][2]int64   `json:"latest"`
	Items     map[string][][5]int64 `json:"items"`
}

type syncDayBlob struct {
	Schema    int       `json:"schema"`
	Market    string    `json:"market"`
	Day       int64     `json:"day"`
	Rev       int       `json:"rev"`
	UpdatedAt int64     `json:"updatedAt"`
	Rows      [][]int64 `json:"rows"` // [itemId, low, high, offers, reporters, latestPrice]
}

type localPullMeta struct {
	ETag        string         `json:"etag"`
	SnapshotVer int            `json:"snapshotVer"`
	DayRevs     map[string]int `json:"dayRevs"` // dayStr -> rev
	UpdatedAt   int64          `json:"updatedAt"`
	FirstDay    int64          `json:"firstDay"`
	LatestDay   int64          `json:"latestDay"`
	TotalItems  int            `json:"totalItems"`
}

type localUploadState struct {
	// Key: "itemId:day", value: "low,high,offers,m"
	Sent map[string]string `json:"sent"`
}

func (a *App) marketSyncDir() string {
	return filepath.Join(a.userData, "market-sync")
}

func (a *App) marketSyncEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings["marketSyncEnabled"] == true
}

func (a *App) marketSyncInGame() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings["marketSyncInGame"] != false
}

func (a *App) marketSyncInGameDays() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n, ok := jsNumber(a.settings["marketSyncInGameDays"]); ok && n >= 0 {
		return int(n)
	}
	return defaultInGameDays
}

func (a *App) marketSyncURL() string {
	if env := strings.TrimSpace(os.Getenv("WOW_TOOLS_SYNC_URL")); env != "" {
		return strings.TrimRight(env, "/")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if u, ok := a.settings["marketSyncUrl"].(string); ok && strings.TrimSpace(u) != "" {
		return strings.TrimRight(strings.TrimSpace(u), "/")
	}
	return defaultMarketSyncURL
}

func (a *App) marketSyncInstallID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if id, ok := a.settings["marketSyncInstallId"].(string); ok && len(id) == 36 {
		return id
	}
	id := newUUIDv4()
	a.settings["marketSyncInstallId"] = id
	_ = a.saveSettingsLocked()
	return id
}

func newUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (a *App) startMarketSync() {
	if a.syncer == nil {
		a.syncer = &marketSyncer{
			app:    a,
			client: &http.Client{Timeout: 45 * time.Second},
		}
	}
	a.syncer.refreshConfig()
	if a.capture {
		return
	}
	a.syncer.restartLoop()
}

func (s *marketSyncer) restartLoop() {
	s.mu.Lock()
	if s.stop != nil {
		close(s.stop)
		s.stop = nil
	}
	enabled := s.app.marketSyncEnabled()
	if !enabled {
		s.state.Enabled = false
		s.state.Phase = "off"
		s.state.Error = ""
		st := s.state
		s.mu.Unlock()
		s.emit(st)
		return
	}
	stop := make(chan struct{})
	s.stop = stop
	s.mu.Unlock()

	go func() {
		s.syncAll(true)
		tick := time.NewTicker(marketSyncPollInterval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				s.syncAll(false)
			}
		}
	}()
}

func (s *marketSyncer) refreshConfig() {
	enabled := s.app.marketSyncEnabled()
	inGame := s.app.marketSyncInGame()
	inGameDays := s.app.marketSyncInGameDays()

	marketLabel, marketPath := "", ""
	if mk, ok := s.detectPrimaryMarket(); ok {
		marketLabel = mk.Display()
		marketPath = mk.Path()
	}

	s.mu.Lock()
	s.state.Enabled = enabled
	s.state.InGame = inGame
	s.state.InGameDays = inGameDays
	if marketLabel != "" {
		s.state.Market = marketLabel
		s.state.MarketPath = marketPath
	}
	if !enabled {
		s.state.Phase = "off"
		s.state.Error = ""
	} else if s.state.Phase == "" || s.state.Phase == "off" {
		s.state.Phase = "idle"
	}
	st := s.state
	s.mu.Unlock()
	s.emit(st)
}

func (s *marketSyncer) getState() marketSyncState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *marketSyncer) emit(st marketSyncState) {
	if s.app.ctx != nil {
		wails.EventsEmit(s.app.ctx, "market-sync", st)
	}
}

func (s *marketSyncer) detectPrimaryMarket() (marketsync.MarketKey, bool) {
	return marketsync.DetectPrimaryMarket(s.app.wowDir())
}

// onAuctionatorChanged is called by watcher.go when Auctionator.lua was updated by the game.
func (s *marketSyncer) onAuctionatorChanged() {
	if s == nil || !s.app.marketSyncEnabled() || s.app.capture {
		return
	}
	go func() {
		time.Sleep(marketSyncUploadDelay)
		uploaded := s.syncAll(true)
		if uploaded {
			// Server publisher debounces 5s; pull again shortly after so our own merged cache includes fresh consensus.
			time.Sleep(marketSyncPostPullWait)
			s.syncAll(false)
		}
	}()
}

// syncAll pushes any local Auctionator deltas (if push=true) and pulls fresh remote data.
// Returns true when an upload was actually accepted.
func (s *marketSyncer) syncAll(push bool) bool {
	if !s.app.marketSyncEnabled() {
		s.refreshConfig()
		return false
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.state.Enabled = true
	s.state.InGame = s.app.marketSyncInGame()
	s.state.InGameDays = s.app.marketSyncInGameDays()
	s.state.Phase = "syncing"
	s.state.Error = ""
	s.emit(s.state)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	wow := s.app.wowDir()
	baseURL := s.app.marketSyncURL()
	installID := s.app.marketSyncInstallID()
	syncDir := s.app.marketSyncDir()

	var market marketsync.MarketKey
	var hasMarket bool
	var uploaded bool
	var uploadCount int
	var lastErr error

	// 1. Discover realms from local Auctionator.lua and optionally push deltas.
	if scans, primary, ok, err := marketsync.ExtractAuctionatorScans(wow); err == nil && ok {
		market, hasMarket = primary, true
		if push {
			for _, sc := range scans {
				n, err := s.pushRealm(baseURL, syncDir, installID, sc)
				if err != nil {
					lastErr = err
				} else if n > 0 {
					uploaded = true
					uploadCount += n
				}
			}
		}
	}

	if !hasMarket {
		s.mu.Lock()
		s.state.Phase = "idle"
		s.state.Error = t("No Auctionator realm detected yet. Run an Auctionator scan in the game first.")
		st := s.state
		s.mu.Unlock()
		s.emit(st)
		return false
	}

	// 2. Pull remote manifest, snapshot, and changed day blobs.
	changed, meta, syncedData, pullErr := s.pullMarket(baseURL, syncDir, market)
	if pullErr != nil && lastErr == nil {
		lastErr = pullErr
	}

	// 3. Write SyncHistory.lua into WoWToolsForever addon if enabled and data is available.
	var igRes *ingame.SyncHistoryResult
	if s.app.marketSyncInGame() {
		if syncedData == nil {
			syncedData, _ = marketsync.ReadCache(marketsync.MergedCachePath(syncDir, market))
		}
		if syncedData != nil {
			if res, err := ingame.WriteSyncHistory(wow, syncedData, s.app.marketSyncInGameDays()); err == nil {
				igRes = res
			}
		}
	}

	s.mu.Lock()
	s.state.Market = market.Display()
	s.state.MarketPath = market.Path()
	if uploaded {
		s.state.LastUploadAt = time.Now().Unix()
		s.state.LastUploadItems = uploadCount
	}
	if meta != nil {
		s.state.LastSyncAt = time.Now().Unix()
		s.state.FirstDay = meta.FirstDay
		s.state.LatestDay = meta.LatestDay
		if meta.LatestDay >= meta.FirstDay && meta.FirstDay > 0 {
			s.state.DaysCount = int(meta.LatestDay - meta.FirstDay + 1)
		}
		s.state.TotalItems = meta.TotalItems
	}
	if igRes != nil {
		s.state.InGameWritten = igRes.Written || igRes.Unchanged
		s.state.InGameItems = igRes.Items
	}
	if lastErr != nil {
		s.state.Phase = "error"
		s.state.Error = lastErr.Error()
	} else {
		s.state.Phase = "synced"
		s.state.Error = ""
	}
	st := s.state
	s.mu.Unlock()
	s.emit(st)

	if changed && s.app.ctx != nil {
		s.app.syncGameData(s.app.sendsToGame())
		wails.EventsEmit(s.app.ctx, "data-changed", map[string]string{"file": "market-sync"})
	}
	return uploaded
}

func (s *marketSyncer) pushRealm(baseURL, syncDir, installID string, scan marketsync.RealmScan) (int, error) {
	mk := scan.Market
	statePath := filepath.Join(syncDir, mk.DirName(), "uploaded-state.json")
	prev := localUploadState{Sent: map[string]string{}}
	if raw, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(raw, &prev)
		if prev.Sent == nil {
			prev.Sent = map[string]string{}
		}
	}

	var entries [][]int64
	nextSent := make(map[string]string, len(prev.Sent))
	for k, v := range prev.Sent {
		nextSent[k] = v
	}

	for _, e := range scan.Entries {
		sig := fmt.Sprintf("%d,%d,%d,%d", e[2], e[3], e[4], e[5])
		k := fmt.Sprintf("%d:%d", e[0], e[1])
		if prev.Sent[k] == sig {
			continue
		}
		entries = append(entries, []int64{e[0], e[1], e[2], e[3], e[4], e[5]})
		nextSent[k] = sig
	}

	if len(entries) == 0 {
		return 0, nil
	}

	payload := map[string]any{
		"schema":       1,
		"installId":    installID,
		"appVersion":   s.app.version,
		"realmDisplay": mk.RealmDisplay,
		"scanTime":     scan.ScanTime,
		"fullScanDays": scan.FullScanDays,
		"entries":      entries,
	}

	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		return 0, err
	}
	if err := json.NewEncoder(gz).Encode(payload); err != nil {
		gz.Close()
		return 0, err
	}
	if err := gz.Close(); err != nil {
		return 0, err
	}

	url := fmt.Sprintf("%s/v1/markets/%s/uploads", baseURL, mk.Path())
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("User-Agent", "WoWToolsForever/"+s.app.version)

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, errors.New(t("Sync server cannot be reached ({reason})", "reason", err.Error()))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusTooManyRequests {
		// Cooldown active — not fatal; next poll will retry.
		return 0, nil
	}
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return 0, errors.New(t("Sync upload failed with HTTP {status}", "status", strconv.Itoa(resp.StatusCode)))
	}

	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err == nil {
		if raw, err := json.Marshal(localUploadState{Sent: nextSent}); err == nil {
			_ = os.WriteFile(statePath, raw, 0o644)
		}
	}
	return len(entries), nil
}

func (s *marketSyncer) pullMarket(baseURL, syncDir string, mk marketsync.MarketKey) (bool, *localPullMeta, *marketsync.SyncedData, error) {
	dir := filepath.Join(syncDir, mk.DirName())
	metaPath := filepath.Join(dir, "meta.json")
	meta := &localPullMeta{DayRevs: map[string]int{}}
	if raw, err := os.ReadFile(metaPath); err == nil {
		_ = json.Unmarshal(raw, meta)
		if meta.DayRevs == nil {
			meta.DayRevs = map[string]int{}
		}
	}

	manifestURL := fmt.Sprintf("%s/v1/markets/%s/manifest.json", baseURL, mk.Path())
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, manifestURL, nil)
	if err != nil {
		return false, meta, nil, err
	}
	req.Header.Set("User-Agent", "WoWToolsForever/"+s.app.version)
	if meta.ETag != "" {
		req.Header.Set("If-None-Match", meta.ETag)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return false, meta, nil, errors.New(t("Sync server cannot be reached ({reason})", "reason", err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return false, meta, nil, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		// Market has no published data yet (e.g. just uploaded for the first time).
		return false, meta, nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, meta, nil, errors.New(t("Sync server answered with HTTP {status}", "status", strconv.Itoa(resp.StatusCode)))
	}

	var man syncManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&man); err != nil {
		return false, meta, nil, err
	}
	newETag := resp.Header.Get("ETag")

	if err := os.MkdirAll(filepath.Join(dir, "days"), 0o755); err != nil {
		return false, meta, nil, err
	}

	changed := false
	snapFile := filepath.Join(dir, "snapshot.json.gz")
	if man.Snapshot != nil && man.Snapshot.Version > 0 && (meta.SnapshotVer != man.Snapshot.Version || !exists(snapFile)) {
		if err := s.downloadVerified(baseURL+man.Snapshot.URL, snapFile, man.Snapshot.SHA256); err != nil {
			return false, meta, nil, err
		}
		meta.SnapshotVer = man.Snapshot.Version
		changed = true
	}

	for _, d := range man.Days {
		ds := strconv.FormatInt(d.Day, 10)
		dayFile := filepath.Join(dir, "days", ds+".json.gz")
		if meta.DayRevs[ds] == d.Rev && exists(dayFile) {
			continue
		}
		if err := s.downloadVerified(baseURL+d.URL, dayFile, d.SHA256); err != nil {
			return false, meta, nil, err
		}
		meta.DayRevs[ds] = d.Rev
		changed = true
	}

	mergedPath := marketsync.MergedCachePath(syncDir, mk)
	if !changed && exists(mergedPath) {
		meta.ETag = newETag
		meta.UpdatedAt = man.UpdatedAt
		meta.FirstDay = man.FirstDay
		meta.LatestDay = man.LatestDay
		meta.TotalItems = man.TotalItems
		if raw, err := json.Marshal(meta); err == nil {
			_ = os.WriteFile(metaPath, raw, 0o644)
		}
		return false, meta, nil, nil
	}

	// Materialize snapshot + all day blobs into merged.json.gz for fast bridge reads.
	synced, err := materializeMarket(mk, man, snapFile, filepath.Join(dir, "days"))
	if err != nil {
		return false, meta, nil, err
	}
	if err := marketsync.WriteCache(mergedPath, synced); err != nil {
		return false, meta, nil, err
	}

	meta.ETag = newETag
	meta.UpdatedAt = man.UpdatedAt
	meta.FirstDay = man.FirstDay
	meta.LatestDay = man.LatestDay
	meta.TotalItems = len(synced.Items)
	if raw, err := json.Marshal(meta); err == nil {
		_ = os.WriteFile(metaPath, raw, 0o644)
	}
	return true, meta, synced, nil
}

func (s *marketSyncer) downloadVerified(url, dstPath, wantSHA string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "WoWToolsForever/"+s.app.version)
	resp, err := s.client.Do(req)
	if err != nil {
		return errors.New(t("Sync server cannot be reached ({reason})", "reason", err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(t("Sync server answered with HTTP {status}", "status", strconv.Itoa(resp.StatusCode)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if wantSHA != "" {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != wantSHA {
			return errors.New(t("Downloaded sync file does not match its checksum."))
		}
	}
	tmp := dstPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dstPath)
}

func materializeMarket(mk marketsync.MarketKey, man syncManifest, snapFile, daysDir string) (*marketsync.SyncedData, error) {
	// Per item -> day -> [day, low, high, offers, reporters]
	itemDays := map[string]map[int64][5]int64{}
	latest := map[string][2]int64{}

	if exists(snapFile) {
		var snap syncSnapshotBlob
		if err := readGzipJSON(snapFile, &snap); err != nil {
			return nil, err
		}
		for itemKey, rows := range snap.Items {
			m := map[int64][5]int64{}
			for _, r := range rows {
				m[r[0]] = r
			}
			itemDays[itemKey] = m
		}
		for itemKey, lp := range snap.Latest {
			latest[itemKey] = lp
		}
	}

	// Apply day blobs in ascending day order so newer revisions override snapshot rows.
	dayFiles, _ := os.ReadDir(daysDir)
	type df struct {
		day  int64
		path string
	}
	var ordered []df
	for _, e := range dayFiles {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json.gz") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".json.gz")
		if d, err := strconv.ParseInt(stem, 10, 64); err == nil {
			ordered = append(ordered, df{day: d, path: filepath.Join(daysDir, e.Name())})
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].day < ordered[j].day })

	for _, f := range ordered {
		var blob syncDayBlob
		if err := readGzipJSON(f.path, &blob); err != nil {
			continue
		}
		for _, r := range blob.Rows {
			if len(r) < 5 {
				continue
			}
			itemKey := strconv.FormatInt(r[0], 10)
			m := itemDays[itemKey]
			if m == nil {
				m = map[int64][5]int64{}
				itemDays[itemKey] = m
			}
			m[blob.Day] = [5]int64{blob.Day, r[1], r[2], r[3], r[4]}
			lp := r[1]
			if len(r) >= 6 && r[5] > 0 {
				lp = r[5]
			}
			prev, ok := latest[itemKey]
			if !ok || blob.Day >= prev[1] {
				latest[itemKey] = [2]int64{lp, blob.Day}
			}
		}
	}

	items := make(map[string][][5]int64, len(itemDays))
	var firstDay, latestDay int64
	for itemKey, byDay := range itemDays {
		rows := make([][5]int64, 0, len(byDay))
		for _, r := range byDay {
			rows = append(rows, r)
			if firstDay == 0 || r[0] < firstDay {
				firstDay = r[0]
			}
			if r[0] > latestDay {
				latestDay = r[0]
			}
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
		items[itemKey] = rows
	}

	return &marketsync.SyncedData{
		Schema:       1,
		Market:       mk.Path(),
		RealmDisplay: mk.RealmDisplay,
		Faction:      mk.FactionTitle(),
		UpdatedAt:    man.UpdatedAt,
		FirstDay:     firstDay,
		LatestDay:    latestDay,
		Latest:       latest,
		Items:        items,
	}, nil
}

func readGzipJSON(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return json.NewDecoder(gz).Decode(dst)
}
