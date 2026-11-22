package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIcons(t *testing.T) {
	fixture(t)
	if _, code := runCmd(t, "icons", "9492", "abc"); code != 1 {
		t.Fatalf("not a number: exit %d", code)
	}
	// Without item IDs only the question mark is needed; run it when the cache has it (no network in a test).
	cache := os.Getenv("WOW_TOOLS_CACHE")
	if _, err := os.Stat(filepath.Join(cache, "icons", "134400.png")); err != nil {
		t.Skip("no 134400.png in the cache")
	}
	out, code := runCmd(t, "icons")
	want := `{"icons": {}, "fallback": 134400, "dir": "`
	if code != 0 || !strings.HasPrefix(out, want) {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestASCIIJSON(t *testing.T) {
	in, want := "{\"a\": \"D\xc3\xbcsterfels \xf0\x9f\x90\x89\"}", `{"a": "D`+"\\"+`u00fcsterfels `+"\\"+`ud83d`+"\\"+`udc09"}`
	if got := string(asciiJSON([]byte(in))); got != want {
		t.Errorf("got %q", got)
	}
}
