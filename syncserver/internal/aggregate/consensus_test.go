package aggregate

import "testing"

func TestSingleReporterBootstrapAndOutlierGuard(t *testing.T) {
	c, ok := Compute([]Observation{{InstallID: "u1", Low: 150, High: 200, Offers: 40, LastPrice: 160, IsFullScan: true}}, 0)
	if !ok || c.Low != 150 || c.High != 200 || c.Offers != 40 || c.LatestPrice != 160 || c.Reporters != 1 || c.FullReporters != 1 {
		t.Fatalf("unexpected single bootstrap: %+v ok=%v", c, ok)
	}
	// Poison upload: 1 copper when trailing median is 10000c (ratio 0.0001 < 0.05)
	c, ok = Compute([]Observation{{InstallID: "bad", Low: 1, High: 1, Offers: 1}}, 10000)
	if ok || len(c.Outliers) != 1 || c.Outliers[0] != "bad" {
		t.Fatalf("expected poison rejection, got %+v ok=%v", c, ok)
	}
	// Normal fluctuation within 20x accepted
	c, ok = Compute([]Observation{{InstallID: "u1", Low: 15000, High: 18000, Offers: 12}}, 10000)
	if !ok || c.Low != 15000 {
		t.Fatalf("expected valid single update, got %+v ok=%v", c, ok)
	}
}

func TestPairCompatibleAndDivergent(t *testing.T) {
	// Compatible pair preserves intraday min/max
	c, ok := Compute([]Observation{
		{InstallID: "u1", Low: 100, High: 150, Offers: 10, LastPrice: 120, IsFullScan: true},
		{InstallID: "u2", Low: 130, High: 220, Offers: 25, LastPrice: 140, IsFullScan: false},
	}, 0)
	if !ok || c.Low != 100 || c.High != 220 || c.Offers != 25 || c.Reporters != 2 || c.FullReporters != 1 {
		t.Fatalf("unexpected pair merge: %+v ok=%v", c, ok)
	}

	// Divergent pair (>4x) uses trailing median to drop poison reporter
	c, ok = Compute([]Observation{
		{InstallID: "good", Low: 1050, High: 1200, Offers: 15, IsFullScan: true},
		{InstallID: "bad", Low: 999999, High: 999999, Offers: 1},
	}, 1000)
	if !ok || c.Low != 1050 || len(c.Outliers) != 1 || c.Outliers[0] != "bad" {
		t.Fatalf("expected good reporter to win, got %+v ok=%v", c, ok)
	}
}

func TestMultiReporterRejectsPoisonAndPreservesRange(t *testing.T) {
	obs := []Observation{
		{InstallID: "u1", Low: 900, High: 1100, Offers: 30, LastPrice: 950, IsFullScan: true},
		{InstallID: "u2", Low: 1000, High: 1300, Offers: 40, LastPrice: 1050, IsFullScan: true},
		{InstallID: "u3", Low: 1100, High: 1250, Offers: 35, LastPrice: 1100, IsFullScan: false},
		{InstallID: "poison-low", Low: 1, High: 1, Offers: 999999, LastPrice: 1},
		{InstallID: "poison-high", Low: 5000000, High: 5000000, Offers: 1, LastPrice: 5000000},
	}
	c, ok := Compute(obs, 1000)
	if !ok {
		t.Fatal("expected ok")
	}
	if c.Low != 900 || c.High != 1300 || c.Offers != 40 || c.Reporters != 3 || c.FullReporters != 2 {
		t.Fatalf("unexpected multi consensus: %+v", c)
	}
	if len(c.Outliers) != 2 {
		t.Fatalf("expected 2 outliers, got %v", c.Outliers)
	}
}

func TestIsAnomalousBatch(t *testing.T) {
	if IsAnomalousBatch(49, 49) {
		t.Fatal("below min items should not trigger")
	}
	if IsAnomalousBatch(100, 15) {
		t.Fatal("exact 15% should not trigger")
	}
	if !IsAnomalousBatch(100, 16) {
		t.Fatal("above 15% should trigger")
	}
}
