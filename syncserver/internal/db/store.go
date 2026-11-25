// Package db handles PostgreSQL persistence for the AH community sync server.
package db

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kejith/wow-tools-forever/syncserver/internal/aggregate"
)

//go:embed migrations/001_init.sql
var initSQL string

// Market identifies a single Auction House market (region + realm + faction).
type Market struct {
	ID           int
	Region       string
	Realm        string
	Faction      string
	DisplayRealm string
	LatestDay    int
	SnapshotVer  int
	SnapshotDay  int
	SnapshotAt   time.Time
	SnapshotSHA  string
	SnapshotSize int
	UpdatedAt    time.Time
}

// Path returns the canonical URL slug, e.g. "eu/pvp/horde".
func (m Market) Path() string { return m.Region + "/" + m.Realm + "/" + m.Faction }

// UploadEntry is one validated item-day tuple from a client upload.
type UploadEntry struct {
	ItemID     int32
	Day        int32
	Low        int64
	High       int64
	Offers     int32
	LastPrice  int64
	IsFullScan bool
}

// UploadInput is a validated upload batch.
type UploadInput struct {
	Region       string
	Realm        string
	Faction      string
	DisplayRealm string
	InstallID    string
	AppVersion   string
	IPHash       []byte
	ScanTime     time.Time
	PayloadSHA   string
	IsFullScan   bool
	Entries      []UploadEntry
}

// UploadResult summarizes what RecordUpload did.
type UploadResult struct {
	MarketID    int
	Accepted    int
	DaysTouched []int
	Deduped     bool
	Quarantined bool
}

// ErrRateLimited is returned when an install or IP exceeds upload rate limits.
var ErrRateLimited = errors.New("rate limited")

// DayRow is one consolidated item entry in a day blob.
type DayRow struct {
	ItemID        int32
	Low           int64
	High          int64
	Offers        int32
	Reporters     int16
	FullReporters int16
	LatestPrice   int64
}

// DayRevision describes a published day blob.
type DayRevision struct {
	Day       int
	Rev       int
	Dirty     bool
	ItemCount int
	SHA256    string
	ByteSize  int
	UpdatedAt time.Time
}

// SnapshotRow is one consolidated historical (item, day) row.
type SnapshotRow struct {
	ItemID    int32
	Day       int32
	Low       int64
	High      int64
	Offers    int32
	Reporters int16
}

// LatestPriceRow is the newest known buyout for an item.
type LatestPriceRow struct {
	ItemID int32
	Price  int64
	Day    int32
}

// Store wraps a PostgreSQL connection pool.
type Store struct {
	pool             *pgxpool.Pool
	installCooldown  time.Duration
	ipHourlyLimit    int
}

// Open connects to PostgreSQL and applies the embedded schema migration.
func Open(ctx context.Context, dsn string, installCooldown time.Duration, ipHourlyLimit int) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 16
	cfg.MinConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if _, err := pool.Exec(ctx, initSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return &Store{
		pool:            pool,
		installCooldown: installCooldown,
		ipHourlyLimit:   ipHourlyLimit,
	}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// RecordUpload validates rate limits, deduplicates identical payloads, stores raw observations,
// and marks touched days dirty for asynchronous consensus publication.
func (s *Store) RecordUpload(ctx context.Context, in UploadInput) (*UploadResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Upsert market
	var marketID int
	err = tx.QueryRow(ctx, `
		INSERT INTO markets (region, realm, faction, display_realm)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (region, realm, faction)
		DO UPDATE SET display_realm = CASE WHEN markets.display_realm = '' THEN EXCLUDED.display_realm ELSE markets.display_realm END
		RETURNING id
	`, in.Region, in.Realm, in.Faction, in.DisplayRealm).Scan(&marketID)
	if err != nil {
		return nil, fmt.Errorf("upsert market: %w", err)
	}

	// Upsert install and read quarantine state
	var quarantined bool
	err = tx.QueryRow(ctx, `
		INSERT INTO installs (id, app_version, last_seen)
		VALUES ($1, $2, now())
		ON CONFLICT (id) DO UPDATE SET last_seen = now(), app_version = EXCLUDED.app_version
		RETURNING quarantined
	`, in.InstallID, in.AppVersion).Scan(&quarantined)
	if err != nil {
		return nil, fmt.Errorf("upsert install: %w", err)
	}

	// Deduplicate identical consecutive upload from same install/market within 24h
	var lastSHA string
	var lastTime time.Time
	err = tx.QueryRow(ctx, `
		SELECT payload_sha256, created_at FROM uploads
		WHERE install_id = $1 AND market_id = $2
		ORDER BY created_at DESC LIMIT 1
	`, in.InstallID, marketID).Scan(&lastSHA, &lastTime)
	if err == nil {
		if lastSHA == in.PayloadSHA && time.Since(lastTime) < 24*time.Hour {
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &UploadResult{MarketID: marketID, Deduped: true, Quarantined: quarantined}, nil
		}
		if s.installCooldown > 0 && time.Since(lastTime) < s.installCooldown {
			return nil, ErrRateLimited
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// Per-IP hourly upload cap
	if s.ipHourlyLimit > 0 && len(in.IPHash) > 0 {
		var ipCount int
		err = tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM uploads
			WHERE ip_hash = $1 AND created_at > now() - interval '1 hour'
		`, in.IPHash).Scan(&ipCount)
		if err != nil {
			return nil, err
		}
		if ipCount >= s.ipHourlyLimit {
			return nil, ErrRateLimited
		}
	}

	// Record upload audit row
	_, err = tx.Exec(ctx, `
		INSERT INTO uploads (market_id, install_id, ip_hash, client_scan_ts, item_day_count, is_full_scan, payload_sha256)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, marketID, in.InstallID, in.IPHash, in.ScanTime, len(in.Entries), in.IsFullScan, in.PayloadSHA)
	if err != nil {
		return nil, fmt.Errorf("insert upload: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE installs SET upload_count = upload_count + 1 WHERE id = $1`, in.InstallID)
	if err != nil {
		return nil, err
	}

	// If install is quarantined, acknowledge silently (shadow-ignore) without polluting observations.
	if quarantined || len(in.Entries) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &UploadResult{MarketID: marketID, Accepted: len(in.Entries), Quarantined: quarantined}, nil
	}

	// Bulk upsert observations using PostgreSQL UNNEST in chunks of 10,000.
	daySet := map[int]struct{}{}
	const chunk = 10000
	for start := 0; start < len(in.Entries); start += chunk {
		end := start + chunk
		if end > len(in.Entries) {
			end = len(in.Entries)
		}
		batch := in.Entries[start:end]
		days := make([]int32, len(batch))
		items := make([]int32, len(batch))
		lows := make([]int64, len(batch))
		highs := make([]int64, len(batch))
		offers := make([]int32, len(batch))
		lasts := make([]*int64, len(batch))
		fulls := make([]bool, len(batch))
		for i, e := range batch {
			days[i] = e.Day
			items[i] = e.ItemID
			lows[i] = e.Low
			highs[i] = e.High
			offers[i] = e.Offers
			if e.LastPrice > 0 {
				v := e.LastPrice
				lasts[i] = &v
			}
			fulls[i] = e.IsFullScan
			daySet[int(e.Day)] = struct{}{}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO observations (market_id, day, item_id, install_id, low, high, offers, last_price, is_full_scan, observed_at)
			SELECT $1, u.day, u.item_id, $2::uuid, u.low, u.high, u.offers, u.last_price, u.is_full_scan, now()
			FROM UNNEST($3::int[], $4::int[], $5::bigint[], $6::bigint[], $7::int[], $8::bigint[], $9::bool[])
				AS u(day, item_id, low, high, offers, last_price, is_full_scan)
			ON CONFLICT (market_id, day, item_id, install_id) DO UPDATE SET
				low = LEAST(observations.low, EXCLUDED.low),
				high = GREATEST(observations.high, EXCLUDED.high),
				offers = GREATEST(observations.offers, EXCLUDED.offers),
				last_price = COALESCE(EXCLUDED.last_price, observations.last_price),
				is_full_scan = observations.is_full_scan OR EXCLUDED.is_full_scan,
				observed_at = now()
		`, marketID, in.InstallID, days, items, lows, highs, offers, lasts, fulls)
		if err != nil {
			return nil, fmt.Errorf("upsert observations: %w", err)
		}
	}

	// Mark touched days dirty
	daysTouched := make([]int, 0, len(daySet))
	days32 := make([]int32, 0, len(daySet))
	for d := range daySet {
		daysTouched = append(daysTouched, d)
		days32 = append(days32, int32(d))
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO day_revisions (market_id, day, rev, dirty, updated_at)
		SELECT $1, d, 0, TRUE, now() FROM UNNEST($2::int[]) AS d
		ON CONFLICT (market_id, day) DO UPDATE SET dirty = TRUE, updated_at = now()
	`, marketID, days32)
	if err != nil {
		return nil, fmt.Errorf("mark dirty days: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &UploadResult{
		MarketID:    marketID,
		Accepted:    len(in.Entries),
		DaysTouched: daysTouched,
	}, nil
}

// GetMarket looks up a market by (region, realm, faction).
func (s *Store) GetMarket(ctx context.Context, region, realm, faction string) (*Market, error) {
	var m Market
	err := s.pool.QueryRow(ctx, `
		SELECT id, region, realm, faction, display_realm, latest_day, snapshot_ver, snapshot_day, snapshot_at, snapshot_sha, snapshot_size, updated_at
		FROM markets WHERE region = $1 AND realm = $2 AND faction = $3
	`, region, realm, faction).Scan(
		&m.ID, &m.Region, &m.Realm, &m.Faction, &m.DisplayRealm,
		&m.LatestDay, &m.SnapshotVer, &m.SnapshotDay, &m.SnapshotAt, &m.SnapshotSHA, &m.SnapshotSize, &m.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &m, err
}

// ListMarkets returns all markets.
func (s *Store) ListMarkets(ctx context.Context) ([]Market, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, region, realm, faction, display_realm, latest_day, snapshot_ver, snapshot_day, snapshot_at, snapshot_sha, snapshot_size, updated_at
		FROM markets ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Market
	for rows.Next() {
		var m Market
		if err := rows.Scan(
			&m.ID, &m.Region, &m.Realm, &m.Faction, &m.DisplayRealm,
			&m.LatestDay, &m.SnapshotVer, &m.SnapshotDay, &m.SnapshotAt, &m.SnapshotSHA, &m.SnapshotSize, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DirtyDays returns dirty day numbers for a market, ascending.
func (s *Store) DirtyDays(ctx context.Context, marketID int) ([]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT day FROM day_revisions WHERE market_id = $1 AND dirty = TRUE ORDER BY day ASC
	`, marketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var days []int
	for rows.Next() {
		var d int
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		days = append(days, d)
	}
	return days, rows.Err()
}

// LoadDayObservations loads non-quarantined observations grouped by item_id, plus trailing 14-day multi-reporter medians.
func (s *Store) LoadDayObservations(ctx context.Context, marketID, day int) (map[int32][]aggregate.Observation, map[int32]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT o.item_id, o.install_id::text, o.low, o.high, o.offers, COALESCE(o.last_price, 0), o.is_full_scan
		FROM observations o
		JOIN installs i ON i.id = o.install_id
		WHERE o.market_id = $1 AND o.day = $2 AND i.quarantined = FALSE
	`, marketID, day)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	byItem := map[int32][]aggregate.Observation{}
	for rows.Next() {
		var itemID int32
		var o aggregate.Observation
		if err := rows.Scan(&itemID, &o.InstallID, &o.Low, &o.High, &o.Offers, &o.LastPrice, &o.IsFullScan); err != nil {
			return nil, nil, err
		}
		byItem[itemID] = append(byItem[itemID], o)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// Trailing 14-day consensus median per item (only days with >=2 reporters so single-reporter noise doesn't anchor it).
	medRows, err := s.pool.Query(ctx, `
		SELECT item_id, percentile_disc(0.5) WITHIN GROUP (ORDER BY low)::bigint
		FROM daily_prices
		WHERE market_id = $1 AND day BETWEEN $2 AND $3 AND reporters >= 2
		GROUP BY item_id
	`, marketID, day-14, day-1)
	if err != nil {
		return nil, nil, err
	}
	defer medRows.Close()

	medians := map[int32]int64{}
	for medRows.Next() {
		var itemID int32
		var med int64
		if err := medRows.Scan(&itemID, &med); err != nil {
			return nil, nil, err
		}
		medians[itemID] = med
	}
	return byItem, medians, medRows.Err()
}

// SaveDayConsensus atomically writes consolidated daily_prices, latest_prices, bumps day_revisions,
// and updates install anomaly/quarantine counters. Returns the new revision number.
func (s *Store) SaveDayConsensus(ctx context.Context, marketID, day int, rows []DayRow, sha string, byteSize int, anomalousInstalls []string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if len(rows) > 0 {
		const chunk = 10000
		for start := 0; start < len(rows); start += chunk {
			end := start + chunk
			if end > len(rows) {
				end = len(rows)
			}
			batch := rows[start:end]
			items := make([]int32, len(batch))
			lows := make([]int64, len(batch))
			highs := make([]int64, len(batch))
			offers := make([]int32, len(batch))
			reps := make([]int16, len(batch))
			fulls := make([]int16, len(batch))

			var latItems []int32
			var latPrices []int64
			var latReps []int16

			for i, r := range batch {
				items[i] = r.ItemID
				lows[i] = r.Low
				highs[i] = r.High
				offers[i] = r.Offers
				reps[i] = r.Reporters
				fulls[i] = r.FullReporters
				price := r.LatestPrice
				if price <= 0 {
					price = r.Low
				}
				latItems = append(latItems, r.ItemID)
				latPrices = append(latPrices, price)
				latReps = append(latReps, r.Reporters)
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO daily_prices (market_id, day, item_id, low, high, offers, reporters, full_reporters, updated_at)
				SELECT $1, $2, u.item_id, u.low, u.high, u.offers, u.reporters, u.full_reporters, now()
				FROM UNNEST($3::int[], $4::bigint[], $5::bigint[], $6::int[], $7::smallint[], $8::smallint[])
					AS u(item_id, low, high, offers, reporters, full_reporters)
				ON CONFLICT (market_id, day, item_id) DO UPDATE SET
					low = EXCLUDED.low,
					high = EXCLUDED.high,
					offers = EXCLUDED.offers,
					reporters = EXCLUDED.reporters,
					full_reporters = EXCLUDED.full_reporters,
					updated_at = now()
			`, marketID, day, items, lows, highs, offers, reps, fulls)
			if err != nil {
				return 0, fmt.Errorf("upsert daily_prices: %w", err)
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO latest_prices (market_id, item_id, price, day, reporters, updated_at)
				SELECT $1, u.item_id, u.price, $2, u.reporters, now()
				FROM UNNEST($3::int[], $4::bigint[], $5::smallint[]) AS u(item_id, price, reporters)
				ON CONFLICT (market_id, item_id) DO UPDATE SET
					price = CASE WHEN EXCLUDED.day >= latest_prices.day THEN EXCLUDED.price ELSE latest_prices.price END,
					day = GREATEST(latest_prices.day, EXCLUDED.day),
					reporters = CASE WHEN EXCLUDED.day >= latest_prices.day THEN EXCLUDED.reporters ELSE latest_prices.reporters END,
					updated_at = CASE WHEN EXCLUDED.day >= latest_prices.day THEN now() ELSE latest_prices.updated_at END
			`, marketID, day, latItems, latPrices, latReps)
			if err != nil {
				return 0, fmt.Errorf("upsert latest_prices: %w", err)
			}
		}
	}

	var newRev int
	err = tx.QueryRow(ctx, `
		INSERT INTO day_revisions (market_id, day, rev, dirty, item_count, sha256, byte_size, updated_at)
		VALUES ($1, $2, 1, FALSE, $3, $4, $5, now())
		ON CONFLICT (market_id, day) DO UPDATE SET
			rev = day_revisions.rev + 1,
			dirty = FALSE,
			item_count = EXCLUDED.item_count,
			sha256 = EXCLUDED.sha256,
			byte_size = EXCLUDED.byte_size,
			updated_at = now()
		RETURNING rev
	`, marketID, day, len(rows), sha, byteSize).Scan(&newRev)
	if err != nil {
		return 0, fmt.Errorf("bump day_revisions: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE markets SET latest_day = GREATEST(latest_day, $2), updated_at = now()
		WHERE id = $1
	`, marketID, day)
	if err != nil {
		return 0, err
	}

	for _, id := range anomalousInstalls {
		_, err = tx.Exec(ctx, `
			UPDATE installs
			SET anomaly_runs = anomaly_runs + 1,
				quarantined = (anomaly_runs + 1 >= $2)
			WHERE id = $1::uuid
		`, id, aggregate.QuarantineRuns)
		if err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return newRev, nil
}

// NextDayRev peeks the revision number that the next publish for (marketID, day) will assign.
func (s *Store) NextDayRev(ctx context.Context, marketID, day int) (int, error) {
	var rev int
	err := s.pool.QueryRow(ctx, `SELECT rev FROM day_revisions WHERE market_id = $1 AND day = $2`, marketID, day).Scan(&rev)
	if errors.Is(err, pgx.ErrNoRows) {
		return 1, nil
	}
	return rev + 1, err
}

// LoadDayRevisions returns all published day revisions for a market, ordered by day ascending.
func (s *Store) LoadDayRevisions(ctx context.Context, marketID int) ([]DayRevision, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT day, rev, dirty, item_count, sha256, byte_size, updated_at
		FROM day_revisions
		WHERE market_id = $1 AND rev > 0
		ORDER BY day ASC
	`, marketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DayRevision
	for rows.Next() {
		var r DayRevision
		if err := rows.Scan(&r.Day, &r.Rev, &r.Dirty, &r.ItemCount, &r.SHA256, &r.ByteSize, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarketStats returns (firstDay, totalItems) from daily_prices/latest_prices.
func (s *Store) MarketStats(ctx context.Context, marketID int) (firstDay int, totalItems int, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(MIN(day), 0) FROM day_revisions WHERE market_id = $1 AND rev > 0 AND item_count > 0
	`, marketID).Scan(&firstDay)
	if err != nil {
		return 0, 0, err
	}
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM latest_prices WHERE market_id = $1
	`, marketID).Scan(&totalItems)
	return firstDay, totalItems, err
}

// LoadSnapshotData loads all consolidated daily prices and latest prices for a market.
func (s *Store) LoadSnapshotData(ctx context.Context, marketID int) ([]SnapshotRow, []LatestPriceRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT item_id, day, low, high, offers, reporters
		FROM daily_prices
		WHERE market_id = $1
		ORDER BY item_id ASC, day ASC
	`, marketID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var daily []SnapshotRow
	for rows.Next() {
		var r SnapshotRow
		if err := rows.Scan(&r.ItemID, &r.Day, &r.Low, &r.High, &r.Offers, &r.Reporters); err != nil {
			return nil, nil, err
		}
		daily = append(daily, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	lrows, err := s.pool.Query(ctx, `
		SELECT item_id, price, day FROM latest_prices WHERE market_id = $1 ORDER BY item_id ASC
	`, marketID)
	if err != nil {
		return nil, nil, err
	}
	defer lrows.Close()
	var latest []LatestPriceRow
	for lrows.Next() {
		var r LatestPriceRow
		if err := lrows.Scan(&r.ItemID, &r.Price, &r.Day); err != nil {
			return nil, nil, err
		}
		latest = append(latest, r)
	}
	return daily, latest, lrows.Err()
}

// SaveSnapshotMeta records the published snapshot version for a market.
func (s *Store) SaveSnapshotMeta(ctx context.Context, marketID, ver, maxDay int, sha string, size int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE markets
		SET snapshot_ver = $2, snapshot_day = $3, snapshot_at = now(), snapshot_sha = $4, snapshot_size = $5, updated_at = now()
		WHERE id = $1
	`, marketID, ver, maxDay, sha, size)
	return err
}

// PruneOldObservations deletes raw per-install observations older than retentionDays (daily_prices is untouched).
func (s *Store) PruneOldObservations(ctx context.Context, retentionDays int) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM observations WHERE observed_at < now() - make_interval(days => $1)
	`, retentionDays)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
