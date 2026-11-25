package ingame

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/tools/marketsync"
)

const (
	// SyncHistoryFile is the compact synced price history file written into WoWToolsForever.
	SyncHistoryFile   = "SyncHistory.lua"
	syncHistoryGlobal = "WoWToolsForever_SyncHistory"
	syncDay0Unix      = 1577836800 // 2020-01-01T00:00:00Z
)

var syncHeadHash = regexp.MustCompile(`\bhash = "([0-9a-f]+)"`)

// SyncHistoryResult reports whether SyncHistory.lua was written.
type SyncHistoryResult struct {
	Written   bool   `json:"written"`
	Unchanged bool   `json:"unchanged,omitempty"`
	File      string `json:"file,omitempty"`
	Items     int    `json:"items"`
	Days      int    `json:"days"`
	Hash      string `json:"hash,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// WriteSyncHistory writes a compact, lazy-parsed SyncHistory.lua into the WoWToolsForever addon folder.
// Each item is encoded as one string `"m|day,low,high,offers;..."` (omitting high when == low) so WoW's
// Lua loader allocates zero nested tables on /reload. maxDays <= 0 keeps all available days.
func WriteSyncHistory(wow string, synced *marketsync.SyncedData, maxDays int) (*SyncHistoryResult, error) {
	dir := addonDir(wow)
	tocPath := game.Join(dir, "WoWToolsForever.toc")
	tocBytes, err := os.ReadFile(tocPath)
	if err != nil {
		return &SyncHistoryResult{Reason: "addon not installed"}, nil
	}
	if !strings.Contains(string(tocBytes), SyncHistoryFile) {
		return &SyncHistoryResult{Reason: "addon does not load " + SyncHistoryFile}, nil
	}
	path := game.Join(dir, SyncHistoryFile)
	if synced == nil || len(synced.Items) == 0 {
		return &SyncHistoryResult{Reason: "no synced data"}, nil
	}

	cutoffDay := int64(0)
	if maxDays > 0 && synced.LatestDay > int64(maxDays) {
		cutoffDay = synced.LatestDay - int64(maxDays) + 1
	}

	type encodedItem struct {
		id  int64
		val string
	}
	encoded := make([]encodedItem, 0, len(synced.Items))
	daySet := map[int64]struct{}{}
	var lastDay int64

	for key, rows := range synced.Items {
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil || id <= 0 || len(rows) == 0 {
			continue
		}
		sorted := make([][5]int64, 0, len(rows))
		for _, r := range rows {
			if r[0] < cutoffDay || r[1] <= 0 || r[2] < r[1] {
				continue
			}
			sorted = append(sorted, r)
		}
		if len(sorted) == 0 {
			continue
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })

		m := sorted[len(sorted)-1][1]
		if lp, ok := synced.Latest[key]; ok && lp[0] > 0 {
			m = lp[0]
		}
		var sb strings.Builder
		sb.WriteString(strconv.FormatInt(m, 10))
		sb.WriteByte('|')
		for i, r := range sorted {
			if i > 0 {
				sb.WriteByte(';')
			}
			day, low, high, offers := r[0], r[1], r[2], r[3]
			daySet[day] = struct{}{}
			if day > lastDay {
				lastDay = day
			}
			sb.WriteString(strconv.FormatInt(day, 10))
			sb.WriteByte(',')
			sb.WriteString(strconv.FormatInt(low, 10))
			sb.WriteByte(',')
			if high > low {
				sb.WriteString(strconv.FormatInt(high, 10))
			}
			sb.WriteByte(',')
			if offers > 0 {
				sb.WriteString(strconv.FormatInt(offers, 10))
			}
		}
		encoded = append(encoded, encodedItem{id: id, val: sb.String()})
	}

	sort.Slice(encoded, func(i, j int) bool { return encoded[i].id < encoded[j].id })

	// Stable content hash over market, maxDays, and item payloads.
	h := sha1.New()
	fmt.Fprintf(h, "%s|%s|%s|%d|%d\n", synced.Market, synced.RealmDisplay, synced.Faction, maxDays, lastDay)
	for _, it := range encoded {
		fmt.Fprintf(h, "%d=%s\n", it.id, it.val)
	}
	digest := hex.EncodeToString(h.Sum(nil))[:16]

	if existingHash := readSyncHistoryHash(path); existingHash == digest {
		return &SyncHistoryResult{
			Written:   false,
			Unchanged: true,
			File:      path,
			Items:     len(encoded),
			Days:      len(daySet),
			Hash:      digest,
		}, nil
	}

	var out strings.Builder
	out.WriteString("-- Written by the WoW Tools Forever desktop app (community price sync).\n")
	out.WriteString("-- Loaded on /reload by WoWToolsForever/History.lua. Do not edit: the app overwrites it.\n")
	fmt.Fprintf(&out, "%s = {\n", syncHistoryGlobal)
	fmt.Fprintf(&out, "  version = 1,\n")
	fmt.Fprintf(&out, "  generated = %d,\n", time.Now().Unix())
	fmt.Fprintf(&out, "  hash = %s,\n", luaString(digest))
	fmt.Fprintf(&out, "  market = %s,\n", luaString(synced.Market))
	fmt.Fprintf(&out, "  realm = %s,\n", luaString(synced.RealmDisplay))
	fmt.Fprintf(&out, "  faction = %s,\n", luaString(synced.Faction))
	fmt.Fprintf(&out, "  day0 = %d,\n", syncDay0Unix)
	fmt.Fprintf(&out, "  lastDay = %d,\n", lastDay)
	fmt.Fprintf(&out, "  maxDays = %d,\n", maxDays)
	out.WriteString("  items = {\n")
	for _, it := range encoded {
		fmt.Fprintf(&out, "    [%d] = %s,\n", it.id, luaString(it.val))
	}
	out.WriteString("  },\n}\n")

	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, []byte(out.String()), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	return &SyncHistoryResult{
		Written: true,
		File:    path,
		Items:   len(encoded),
		Days:    len(daySet),
		Hash:    digest,
	}, nil
}

func readSyncHistoryHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 1024)
	n, _ := f.Read(buf)
	if m := syncHeadHash.FindSubmatch(buf[:n]); m != nil {
		return string(m[1])
	}
	return ""
}
