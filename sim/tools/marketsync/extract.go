package marketsync

import (
	"sort"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// FullScanShare mirrors pricehistory.FullScanShare: days with at least 30% of the largest day's item count.
const FullScanShare = 0.3

// RealmScan holds extracted numeric item-day tuples from one realm in Auctionator.lua.
type RealmScan struct {
	Market       MarketKey
	ScanTime     int64
	FullScanDays []int
	Entries      [][6]int64 // [itemId, day, low, high, offers, lastPriceOr0]
}

// DetectPrimaryMarket returns the MarketKey of the largest realm in Auctionator.lua, if present.
func DetectPrimaryMarket(wowDir string) (MarketKey, bool) {
	path := game.FindAuctionator(wowDir)
	if path == "" {
		return MarketKey{}, false
	}
	realmKey, _, err := sv.PricesWithRealm(path)
	if err != nil || realmKey == "" {
		return MarketKey{}, false
	}
	return MarketFromAuctionator(wowDir, realmKey)
}

// ExtractAuctionatorScans reads all realms from local Auctionator.lua and extracts numeric item-day tuples
// suitable for delta uploading. Returns all realm scans plus the primary market key.
func ExtractAuctionatorScans(wowDir string) ([]RealmScan, MarketKey, bool, error) {
	path := game.FindAuctionator(wowDir)
	if path == "" {
		return nil, MarketKey{}, false, nil
	}
	all, primaryKey, err := sv.AllRealmPrices(path)
	if err != nil {
		return nil, MarketKey{}, false, err
	}
	mtime, _ := game.Mtime(path)
	primary, hasPrimary := MarketFromAuctionator(wowDir, primaryKey)

	var scans []RealmScan
	for rKey, table := range all {
		mk, ok := MarketFromAuctionator(wowDir, rKey)
		if !ok {
			continue
		}
		scan := extractTable(mk, table, int64(mtime))
		scans = append(scans, scan)
	}
	sort.Slice(scans, func(i, j int) bool { return scans[i].Market.Path() < scans[j].Market.Path() })
	return scans, primary, hasPrimary, nil
}

func extractTable(mk MarketKey, prices *sv.Table, scanTime int64) RealmScan {
	type dayRow struct {
		day, low, high, offers int64
	}
	type itemRec struct {
		id   int64
		m    int64
		days []dayRow
	}

	var items []itemRec
	perDay := map[int64]int{}

	for i, k := range prices.Keys() {
		keyStr, _ := k.(string)
		if !isAllDigits(keyStr) {
			continue
		}
		entry, ok := prices.Values()[i].(*sv.Table)
		if !ok || entry.IsList() {
			continue
		}
		id, ok := pyfmt.ParseInt(keyStr)
		if !ok || id <= 0 {
			continue
		}
		highs, _ := entry.Get("h").(*sv.Table)
		if highs == nil || highs.IsList() {
			continue
		}
		lows, _ := entry.Get("l").(*sv.Table)
		offers, _ := entry.Get("a").(*sv.Table)

		var days []dayRow
		for j, dk := range highs.Keys() {
			ds := pyfmt.Str(dk)
			if !isAllDigits(ds) {
				continue
			}
			dNum, ok := pyfmt.ParseInt(ds)
			if !ok || dNum <= 0 {
				continue
			}
			hv, ok := toInt64(highs.Values()[j])
			if !ok || hv <= 0 {
				continue
			}
			lv := hv
			if lows != nil && !lows.IsList() {
				if v, ok := toInt64(lows.Get(dk)); ok && v > 0 && v <= hv {
					lv = v
				}
			}
			var av int64
			if offers != nil && !offers.IsList() {
				if v, ok := toInt64(offers.Get(dk)); ok && v > 0 {
					av = v
				}
			}
			days = append(days, dayRow{day: dNum, low: lv, high: hv, offers: av})
			perDay[dNum]++
		}
		if len(days) == 0 {
			continue
		}
		sort.Slice(days, func(a, b int) bool { return days[a].day < days[b].day })
		mVal := days[len(days)-1].low
		if mv, ok := toInt64(entry.Get("m")); ok && mv > 0 {
			mVal = mv
		}
		items = append(items, itemRec{id: id, m: mVal, days: days})
	}

	biggest := 0
	for _, n := range perDay {
		if n > biggest {
			biggest = n
		}
	}
	var fullDays []int
	for day, n := range perDay {
		if biggest > 0 && float64(n) >= FullScanShare*float64(biggest) {
			fullDays = append(fullDays, int(day))
		}
	}
	sort.Ints(fullDays)

	var entries [][6]int64
	for _, it := range items {
		lastIdx := len(it.days) - 1
		for idx, d := range it.days {
			var lp int64
			if idx == lastIdx {
				lp = it.m
			}
			entries = append(entries, [6]int64{it.id, d.day, d.low, d.high, d.offers, lp})
		}
	}
	return RealmScan{
		Market:       mk,
		ScanTime:     scanTime,
		FullScanDays: fullDays,
		Entries:      entries,
	}
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
