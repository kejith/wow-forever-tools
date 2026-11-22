package cli

import "testing"

func TestHealingGolden(t *testing.T) {
	wow := fixture(t)
	for _, c := range []struct {
		file string
		args []string
	}{
		{"healing-ilse-en.txt", []string{"--char", "Ilse", "--lang", "en"}},
		{"healing-ilse-de.txt", []string{"--char", "Ilse", "--lang", "de", "--fight-seconds", "120", "--target-delta", "0"}},
	} {
		out, code := runCmd(t, "healing", append([]string{"--wow", wow}, c.args...)...)
		if code != 0 {
			t.Fatalf("%s: exit %d", c.file, code)
		}
		golden(t, c.file, out)
	}
}

func TestHealingErrors(t *testing.T) {
	wow := fixture(t)
	// A warlock has no heals.
	if out, code := runCmd(t, "healing", "--wow", wow, "--char", "Brakk", "--lang", "en"); code != 1 || out != "" {
		t.Errorf("warlock: exit %d, %q", code, out)
	}
}
