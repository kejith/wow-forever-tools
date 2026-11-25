// Package publish materializes consolidated daily_prices into immutable gzipped JSON blobs
// and manifest.json files suitable for Cloudflare Edge caching.
package publish

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/kejith/wow-tools-forever/syncserver/internal/aggregate"
	"github.com/kejith/wow-tools-forever/syncserver/internal/db"
)

// Store is the persistence subset needed by Publisher.
type Store interface {
	ListMarkets(ctx context.Context) ([]db.Market, error)
	GetMarket(ctx context.Context, region, realm, faction string) (*db.Market, error)
	DirtyDays(ctx context.Context, marketID int) ([]int, error)
	LoadDayObservations(ctx context.Context, marketID, day int) (map[int32][]aggregate.Observation, map[int32]int64, error)
	NextDayRev(ctx context.Context, marketID, day int) (int, error)
	SaveDayConsensus(ctx context.Context, marketID, day int, rows []db.DayRow, sha string, byteSize int, anomalousInstalls []string) (int, error)
	LoadDayRevisions(ctx context.Context, marketID int) ([]db.DayRevision, error)
	MarketStats(ctx context.Context, marketID int) (firstDay int, totalItems int, err error)
	LoadSnapshotData(ctx context.Context, marketID int) ([]db.SnapshotRow, []db.LatestPriceRow, error)
	SaveSnapshotMeta(ctx context.Context, marketID, ver, maxDay int, sha string, size int) error
}

// Manifest is served at /v1/markets/{region}/{realm}/{faction}/manifest.json.
type Manifest struct {
	Schema       int           `json:"schema"`
	Market       string        `json:"market"`
	RealmDisplay string        `json:"realmDisplay"`
	Faction      string        `json:"faction"`
	Day0         string        `json:"day0"`
	UpdatedAt    int64         `json:"updatedAt"`
	FirstDay     int           `json:"firstDay"`
	LatestDay    int           `json:"latestDay"`
	TotalItems   int           `json:"totalItems"`
	Snapshot     *SnapshotRef  `json:"snapshot,omitempty"`
	Days         []ManifestDay `json:"days"`
}

// SnapshotRef points to the latest full historical snapshot.
type SnapshotRef struct {
	Version int    `json:"version"`
	MaxDay  int    `json:"maxDay"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Bytes   int    `json:"bytes"`
}

// ManifestDay points to an immutable day revision blob.
type ManifestDay struct {
	Day       int    `json:"day"`
	Rev       int    `json:"rev"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Bytes     int    `json:"bytes"`
	Items     int    `json:"items"`
	UpdatedAt int64  `json:"updatedAt"`
}

// DayBlob is serialized as days/{day}/{rev}.json.gz.
// Rows are compact tuples: [itemId, low, high, offers, reporters, latestPrice].
type DayBlob struct {
	Schema    int       `json:"schema"`
	Market    string    `json:"market"`
	Day       int       `json:"day"`
	Rev       int       `json:"rev"`
	UpdatedAt int64     `json:"updatedAt"`
	Rows      [][]int64 `json:"rows"`
}

// SnapshotBlob is serialized as snapshots/{ver}.json.gz.
// Items maps itemId -> [[day, low, high, offers, reporters], ...].
// Latest maps itemId -> [price, day].
type SnapshotBlob struct {
	Schema    int                   `json:"schema"`
	Market    string                `json:"market"`
	Version   int                   `json:"version"`
	MaxDay    int                   `json:"maxDay"`
	UpdatedAt int64                 `json:"updatedAt"`
	Latest    map[string][2]int64   `json:"latest"`
	Items     map[string][][5]int64 `json:"items"`
}

// Publisher aggregates dirty days and writes immutable gzipped blobs to BlobDir.
type Publisher struct {
	store           Store
	blobDir         string
	snapshotMaxDays int // rebuild snapshot when un-snapshotted days exceed this
	snapshotEvery   time.Duration
	lastSnapshot    map[int]time.Time
}

// New creates a Publisher writing into blobDir.
func New(store Store, blobDir string) *Publisher {
	return &Publisher{
		store:           store,
		blobDir:         blobDir,
		snapshotMaxDays: 14,
		snapshotEvery:   6 * time.Hour,
		lastSnapshot:    map[int]time.Time{},
	}
}

// MarketDir returns the local filesystem directory for a market's blobs.
func (p *Publisher) MarketDir(region, realm, faction string) string {
	return filepath.Join(p.blobDir, region, realm, faction)
}

// RunOnce processes all dirty days across all markets and updates manifests & snapshots.
func (p *Publisher) RunOnce(ctx context.Context) error {
	markets, err := p.store.ListMarkets(ctx)
	if err != nil {
		return err
	}
	for _, m := range markets {
		if err := p.processMarket(ctx, m); err != nil {
			log.Printf("publish market %s: %v", m.Path(), err)
		}
	}
	return nil
}

func (p *Publisher) processMarket(ctx context.Context, m db.Market) error {
	dirty, err := p.store.DirtyDays(ctx, m.ID)
	if err != nil {
		return err
	}
	if len(dirty) == 0 && p.manifestExists(m) {
		return nil
	}

	now := time.Now().UTC()
	for _, day := range dirty {
		if err := p.publishDay(ctx, m, day, now); err != nil {
			return fmt.Errorf("day %d: %w", day, err)
		}
	}

	// Refresh market metadata after dirty days updated latest_day.
	fresh, err := p.store.GetMarket(ctx, m.Region, m.Realm, m.Faction)
	if err != nil || fresh == nil {
		return err
	}
	revs, err := p.store.LoadDayRevisions(ctx, fresh.ID)
	if err != nil {
		return err
	}

	// Decide whether to build/rebuild full snapshot.
	deltaDays := 0
	for _, r := range revs {
		if r.Day > fresh.SnapshotDay || r.UpdatedAt.After(fresh.SnapshotAt) {
			deltaDays++
		}
	}
	needSnapshot := len(revs) > 0 && (fresh.SnapshotVer == 0 ||
		deltaDays >= p.snapshotMaxDays ||
		(len(dirty) > 0 && time.Since(fresh.SnapshotAt) >= p.snapshotEvery))
	if needSnapshot {
		if err := p.publishSnapshot(ctx, *fresh, now); err != nil {
			return fmt.Errorf("snapshot: %w", err)
		}
		p.lastSnapshot[fresh.ID] = now
		fresh, err = p.store.GetMarket(ctx, m.Region, m.Realm, m.Faction)
		if err != nil || fresh == nil {
			return err
		}
	}

	return p.writeManifest(ctx, *fresh, revs, now)
}

func (p *Publisher) publishDay(ctx context.Context, m db.Market, day int, now time.Time) error {
	byItem, medians, err := p.store.LoadDayObservations(ctx, m.ID, day)
	if err != nil {
		return err
	}

	// Track per-install outlier rates across multi-reporter items in this day.
	compared := map[string]int{}
	outliers := map[string]int{}

	itemIDs := make([]int32, 0, len(byItem))
	for id := range byItem {
		itemIDs = append(itemIDs, id)
	}
	sort.Slice(itemIDs, func(i, j int) bool { return itemIDs[i] < itemIDs[j] })

	dayRows := make([]db.DayRow, 0, len(itemIDs))
	blobRows := make([][]int64, 0, len(itemIDs))

	for _, id := range itemIDs {
		obs := byItem[id]
		cons, ok := aggregate.Compute(obs, medians[id])
		if len(obs) >= 3 {
			for _, o := range obs {
				compared[o.InstallID]++
			}
		}
		for _, bad := range cons.Outliers {
			outliers[bad]++
		}
		if !ok {
			continue
		}
		dayRows = append(dayRows, db.DayRow{
			ItemID:        id,
			Low:           cons.Low,
			High:          cons.High,
			Offers:        cons.Offers,
			Reporters:     cons.Reporters,
			FullReporters: cons.FullReporters,
			LatestPrice:   cons.LatestPrice,
		})
		blobRows = append(blobRows, []int64{
			int64(id), cons.Low, cons.High, int64(cons.Offers), int64(cons.Reporters), cons.LatestPrice,
		})
	}

	var anomalous []string
	for inst, n := range compared {
		if aggregate.IsAnomalousBatch(n, outliers[inst]) {
			anomalous = append(anomalous, inst)
		}
	}

	nextRev, err := p.store.NextDayRev(ctx, m.ID, day)
	if err != nil {
		return err
	}

	blob := DayBlob{
		Schema:    1,
		Market:    m.Path(),
		Day:       day,
		Rev:       nextRev,
		UpdatedAt: now.Unix(),
		Rows:      blobRows,
	}
	gz, sha, err := encodeGzipJSON(blob)
	if err != nil {
		return err
	}

	target := filepath.Join(p.MarketDir(m.Region, m.Realm, m.Faction), "days", strconv.Itoa(day), fmt.Sprintf("%d.json.gz", nextRev))
	if err := writeAtomic(target, gz); err != nil {
		return err
	}

	savedRev, err := p.store.SaveDayConsensus(ctx, m.ID, day, dayRows, sha, len(gz), anomalous)
	if err != nil {
		return err
	}
	if savedRev != nextRev {
		// Rare race guard: ensure filename matches committed rev.
		alt := filepath.Join(p.MarketDir(m.Region, m.Realm, m.Faction), "days", strconv.Itoa(day), fmt.Sprintf("%d.json.gz", savedRev))
		_ = writeAtomic(alt, gz)
	}
	return nil
}

func (p *Publisher) publishSnapshot(ctx context.Context, m db.Market, now time.Time) error {
	daily, latest, err := p.store.LoadSnapshotData(ctx, m.ID)
	if err != nil {
		return err
	}
	ver := m.SnapshotVer + 1
	maxDay := 0
	itemsMap := make(map[string][][5]int64, len(latest))
	for _, r := range daily {
		if int(r.Day) > maxDay {
			maxDay = int(r.Day)
		}
		k := strconv.Itoa(int(r.ItemID))
		itemsMap[k] = append(itemsMap[k], [5]int64{int64(r.Day), r.Low, r.High, int64(r.Offers), int64(r.Reporters)})
	}
	latestMap := make(map[string][2]int64, len(latest))
	for _, r := range latest {
		latestMap[strconv.Itoa(int(r.ItemID))] = [2]int64{r.Price, int64(r.Day)}
	}

	blob := SnapshotBlob{
		Schema:    1,
		Market:    m.Path(),
		Version:   ver,
		MaxDay:    maxDay,
		UpdatedAt: now.Unix(),
		Latest:    latestMap,
		Items:     itemsMap,
	}
	gz, sha, err := encodeGzipJSON(blob)
	if err != nil {
		return err
	}
	target := filepath.Join(p.MarketDir(m.Region, m.Realm, m.Faction), "snapshots", fmt.Sprintf("%d.json.gz", ver))
	if err := writeAtomic(target, gz); err != nil {
		return err
	}
	return p.store.SaveSnapshotMeta(ctx, m.ID, ver, maxDay, sha, len(gz))
}

func (p *Publisher) writeManifest(ctx context.Context, m db.Market, revs []db.DayRevision, now time.Time) error {
	firstDay, totalItems, err := p.store.MarketStats(ctx, m.ID)
	if err != nil {
		return err
	}
	factionDisplay := "Horde"
	if m.Faction == "alliance" {
		factionDisplay = "Alliance"
	}
	man := Manifest{
		Schema:       1,
		Market:       m.Path(),
		RealmDisplay: m.DisplayRealm,
		Faction:      factionDisplay,
		Day0:         "2020-01-01",
		UpdatedAt:    now.Unix(),
		FirstDay:     firstDay,
		LatestDay:    m.LatestDay,
		TotalItems:   totalItems,
		Days:         make([]ManifestDay, 0, len(revs)),
	}
	if m.SnapshotVer > 0 {
		man.Snapshot = &SnapshotRef{
			Version: m.SnapshotVer,
			MaxDay:  m.SnapshotDay,
			URL:     fmt.Sprintf("/v1/markets/%s/snapshots/%d.json.gz", m.Path(), m.SnapshotVer),
			SHA256:  m.SnapshotSHA,
			Bytes:   m.SnapshotSize,
		}
	}
	for _, r := range revs {
		// Include all days after snapshot OR days updated after snapshot was built (historical backfills).
		if m.SnapshotVer == 0 || r.Day > m.SnapshotDay || r.UpdatedAt.After(m.SnapshotAt) {
			man.Days = append(man.Days, ManifestDay{
				Day:       r.Day,
				Rev:       r.Rev,
				URL:       fmt.Sprintf("/v1/markets/%s/days/%d/%d.json.gz", m.Path(), r.Day, r.Rev),
				SHA256:    r.SHA256,
				Bytes:     r.ByteSize,
				Items:     r.ItemCount,
				UpdatedAt: r.UpdatedAt.Unix(),
			})
		}
	}
	raw, err := json.Marshal(man)
	if err != nil {
		return err
	}
	target := filepath.Join(p.MarketDir(m.Region, m.Realm, m.Faction), "manifest.json")
	return writeAtomic(target, raw)
}

func (p *Publisher) manifestExists(m db.Market) bool {
	_, err := os.Stat(filepath.Join(p.MarketDir(m.Region, m.Realm, m.Faction), "manifest.json"))
	return err == nil
}

// CleanupOldBlobs removes superseded day/snapshot blob files older than maxAge.
func (p *Publisher) CleanupOldBlobs(ctx context.Context, maxAge time.Duration) error {
	markets, err := p.store.ListMarkets(ctx)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	for _, m := range markets {
		revs, err := p.store.LoadDayRevisions(ctx, m.ID)
		if err != nil {
			continue
		}
		keepDay := map[int]string{}
		for _, r := range revs {
			keepDay[r.Day] = fmt.Sprintf("%d.json.gz", r.Rev)
		}
		base := p.MarketDir(m.Region, m.Realm, m.Faction)
		// Prune old day revisions
		for day, keepFile := range keepDay {
			dir := filepath.Join(base, "days", strconv.Itoa(day))
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if e.Name() == keepFile {
					continue
				}
				if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
					_ = os.Remove(filepath.Join(dir, e.Name()))
				}
			}
		}
		// Prune old snapshots (keep current and previous)
		snapDir := filepath.Join(base, "snapshots")
		entries, _ := os.ReadDir(snapDir)
		keepA := fmt.Sprintf("%d.json.gz", m.SnapshotVer)
		keepB := fmt.Sprintf("%d.json.gz", m.SnapshotVer-1)
		for _, e := range entries {
			if e.Name() == keepA || e.Name() == keepB {
				continue
			}
			if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(snapDir, e.Name()))
			}
		}
	}
	return nil
}

func encodeGzipJSON(v any) ([]byte, string, error) {
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, "", err
	}
	enc := json.NewEncoder(gz)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		_ = gz.Close()
		return nil, "", err
	}
	if err := gz.Close(); err != nil {
		return nil, "", err
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
