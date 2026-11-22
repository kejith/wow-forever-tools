package cli

import "testing"

func TestRotationsGolden(t *testing.T) {
	wow := fixture(t)
	for _, c := range []struct {
		file string
		args []string
	}{
		{"rotations-brakk-de.txt", []string{"--char", "Brakk", "--lang", "de", "--fight-seconds", "90", "--target-delta", "3",
			"--kills-per-minute", "2"}},
		{"rotations-ilse-en.txt", []string{"--char", "Ilse", "--lang", "en", "--consumables"}},
	} {
		out, code := runCmd(t, "rotations", append([]string{"--wow", wow}, c.args...)...)
		if code != 0 {
			t.Fatalf("%s: exit %d", c.file, code)
		}
		golden(t, c.file, out)
	}
}

func TestRotationsErrors(t *testing.T) {
	wow := fixture(t)
	if out, code := runCmd(t, "rotations", "--wow", wow, "--char", "Nobody", "--lang", "en"); code != 1 || out != "" {
		t.Errorf("unknown character: exit %d, %q", code, out)
	}
	if _, code := runCmd(t, "rotations", "--wow", wow, "--target-delta", "4"); code != 2 {
		t.Errorf("--target-delta 4: exit %d, want 2", code)
	}
}
