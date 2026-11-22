package cli

import "testing"

func TestDamageGolden(t *testing.T) {
	wow := fixture(t)
	for _, c := range []struct {
		file string
		args []string
	}{
		{"damage-brakk-en.txt", []string{"--char", "Brakk", "--lang", "en"}},
		{"damage-ilse-de.txt", []string{"--char", "Ilse", "--lang", "de", "--all-ranks"}},
	} {
		out, code := runCmd(t, "damage", append([]string{"--wow", wow}, c.args...)...)
		if code != 0 {
			t.Fatalf("%s: exit %d", c.file, code)
		}
		golden(t, c.file, out)
	}
}

func TestDamageErrors(t *testing.T) {
	wow := fixture(t)
	if out, code := runCmd(t, "damage", "--wow", wow, "--char", "Nobody", "--lang", "de"); code != 1 || out != "" {
		t.Errorf("unknown character: exit %d, %q", code, out)
	}
}
