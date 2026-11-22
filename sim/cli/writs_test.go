package cli

import (
	"strings"
	"testing"
)

// The goldens are writs.py's text run with lupa (`uv run --with lupa`): the script has no inline
// dependencies, and without lupa it leaves out the measured payouts, the level and the favor.
func TestWrits(t *testing.T) {
	wow := fixture(t)
	for _, c := range []struct {
		file string
		args []string
	}{
		{"writs-brakk-en.txt", []string{"--char", "Brakk", "--lang", "en"}},
		{"writs-brakk-de.txt", []string{"--char", "Brakk", "--lang", "de"}},
		{"writs-ilse-top5-owned.txt", []string{"--char", "Ilse", "--top", "5", "--owned-at-ah-price", "--lang", "en"}},
	} {
		out, code := runCmd(t, "writs", append([]string{"--wow", wow}, c.args...)...)
		if code != 0 {
			t.Fatalf("%s: exit %d", c.file, code)
		}
		golden(t, c.file, out)
	}
}

func TestWritsJSONAndErrors(t *testing.T) {
	wow := fixture(t)
	out, code := runCmd(t, "writs", "--wow", wow, "--char", "Brakk", "--lang", "en", "--json")
	if code != 0 || !strings.HasPrefix(out, `{"scanDate": "2026-11-21", "character": "Brakk-Düsterfels", `) {
		t.Fatalf("exit %d: %.120s", code, out)
	}
	if _, code := runCmd(t, "writs", "--wow", t.TempDir(), "--lang", "en"); code != 1 {
		t.Fatalf("no Auctionator.lua: exit %d", code)
	}
	if _, code := runCmd(t, "writs", "--wow", wow, "--top", "x"); code != 2 {
		t.Fatalf("bad --top: exit %d", code)
	}
}
