// Package marketsync reads cached community-synced Auction House price history and merges it
// with local Auctionator.lua data without mutating Auctionator's SavedVariables.
package marketsync

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// EnvSyncDir is the environment variable passed by the desktop app pointing to its market-sync cache.
const EnvSyncDir = "WOW_TOOLS_SYNC_DIR"

// MarketKey uniquely identifies a single Auction House market (region + realm + faction).
type MarketKey struct {
	Region       string `json:"region"`
	Realm        string `json:"realm"`
	RealmDisplay string `json:"realmDisplay"`
	Faction      string `json:"faction"` // "horde" | "alliance"
}

// Valid reports whether the key has all required components.
func (m MarketKey) Valid() bool {
	return m.Region != "" && m.Realm != "" && (m.Faction == "horde" || m.Faction == "alliance")
}

// Path returns the canonical URL slug, e.g. "eu/pvp/horde".
func (m MarketKey) Path() string { return m.Region + "/" + m.Realm + "/" + m.Faction }

// DirName returns the local filesystem folder name, e.g. "eu-pvp-horde".
func (m MarketKey) DirName() string { return m.Region + "-" + m.Realm + "-" + m.Faction }

// FactionTitle returns "Horde" or "Alliance".
func (m MarketKey) FactionTitle() string {
	if m.Faction == "alliance" {
		return "Alliance"
	}
	return "Horde"
}

// Display returns a human-readable label, e.g. "EU · PvP · Horde".
func (m MarketKey) Display() string {
	realm := m.RealmDisplay
	if realm == "" {
		realm = m.Realm
	}
	return strings.ToUpper(m.Region) + " · " + realm + " · " + m.FactionTitle()
}

// SyncedData is the materialized cache file written by the desktop app at <syncDir>/<dirName>/merged.json.gz.
type SyncedData struct {
	Schema       int                   `json:"schema"`
	Market       string                `json:"market"`
	RealmDisplay string                `json:"realmDisplay"`
	Faction      string                `json:"faction"`
	UpdatedAt    int64                 `json:"updatedAt"`
	FirstDay     int64                 `json:"firstDay"`
	LatestDay    int64                 `json:"latestDay"`
	Latest       map[string][2]int64   `json:"latest"` // itemId -> [price, day]
	Items        map[string][][5]int64 `json:"items"`  // itemId -> [[day, low, high, offers, reporters], ...]
}

// SourceInfo describes where LoadPrices obtained its price data.
type SourceInfo struct {
	Market        MarketKey
	Synced        bool
	SyncUpdatedAt int64
	SyncFirstDay  int64
	SyncLatestDay int64
	SyncItems     int
	LocalPath     string
	LocalMtime    float64
}

var (
	portalRe   = regexp.MustCompile(`(?im)^\s*SET\s+portal\s+"([^"]+)"`)
	nonAlnumRe = regexp.MustCompile(`[^a-z0-9]+`)
)

// DetectPortal reads `SET portal "EU"` from <wowDir>/WTF/Config.wtf and returns a lowercase region code.
func DetectPortal(wowDir string) string {
	data, err := os.ReadFile(filepath.Join(wowDir, "WTF", "Config.wtf"))
	if err != nil {
		return ""
	}
	m := portalRe.FindSubmatch(data)
	if m == nil {
		return ""
	}
	p := strings.ToLower(strings.TrimSpace(string(m[1])))
	switch p {
	case "eu", "us", "kr", "tw", "cn":
		return p
	}
	return ""
}

// ParseAuctionatorRealm parses an Auctionator realm key (e.g. "PvP Horde") into display realm, slug, and faction.
func ParseAuctionatorRealm(key string) (displayRealm, realmSlug, faction string, ok bool) {
	k := strings.TrimSpace(key)
	lower := strings.ToLower(k)
	switch {
	case strings.HasSuffix(lower, " horde"):
		displayRealm = strings.TrimSpace(k[:len(k)-len(" horde")])
		faction = "horde"
	case strings.HasSuffix(lower, " alliance"):
		displayRealm = strings.TrimSpace(k[:len(k)-len(" alliance")])
		faction = "alliance"
	default:
		return "", "", "", false
	}
	realmSlug = Slug(displayRealm)
	if realmSlug == "" {
		return "", "", "", false
	}
	return displayRealm, realmSlug, faction, true
}

// Slug normalizes a realm name for URLs and cache directories.
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlnumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// MarketFromAuctionator combines Config.wtf portal and an Auctionator realm key.
func MarketFromAuctionator(wowDir, auctionatorRealmKey string) (MarketKey, bool) {
	region := DetectPortal(wowDir)
	if region == "" {
		return MarketKey{}, false
	}
	display, slug, faction, ok := ParseAuctionatorRealm(auctionatorRealmKey)
	if !ok {
		return MarketKey{}, false
	}
	return MarketKey{Region: region, Realm: slug, RealmDisplay: display, Faction: faction}, true
}

// MergedCachePath returns the path to the materialized merged cache for a market.
func MergedCachePath(syncDir string, m MarketKey) string {
	return filepath.Join(syncDir, m.DirName(), "merged.json.gz")
}

// ReadCache loads a materialized SyncedData cache file.
func ReadCache(path string) (*SyncedData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	var d SyncedData
	if err := json.NewDecoder(gz).Decode(&d); err != nil {
		return nil, err
	}
	if d.Schema != 1 {
		return nil, errors.New("unsupported sync cache schema")
	}
	return &d, nil
}

// WriteCache atomically writes a materialized SyncedData cache file (gzipped JSON).
func WriteCache(path string, d *SyncedData) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	gz, err := gzip.NewWriterLevel(f, gzip.DefaultCompression)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	enc := json.NewEncoder(gz)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(d); err != nil {
		gz.Close()
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// MergeTables merges community SyncedData with a local Auctionator *sv.Table without mutating local.
// Local may be nil (e.g. a user with no local scan yet).
func MergeTables(local *sv.Table, localMtime time.Time, synced *SyncedData) *sv.Table {
	out := sv.NewTable()
	if local != nil {
		for i, k := range local.Keys() {
			v := local.Values()[i]
			if entry, ok := v.(*sv.Table); ok && !entry.IsList() {
				out.Set(k, cloneEntry(entry))
			} else {
				out.Set(k, v)
			}
		}
	}
	if synced == nil {
		return out
	}

	localUnix := localMtime.Unix()

	for itemKey, rows := range synced.Items {
		entry, _ := out.Get(itemKey).(*sv.Table)
		if entry == nil || entry.IsList() {
			entry = sv.NewTable()
			entry.Set("h", sv.NewTable())
			entry.Set("l", sv.NewTable())
			entry.Set("a", sv.NewTable())
			out.Set(itemKey, entry)
		}
		hTab := ensureSubTable(entry, "h")
		lTab := ensureSubTable(entry, "l")
		aTab := ensureSubTable(entry, "a")

		localMaxDay := maxDayKey(hTab)

		var syncMaxDay int64
		var syncMaxDayLow int64
		for _, r := range rows {
			day, low, high, offers := r[0], r[1], r[2], r[3]
			if day <= 0 || low <= 0 || high < low {
				continue
			}
			if day > syncMaxDay {
				syncMaxDay = day
				syncMaxDayLow = low
			}
			ds := strconv.FormatInt(day, 10)
			if existingH, ok := toInt64(hTab.Get(ds)); ok && existingH > 0 {
				existingL := existingH
				if v, ok := toInt64(lTab.Get(ds)); ok && v > 0 && v <= existingH {
					existingL = v
				}
				mergedH := existingH
				if high > mergedH {
					mergedH = high
				}
				mergedL := existingL
				if low < mergedL {
					mergedL = low
				}
				hTab.Set(ds, mergedH)
				if mergedL < mergedH {
					lTab.Set(ds, mergedL)
				}
			} else {
				hTab.Set(ds, high)
				if low < high {
					lTab.Set(ds, low)
				}
			}
			if offers > 0 {
				if existingA, ok := toInt64(aTab.Get(ds)); !ok || offers > existingA {
					aTab.Set(ds, offers)
				}
			}
		}

		// Determine merged 'm' (last minimum buyout).
		syncPrice, syncDay := syncMaxDayLow, syncMaxDay
		if lp, ok := synced.Latest[itemKey]; ok && lp[0] > 0 {
			syncPrice, syncDay = lp[0], lp[1]
		}
		localM, hasLocalM := toInt64(entry.Get("m"))
		if !hasLocalM || localM <= 0 {
			if syncPrice > 0 {
				entry.Set("m", syncPrice)
			}
		} else if syncPrice > 0 {
			if syncDay > localMaxDay || (syncDay == localMaxDay && synced.UpdatedAt > localUnix) {
				entry.Set("m", syncPrice)
			}
		}
	}
	return out
}

// LoadPrices loads local Auctionator.lua and, when WOW_TOOLS_SYNC_DIR is set and a matching cached market
// exists, merges it with community-synced price history.
func LoadPrices(wow string) (*sv.Table, *SourceInfo, error) {
	syncDir := os.Getenv(EnvSyncDir)
	path := game.FindAuctionator(wow)

	var local *sv.Table
	var realmKey string
	var localMtime time.Time
	var localMtimeFloat float64
	var localErr error

	if path != "" {
		realmKey, local, localErr = sv.PricesWithRealm(path)
		if t, err := game.Mtime(path); err == nil {
			localMtimeFloat = t
			localMtime = time.Unix(int64(t), 0)
		}
	}

	info := &SourceInfo{LocalPath: path, LocalMtime: localMtimeFloat}

	var market MarketKey
	var hasMarket bool
	if realmKey != "" {
		market, hasMarket = MarketFromAuctionator(wow, realmKey)
	}
	if !hasMarket && syncDir != "" {
		// Fallback: if no local Auctionator.lua yet, check if there is exactly one cached market for this portal.
		market, hasMarket = findFallbackMarket(syncDir, DetectPortal(wow))
	}
	info.Market = market

	if syncDir != "" && hasMarket {
		cachePath := MergedCachePath(syncDir, market)
		if synced, err := ReadCache(cachePath); err == nil && len(synced.Items) > 0 {
			merged := MergeTables(local, localMtime, synced)
			info.Synced = true
			info.SyncUpdatedAt = synced.UpdatedAt
			info.SyncFirstDay = synced.FirstDay
			info.SyncLatestDay = synced.LatestDay
			info.SyncItems = len(synced.Items)
			return merged, info, nil
		}
	}

	// No sync cache available: return exact original Auctionator error/result.
	if path == "" {
		return nil, info, errors.New(i18n.T("Auctionator.lua not found under {wow}", "wow", wow))
	}
	if localErr != nil {
		if errors.Is(localErr, sv.ErrNoPrices) {
			return nil, info, errors.New(i18n.T("No price data found in Auctionator.lua."))
		}
		return nil, info, localErr
	}
	return local, info, nil
}

func findFallbackMarket(syncDir, portal string) (MarketKey, bool) {
	entries, err := os.ReadDir(syncDir)
	if err != nil {
		return MarketKey{}, false
	}
	var found []MarketKey
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if portal != "" && !strings.HasPrefix(name, portal+"-") {
			continue
		}
		parts := strings.Split(name, "-")
		if len(parts) < 3 {
			continue
		}
		reg := parts[0]
		fac := parts[len(parts)-1]
		realm := strings.Join(parts[1:len(parts)-1], "-")
		mk := MarketKey{Region: reg, Realm: realm, Faction: fac}
		if mk.Valid() {
			if _, err := os.Stat(MergedCachePath(syncDir, mk)); err == nil {
				found = append(found, mk)
			}
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return MarketKey{}, false
}

func cloneEntry(src *sv.Table) *sv.Table {
	dst := sv.NewTable()
	for i, k := range src.Keys() {
		v := src.Values()[i]
		if sub, ok := v.(*sv.Table); ok && !sub.IsList() {
			c := sv.NewTable()
			for j, sk := range sub.Keys() {
				c.Set(sk, sub.Values()[j])
			}
			dst.Set(k, c)
		} else {
			dst.Set(k, v)
		}
	}
	return dst
}

func ensureSubTable(entry *sv.Table, key string) *sv.Table {
	if sub, ok := entry.Get(key).(*sv.Table); ok && !sub.IsList() {
		return sub
	}
	sub := sv.NewTable()
	entry.Set(key, sub)
	return sub
}

func maxDayKey(t *sv.Table) int64 {
	var best int64
	for _, k := range t.Keys() {
		if d, ok := pyfmt.ParseInt(pyfmt.Str(k)); ok && d > best {
			best = d
		}
	}
	return best
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case float64:
		return pyfmt.RoundInt(x), true
	case int:
		return int64(x), true
	}
	return 0, false
}
