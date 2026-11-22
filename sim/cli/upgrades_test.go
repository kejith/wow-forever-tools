package cli

import (
	"strings"
	"testing"
	"time"
)

// utc formats the time of the item export in the header as the golden files have it (made with TZ=UTC).
func utc(t *testing.T) {
	t.Helper()
	local := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = local })
}

func TestUpgradesGolden(t *testing.T) {
	fx := fixture(t)
	utc(t)
	for _, c := range []struct {
		file string
		args []string
	}{
		{"upgrades-brakk-en.txt", []string{"--char", "Brakk", "--lang", "en"}},
		{"upgrades-ilse-de.txt", []string{"--char", "Ilse", "--lang", "de"}},
		{"upgrades-brakk-scale-de.txt", []string{"--char", "Brakk", "--lang", "de", "--engine", "off",
			"--max-price", "5", "--sort", "price", "--top", "5"}},
		{"upgrades-ilse-dps-en.txt", []string{"--char", "Ilse", "--lang", "en", "--metric", "dps",
			"--profile", "dungeon", "--preview", "0"}},
	} {
		out, code := runCmd(t, "upgrades", append([]string{"--wow", fx}, c.args...)...)
		if code != 0 {
			t.Fatalf("%s: exit %d", c.file, code)
		}
		golden(t, c.file, out)
	}
}

func TestUpgradesErrors(t *testing.T) {
	fx := fixture(t)
	if _, code := runCmd(t, "upgrades", "--wow", fx, "--char", "Nobody"); code != 1 {
		t.Errorf("unknown character: exit %d, want 1", code)
	}
	if _, code := runCmd(t, "upgrades", "--wow", fx, "--sort", "cheap"); code != 2 {
		t.Errorf("bad --sort: exit %d, want 2", code)
	}
	out, code := runCmd(t, "upgrades", "--wow", fx, "--char", "Brakk", "--json", "--engine", "off")
	if code != 0 || !strings.HasPrefix(out, "{") || !strings.Contains(out, `"slots": [`) {
		t.Errorf("--json: exit %d, %.80s", code, out)
	}
}
