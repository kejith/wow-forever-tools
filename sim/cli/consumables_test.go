package cli

import "testing"

func TestConsumablesGolden(t *testing.T) {
	fx := fixture(t)
	for _, c := range []struct {
		file string
		args []string
	}{
		{"consumables-brakk-en.txt", []string{"--char", "Brakk", "--lang", "en"}},
		{"consumables-ilse-de.txt", []string{"--char", "Ilse", "--lang", "de"}},
		{"consumables-brakk-all-de.txt", []string{"--char", "Brakk", "--lang", "de", "--top", "2", "--all-buffs",
			"--engine", "off"}},
		{"consumables-ilse-fight-en.txt", []string{"--char", "Ilse", "--lang", "en", "--engine", "on",
			"--fight-seconds", "120", "--target-delta", "3"}},
		// The set by value; recorded from wowtools (the Python script had no such set).
		{"consumables-brakk-value-en.txt", []string{"--char", "Brakk", "--lang", "en", "--engine", "on",
			"--set", "value"}},
		{"consumables-ilse-value-budget-de.txt", []string{"--char", "Ilse", "--lang", "de", "--engine", "on",
			"--set", "value", "--budget", "0.05"}},
	} {
		out, code := runCmd(t, "consumables", append([]string{"--wow", fx}, c.args...)...)
		if code != 0 {
			t.Fatalf("%s: exit %d", c.file, code)
		}
		golden(t, c.file, out)
	}
	if _, code := runCmd(t, "consumables", "--wow", fx, "--char", "Nobody"); code != 1 {
		t.Errorf("unknown character: exit %d, want 1", code)
	}
	if _, code := runCmd(t, "consumables", "--wow", fx, "--char", "Brakk", "--set", "cheapest"); code == 0 {
		t.Errorf("unknown set: exit 0")
	}
}
