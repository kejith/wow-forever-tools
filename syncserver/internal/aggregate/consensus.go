// Package aggregate implements outlier-resistant multi-reporter consensus aggregation
// for daily Auction House price observations.
package aggregate

import (
	"math"
	"sort"
)

// Observation is one install's reported prices for a single (market, item, day).
type Observation struct {
	InstallID  string
	Low        int64
	High       int64
	Offers     int32
	LastPrice  int64 // >0 when this day is the item's newest day in the upload
	IsFullScan bool
}

// Consensus is the consolidated daily price row for one (market, item, day).
type Consensus struct {
	Low           int64
	High          int64
	Offers        int32
	LatestPrice   int64 // 0 when no reporter supplied a last_price
	Reporters     int16
	FullReporters int16
	Outliers      []string // InstallIDs whose values were rejected as outliers
}

// Bounds for outlier filtering.
const (
	singleMinRatio    = 0.05 // single reporter accepted within [1/20x, 20x] of trailing median
	singleMaxRatio    = 20.0
	pairDivergeFactor = 4.0 // two reporters are compatible within 4x of each other
	multiMinRatio     = 0.25
	multiMaxRatio     = 4.0
	offersMaxRatio    = 3.0
)

// Compute merges observations for one (item, day). trailingMedian is the item's
// recent multi-reporter median low price (0 when unknown).
// Returns ok=false when no valid consensus can be formed (e.g. a lone poisoned upload
// wildly outside established multi-reporter history).
func Compute(obs []Observation, trailingMedian int64) (Consensus, bool) {
	if len(obs) == 0 {
		return Consensus{}, false
	}
	if len(obs) == 1 {
		o := obs[0]
		if trailingMedian > 0 {
			ratio := float64(o.Low) / float64(trailingMedian)
			if ratio < singleMinRatio || ratio > singleMaxRatio {
				return Consensus{Outliers: []string{o.InstallID}}, false
			}
		}
		var full int16
		if o.IsFullScan {
			full = 1
		}
		return Consensus{
			Low:           o.Low,
			High:          maxInt64(o.Low, o.High),
			Offers:        o.Offers,
			LatestPrice:   o.LastPrice,
			Reporters:     1,
			FullReporters: full,
		}, true
	}

	if len(obs) == 2 {
		return computePair(obs[0], obs[1], trailingMedian)
	}

	return computeMulti(obs)
}

func computePair(a, b Observation, trailingMedian int64) (Consensus, bool) {
	lo, hi := a.Low, b.Low
	if lo > hi {
		lo, hi = hi, lo
	}
	var full int16
	if a.IsFullScan {
		full++
	}
	if b.IsFullScan {
		full++
	}
	if lo > 0 && float64(hi)/float64(lo) <= pairDivergeFactor {
		latest := medianPositive([]int64{a.LastPrice, b.LastPrice})
		return Consensus{
			Low:           minInt64(a.Low, b.Low),
			High:          maxInt64(a.High, b.High),
			Offers:        maxInt32(a.Offers, b.Offers),
			LatestPrice:   latest,
			Reporters:     2,
			FullReporters: full,
		}, true
	}
	// Two reporters diverge by >4x: prefer the one closer to trailing median if available.
	if trailingMedian > 0 {
		da := logDist(a.Low, trailingMedian)
		db := logDist(b.Low, trailingMedian)
		winner, loser := a, b
		if db < da {
			winner, loser = b, a
		}
		var wf int16
		if winner.IsFullScan {
			wf = 1
		}
		return Consensus{
			Low:           winner.Low,
			High:          maxInt64(winner.Low, winner.High),
			Offers:        winner.Offers,
			LatestPrice:   winner.LastPrice,
			Reporters:     1,
			FullReporters: wf,
			Outliers:      []string{loser.InstallID},
		}, true
	}
	// No trailing history: fall back to conservative average/median so neither extreme dominates.
	avgLow := (a.Low + b.Low) / 2
	avgHigh := maxInt64(avgLow, (a.High+b.High)/2)
	avgOffers := int32((int64(a.Offers) + int64(b.Offers)) / 2)
	return Consensus{
		Low:           avgLow,
		High:          avgHigh,
		Offers:        avgOffers,
		LatestPrice:   medianPositive([]int64{a.LastPrice, b.LastPrice}),
		Reporters:     2,
		FullReporters: full,
	}, true
}

func computeMulti(obs []Observation) (Consensus, bool) {
	lows := make([]int64, len(obs))
	highs := make([]int64, len(obs))
	offers := make([]int64, len(obs))
	for i, o := range obs {
		lows[i] = o.Low
		highs[i] = maxInt64(o.Low, o.High)
		offers[i] = int64(o.Offers)
	}
	medLow := medianInt64(lows)
	medHigh := medianInt64(highs)
	medOffers := medianInt64(offers)

	var inliers []Observation
	var outliers []string
	for _, o := range obs {
		h := maxInt64(o.Low, o.High)
		lowRatio := float64(o.Low) / float64(maxInt64(1, medLow))
		highRatio := float64(h) / float64(maxInt64(1, medHigh))
		if lowRatio < multiMinRatio || lowRatio > multiMaxRatio ||
			highRatio < multiMinRatio || highRatio > multiMaxRatio {
			outliers = append(outliers, o.InstallID)
			continue
		}
		inliers = append(inliers, o)
	}
	if len(inliers) == 0 {
		// Degenerate spread: take medians directly.
		var full int16
		var latestVals []int64
		for _, o := range obs {
			if o.IsFullScan {
				full++
			}
			if o.LastPrice > 0 {
				latestVals = append(latestVals, o.LastPrice)
			}
		}
		return Consensus{
			Low:           medLow,
			High:          maxInt64(medLow, medHigh),
			Offers:        int32(medOffers),
			LatestPrice:   medianPositive(latestVals),
			Reporters:     int16(len(obs)),
			FullReporters: full,
		}, true
	}

	minLow := inliers[0].Low
	maxHigh := maxInt64(inliers[0].Low, inliers[0].High)
	maxOfferCap := int64(math.Ceil(float64(maxInt64(1, medOffers)) * offersMaxRatio))
	bestOffers := int32(0)
	var full int16
	var latestVals []int64
	for _, o := range inliers {
		if o.Low < minLow {
			minLow = o.Low
		}
		h := maxInt64(o.Low, o.High)
		if h > maxHigh {
			maxHigh = h
		}
		if int64(o.Offers) <= maxOfferCap && o.Offers > bestOffers {
			bestOffers = o.Offers
		}
		if o.IsFullScan {
			full++
		}
		if o.LastPrice > 0 {
			latestVals = append(latestVals, o.LastPrice)
		}
	}
	if bestOffers == 0 && medOffers > 0 {
		bestOffers = int32(medOffers)
	}
	return Consensus{
		Low:           minLow,
		High:          maxHigh,
		Offers:        bestOffers,
		LatestPrice:   medianPositive(latestVals),
		Reporters:     int16(len(inliers)),
		FullReporters: full,
		Outliers:      outliers,
	}, true
}

// AnomalyThreshold: an install flagged as an outlier on more than 15% of at least 50
// multi-reporter items in a single batch counts as an anomalous upload.
const (
	AnomalyMinItems = 50
	AnomalyMaxShare = 0.15
	QuarantineRuns  = 3
)

// IsAnomalousBatch reports whether an install's outlier rate exceeds the threshold.
func IsAnomalousBatch(compared, outlierCount int) bool {
	return compared >= AnomalyMinItems && float64(outlierCount)/float64(compared) > AnomalyMaxShare
}

func logDist(a, b int64) float64 {
	if a <= 0 || b <= 0 {
		return math.Inf(1)
	}
	return math.Abs(math.Log(float64(a) / float64(b)))
}

func medianInt64(vals []int64) int64 {
	cp := append([]int64(nil), vals...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	n := len(cp)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

func medianPositive(vals []int64) int64 {
	var pos []int64
	for _, v := range vals {
		if v > 0 {
			pos = append(pos, v)
		}
	}
	return medianInt64(pos)
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
