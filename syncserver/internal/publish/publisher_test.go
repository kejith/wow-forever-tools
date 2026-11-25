package publish

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kejith/wow-tools-forever/syncserver/internal/aggregate"
	"github.com/kejith/wow-tools-forever/syncserver/internal/db"
)

type memStore struct {
	market  db.Market
	dirty   []int
	obs     map[int]map[int32][]aggregate.Observation
	revs    map[int]db.DayRevision
	daily   []db.SnapshotRow
	latest  []db.LatestPriceRow
	anoms   []string
}

func (m *memStore) ListMarkets(context.Context) ([]db.Market, error) { return []db.Market{m.market}, nil }
func (m *memStore) GetMarket(context.Context, string, string, string) (*db.Market, error) {
	cp := m.market
	return &cp, nil
}
func (m *memStore) DirtyDays(context.Context, int) ([]int, error) { return m.dirty, nil }
func (m *memStore) LoadDayObservations(_ context.Context, _ int, day int) (map[int32][]aggregate.Observation, map[int32]int64, error) {
	return m.obs[day], map[int32]int64{}, nil
}
func (m *memStore) NextDayRev(_ context.Context, _ int, day int) (int, error) {
	return m.revs[day].Rev + 1, nil
}
func (m *memStore) SaveDayConsensus(_ context.Context, _ int, day int, rows []db.DayRow, sha string, size int, anoms []string) (int, error) {
	rev := m.revs[day].Rev + 1
	m.revs[day] = db.DayRevision{Day: day, Rev: rev, ItemCount: len(rows), SHA256: sha, ByteSize: size, UpdatedAt: time.Now().UTC()}
	if day > m.market.LatestDay {
		m.market.LatestDay = day
	}
	for _, r := range rows {
		m.daily = append(m.daily, db.SnapshotRow{ItemID: r.ItemID, Day: int32(day), Low: r.Low, High: r.High, Offers: r.Offers, Reporters: r.Reporters})
		price := r.LatestPrice
		if price == 0 {
			price = r.Low
		}
		m.latest = append(m.latest, db.LatestPriceRow{ItemID: r.ItemID, Price: price, Day: int32(day)})
	}
	m.anoms = append(m.anoms, anoms...)
	return rev, nil
}
func (m *memStore) LoadDayRevisions(context.Context, int) ([]db.DayRevision, error) {
	var out []db.DayRevision
	for _, r := range m.revs {
		out = append(out, r)
	}
	return out, nil
}
func (m *memStore) MarketStats(context.Context, int) (int, int, error) { return 2519, len(m.latest), nil }
func (m *memStore) LoadSnapshotData(context.Context, int) ([]db.SnapshotRow, []db.LatestPriceRow, error) {
	return m.daily, m.latest, nil
}
func (m *memStore) SaveSnapshotMeta(_ context.Context, _ int, ver, maxDay int, sha string, size int) error {
	m.market.SnapshotVer = ver
	m.market.SnapshotDay = maxDay
	m.market.SnapshotAt = time.Now().UTC()
	m.market.SnapshotSHA = sha
	m.market.SnapshotSize = size
	return nil
}

func TestPublisherWritesDaySnapshotAndManifest(t *testing.T) {
	dir := t.TempDir()
	st := &memStore{
		market: db.Market{ID: 1, Region: "eu", Realm: "pvp", Faction: "horde", DisplayRealm: "PvP"},
		dirty:  []int{2519},
		revs:   map[int]db.DayRevision{},
		obs: map[int]map[int32][]aggregate.Observation{
			2519: {
				2770: {
					{InstallID: "u1", Low: 195, High: 220, Offers: 4546, LastPrice: 195, IsFullScan: true},
					{InstallID: "u2", Low: 190, High: 230, Offers: 4600, LastPrice: 200, IsFullScan: true},
				},
			},
		},
	}

	pub := New(st, dir)
	if err := pub.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// Verify manifest.json
	manBytes, err := os.ReadFile(filepath.Join(dir, "eu", "pvp", "horde", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var man Manifest
	if err := json.Unmarshal(manBytes, &man); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if man.Market != "eu/pvp/horde" || man.LatestDay != 2519 || man.Snapshot == nil || man.Snapshot.Version != 1 {
		t.Fatalf("unexpected manifest: %+v", man)
	}

	// Verify day blob decodes cleanly
	dayGz, err := os.ReadFile(filepath.Join(dir, "eu", "pvp", "horde", "days", "2519", "1.json.gz"))
	if err != nil {
		t.Fatalf("read day blob: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(dayGz))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	var blob DayBlob
	if err := json.NewDecoder(gz).Decode(&blob); err != nil {
		t.Fatalf("decode day blob: %v", err)
	}
	if len(blob.Rows) != 1 || blob.Rows[0][0] != 2770 || blob.Rows[0][1] != 190 || blob.Rows[0][2] != 230 {
		t.Fatalf("unexpected day rows: %+v", blob.Rows)
	}
}
