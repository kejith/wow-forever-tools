package cli

import (
	"strings"
	"testing"
)

func TestPriceHistory(t *testing.T) {
	wow := fixture(t)
	for _, c := range []struct {
		file string
		args []string
	}{
		{"pricehistory-en.txt", []string{"--lang", "en"}},
		{"pricehistory-de.txt", []string{"--lang", "de"}},
		{"pricehistory-item-de.txt", []string{"--item", "2447", "--lang", "de"}},
	} {
		out, code := runCmd(t, "pricehistory", append([]string{"--wow", wow}, c.args...)...)
		if code != 0 {
			t.Fatalf("%s: exit %d", c.file, code)
		}
		golden(t, c.file, out)
	}
}

func TestPriceHistoryJSONAndErrors(t *testing.T) {
	wow := fixture(t)
	out, code := runCmd(t, "pricehistory", "--wow", wow, "--item", "999999", "--json", "--lang", "en")
	if code != 0 || !strings.HasPrefix(out, `{"day0": "2020-01-01", `) || !strings.Contains(out, `"items": [], "count": `) {
		t.Fatalf("exit %d: %.200s", code, out)
	}
	if _, code := runCmd(t, "pricehistory", "--wow", t.TempDir(), "--lang", "en"); code != 1 {
		t.Fatalf("no Auctionator.lua: exit %d", code)
	}
}
