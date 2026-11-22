package cli

import (
	"strings"
	"testing"
	"time"
)

// The time a profession window was read is printed in local time: the goldens were saved with TZ=UTC.
func localUTC(t *testing.T) {
	t.Helper()
	local := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = local })
}

func TestRecipes(t *testing.T) {
	wow := fixture(t)
	localUTC(t)
	for _, c := range []struct {
		file string
		args []string
	}{
		{"recipes-brakk-en.txt", []string{"--char", "Brakk", "--lang", "en"}},
		{"recipes-brakk-de.txt", []string{"--char", "Brakk", "--lang", "de"}},
		{"recipes-brakk-alchemy-top4-de.txt", []string{"--char", "Brakk", "--profession", "Alch", "--top", "4", "--lang", "de"}},
	} {
		out, code := runCmd(t, "recipes", append([]string{"--wow", wow}, c.args...)...)
		if code != 0 {
			t.Fatalf("%s: exit %d", c.file, code)
		}
		golden(t, c.file, out)
	}
}

func TestRecipesJSONAndErrors(t *testing.T) {
	wow := fixture(t)
	out, code := runCmd(t, "recipes", "--wow", wow, "--char", "Ilse", "--lang", "en", "--json", "--top", "2")
	if code != 0 || !strings.HasPrefix(out, `{"character": "Ilse-Grabeshauch", `) {
		t.Fatalf("exit %d: %.120s", code, out)
	}
	if out, code := runCmd(t, "recipes", "--wow", wow, "--char", "Nobody", "--lang", "en"); code != 1 || out != "" {
		t.Fatalf("unknown character: exit %d, %q", code, out)
	}
}
